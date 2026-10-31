package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dotfrankruan/cattv/internal/app"
	"github.com/dotfrankruan/cattv/internal/delivery"
	"github.com/dotfrankruan/cattv/internal/fetch"
	"github.com/dotfrankruan/cattv/internal/render"
	"github.com/dotfrankruan/cattv/internal/terminal"
	"github.com/dotfrankruan/cattv/internal/textsafe"
	"github.com/dotfrankruan/cattv/internal/version"
)

func (c *cli) runFIFO(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		c.printFIFOHelp()
		return nil
	}
	switch args[0] {
	case "serve":
		return c.runFIFOServe(ctx, args[1:])
	case "client":
		return c.runFIFOClient(ctx, args[1:])
	case "path":
		return c.runFIFOPath(args[1:])
	default:
		return &usageError{err: fmt.Errorf("unknown fifo command %q", args[0])}
	}
}

func (c *cli) runFIFOServe(ctx context.Context, args []string) error {
	flags, err := newDisplayFlags()
	if err != nil {
		return err
	}
	flags.renderer = fifoDefaultRenderer()
	fs := newFlagSet("cattv fifo serve")
	flags.register(fs)
	var (
		fifoPath   string
		socketPath string
		noSocket   bool
	)
	fs.StringVar(&fifoPath, "fifo", "", "FIFO path (default: $CATTV_FIFO or ~/.cache/cat.fifo)")
	fs.StringVar(&socketPath, "socket", "", "strict Unix socket path (default: $CATTV_SOCKET or ~/.cache/cat.sock)")
	fs.BoolVar(&noSocket, "no-socket", false, "disable the strict Unix socket endpoint")
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if flags.help {
		c.printFIFOServeHelp()
		return nil
	}
	if len(positional) != 0 {
		return &usageError{err: fmt.Errorf("unexpected argument %q", positional[0])}
	}
	if err := flags.validate(); err != nil {
		return err
	}
	kind, err := render.ParseKind(flags.renderer)
	if err != nil {
		return &usageError{err: err}
	}
	if kind == render.KindAuto {
		return &usageError{err: errors.New("fifo serve requires an explicit renderer (kitty, iterm2, or ansi)")}
	}
	fifoPath, socketPath, err = resolveFIFOPaths(fifoPath, socketPath, noSocket)
	if err != nil {
		return err
	}

	geometry := terminal.Detect(c.out)
	fetcher := fetch.New(minDuration(flags.timeout, 10*time.Second), flags.retries, maxDownloadBytes, version.UserAgent())
	var (
		generateMu sync.Mutex
		printMu    sync.Mutex
	)
	generate := func(frameCtx context.Context) ([]byte, error) {
		generateMu.Lock()
		defer generateMu.Unlock()
		frameCtx, cancel := context.WithTimeout(frameCtx, flags.timeout)
		defer cancel()
		var out bytes.Buffer
		a, err := app.New(app.Options{
			CatalogPath: c.cfg.CatalogPath,
			LibraryPath: c.cfg.LibraryPath,
			StatePath:   c.cfg.StatePath,
			Renderer:    kind,
			Geometry:    geometry,
			MaxColumns:  flags.width,
			MaxRows:     flags.height,
			NoCaption:   flags.noCaption,
			// The bytes are consumed by a terminal through `cat` or the strict
			// client, so muted-detail styling is appropriate even though the
			// server's own stdout may be redirected.
			Color: true,
			Out:   &out,
			Log:   c.errOut,
		}, fetcher)
		if err != nil {
			return nil, err
		}
		if _, err := a.Next(frameCtx); err != nil {
			return nil, err
		}
		return out.Bytes(), nil
	}

	ready := func(format string, args ...any) {
		printMu.Lock()
		defer printMu.Unlock()
		fmt.Fprintf(c.out, format, args...)
	}
	fmt.Fprintf(c.out, "CatTV FIFO delivery\n")
	fmt.Fprintf(c.out, "  renderer: %s\n", kind)
	fmt.Fprintf(c.out, "  geometry: %dx%d cells, %dx%d px\n",
		geometry.Columns, geometry.Rows, geometry.PixelWidth, geometry.PixelHeight)

	fifoServer := &delivery.FIFOServer{
		Path:      fifoPath,
		Generator: delivery.GeneratorFunc(generate),
		Log:       c.errOut,
		OnReady: func() {
			ready("FIFO ready: %s\n  cat %s\n", textsafe.Terminal(fifoPath), shellQuote(textsafe.Terminal(fifoPath)))
		},
	}
	servers := []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "FIFO", run: fifoServer.Run},
	}
	if socketPath != "" {
		socketServer := &delivery.SocketServer{
			Path:      socketPath,
			Generator: delivery.GeneratorFunc(generate),
			Log:       c.errOut,
			OnReady: func() {
				ready("Strict socket ready: %s\n  cattv fifo client --socket %s\n", textsafe.Terminal(socketPath), shellQuote(textsafe.Terminal(socketPath)))
			},
		}
		servers = append(servers, struct {
			name string
			run  func(context.Context) error
		}{name: "strict socket", run: socketServer.Run})
	}
	if socketPath != "" {
		fmt.Fprintln(c.out, "  FIFO is experimental; use the strict client for per-invocation guarantees.")
	} else {
		fmt.Fprintln(c.out, "  FIFO is experimental and has no per-reader message boundaries.")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type serverResult struct {
		name string
		err  error
	}
	results := make(chan serverResult, len(servers))
	for _, server := range servers {
		server := server
		go func() {
			results <- serverResult{name: server.name, err: server.run(runCtx)}
		}()
	}
	var firstErr error
	for i := 0; i < len(servers); i++ {
		result := <-results
		switch {
		case result.err != nil:
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", result.name, result.err)
				cancel()
			}
		case runCtx.Err() == nil:
			if firstErr == nil {
				firstErr = fmt.Errorf("%s stopped unexpectedly", result.name)
				cancel()
			}
		}
	}
	return firstErr
}

func (c *cli) runFIFOClient(ctx context.Context, args []string) error {
	fs := newFlagSet("cattv fifo client")
	var (
		socketPath string
		help       bool
		timeout    = 30 * time.Second
	)
	fs.StringVar(&socketPath, "socket", "", "strict Unix socket path (default: $CATTV_SOCKET or ~/.cache/cat.sock)")
	fs.Var(durationValue{target: &timeout}, "timeout", "overall request deadline, e.g. 30s")
	addHelpFlag(fs, &help)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if help {
		c.printFIFOClientHelp()
		return nil
	}
	if len(positional) != 0 {
		return &usageError{err: fmt.Errorf("unexpected argument %q", positional[0])}
	}
	if timeout < time.Second || timeout > 10*time.Minute {
		return &usageError{err: errors.New("--timeout must be between 1s and 10m")}
	}
	if socketPath == "" {
		socketPath = defaultSocketPath()
	}
	if socketPath == "" {
		return errors.New("cannot determine the Unix socket path; set CATTV_SOCKET or pass --socket")
	}
	payload, err := delivery.FetchFrame(ctx, socketPath, timeout)
	if err != nil {
		return fmt.Errorf("%w (is `cattv fifo serve` running?)", err)
	}
	if err := writeOutput(c.out, payload); err != nil {
		return err
	}
	return nil
}

func (c *cli) runFIFOPath(args []string) error {
	fs := newFlagSet("cattv fifo path")
	var (
		fifoPath   string
		socketPath string
		noSocket   bool
		help       bool
	)
	fs.StringVar(&fifoPath, "fifo", "", "FIFO path to resolve")
	fs.StringVar(&socketPath, "socket", "", "socket path to resolve")
	fs.BoolVar(&noSocket, "no-socket", false, "omit the socket path")
	addHelpFlag(fs, &help)
	positional, err := parseFlags(fs, args)
	if err != nil {
		return &usageError{err: err}
	}
	if help {
		fmt.Fprintln(c.out, "Usage: cattv fifo path [--fifo PATH] [--socket PATH]")
		return nil
	}
	if len(positional) != 0 {
		return &usageError{err: fmt.Errorf("unexpected argument %q", positional[0])}
	}
	fifoPath, socketPath, err = resolveFIFOPaths(fifoPath, socketPath, noSocket)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "FIFO: %s\n", textsafe.Terminal(fifoPath))
	if socketPath != "" {
		fmt.Fprintf(c.out, "Socket: %s\n", textsafe.Terminal(socketPath))
	}
	return nil
}

func resolveFIFOPaths(fifoPath, socketPath string, noSocket bool) (string, string, error) {
	if fifoPath == "" {
		fifoPath = defaultFIFOPath()
	}
	if fifoPath == "" {
		return "", "", errors.New("cannot determine the FIFO path; set CATTV_FIFO or pass --fifo")
	}
	if !noSocket && socketPath == "" {
		socketPath = defaultSocketPath()
	}
	if noSocket {
		socketPath = ""
	}
	if socketPath != "" {
		fifoAbs, err := filepath.Abs(fifoPath)
		if err != nil {
			return "", "", err
		}
		socketAbs, err := filepath.Abs(socketPath)
		if err != nil {
			return "", "", err
		}
		if fifoAbs == socketAbs {
			return "", "", errors.New("FIFO and socket paths must differ")
		}
	}
	return fifoPath, socketPath, nil
}

func defaultFIFOPath() string {
	if path := os.Getenv("CATTV_FIFO"); path != "" {
		return path
	}
	return defaultCachePath("cat.fifo")
}

func defaultSocketPath() string {
	if path := os.Getenv("CATTV_SOCKET"); path != "" {
		return path
	}
	return defaultCachePath("cat.sock")
}

func defaultCachePath(name string) string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", name)
}

func fifoDefaultRenderer() string {
	if value := strings.TrimSpace(os.Getenv("CATTV_RENDERER")); value != "" {
		return value
	}
	return string(render.KindKitty)
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:@%+=,-"
	if strings.IndexFunc(s, func(r rune) bool { return !strings.ContainsRune(safe, r) }) == -1 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func writeOutput(out *os.File, payload []byte) error {
	for len(payload) > 0 {
		n, err := out.Write(payload)
		if n > 0 {
			payload = payload[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("short write to standard output")
		}
	}
	return nil
}
