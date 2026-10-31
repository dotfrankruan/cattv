// Package cli implements CatTV's command-line interface.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/dotfrankruan/cattv/internal/app"
	"github.com/dotfrankruan/cattv/internal/config"
	"github.com/dotfrankruan/cattv/internal/version"
)

const (
	exitOK        = 0
	exitRuntime   = 1
	exitUsage     = 2
	exitSetup     = 3
	exitNetwork   = 4
	exitNoImage   = 5
	exitInterrupt = 130
)

type cli struct {
	in     *os.File
	out    *os.File
	errOut io.Writer
	cfg    config.Config
}

type commandError struct {
	code int
	err  error
}

func (e *commandError) Error() string { return e.err.Error() }
func (e *commandError) Unwrap() error { return e.err }

type usageError struct {
	err error
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// Run executes one CatTV invocation. A nil input or output falls back to the
// process streams. errOut may be any writer, which keeps tests simple.
func Run(ctx context.Context, args []string, in, out *os.File, errOut io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	c := &cli{in: in, out: out, errOut: errOut}
	if err := c.run(ctx, args); err != nil {
		return c.report(err)
	}
	return exitOK
}

func (c *cli) run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return c.afterConfig(ctx, args)
	}
	switch args[0] {
	case "help":
		c.printHelpTopic(args[1:])
		return nil
	case "-h", "--help", "-help":
		c.printHelp()
		return nil
	case "version", "--version", "-version":
		fmt.Fprintf(c.out, "cattv %s (%s/%s)\n", version.Current(), runtime.GOOS, runtime.GOARCH)
		return nil
	}
	return c.afterConfig(ctx, args)
}

func (c *cli) afterConfig(ctx context.Context, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	c.cfg = cfg

	if len(args) == 0 {
		return c.runNext(ctx, nil)
	}
	cmd, rest := args[0], args[1:]
	if strings.HasPrefix(cmd, "-") {
		// Allow `cattv --renderer ansi` as shorthand for `cattv next`.
		return c.runNext(ctx, args)
	}
	switch cmd {
	case "next":
		return c.runNext(ctx, rest)
	case "show":
		return c.runShow(rest)
	case "local":
		return c.runLocal(ctx, rest)
	case "import":
		return c.runImport(ctx, rest)
	case "init":
		return c.runInit(ctx, rest)
	case "catalog":
		return c.runCatalog(ctx, rest)
	case "library":
		return c.runLibrary(ctx, rest)
	case "doctor":
		return c.runDoctor(rest)
	case "fifo":
		return c.runFIFO(ctx, rest)
	default:
		return &usageError{err: fmt.Errorf("unknown command %q", cmd)}
	}
}

func (c *cli) report(err error) int {
	if err == nil {
		return exitOK
	}
	var ce *commandError
	if errors.As(err, &ce) {
		fmt.Fprintln(c.errOut, ce.err)
		return ce.code
	}
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(c.errOut, "cattv: %v\n", ue.err)
		fmt.Fprintln(c.errOut, "Run `cattv help` for usage.")
		return exitUsage
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(c.errOut, "cattv: cancelled")
		return exitInterrupt
	}
	if errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintln(c.errOut, "cattv: network request timed out")
		fmt.Fprintln(c.errOut, "Try again, or use `cattv show <file>` / `cattv local` while offline.")
		return exitNetwork
	}
	if errors.Is(err, app.ErrCatalogMissing) || errors.Is(err, app.ErrCatalogInvalid) {
		fmt.Fprintf(c.errOut, "cattv: %v\n", err)
		fmt.Fprintln(c.errOut, "")
		fmt.Fprintln(c.errOut, "Build the local metadata catalog once, without downloading images:")
		fmt.Fprintln(c.errOut, "  cattv init --parquet /path/to/unsplash_cats_clean.parquet")
		fmt.Fprintln(c.errOut, "or set CATTV_PARQUET and run `cattv init`.")
		return exitSetup
	}
	if errors.Is(err, app.ErrOffline) {
		fmt.Fprintf(c.errOut, "cattv: %v\n", err)
		fmt.Fprintln(c.errOut, "")
		fmt.Fprintln(c.errOut, "The default `cattv` command fetches one image on demand and needs a network connection.")
		fmt.Fprintln(c.errOut, "Offline alternatives:")
		fmt.Fprintln(c.errOut, "  cattv show <local-image>")
		fmt.Fprintln(c.errOut, "  cattv local")
		return exitNetwork
	}
	if errors.Is(err, app.ErrLibraryEmpty) {
		fmt.Fprintln(c.errOut, "cattv: the local library is empty.")
		fmt.Fprintln(c.errOut, "Add images with:")
		fmt.Fprintln(c.errOut, "  cattv import ~/Pictures/cats")
		return exitNoImage
	}
	if errors.Is(err, app.ErrNoImage) {
		fmt.Fprintf(c.errOut, "cattv: %v\n", err)
		fmt.Fprintln(c.errOut, "Try again, inspect `cattv doctor`, or add local images with `cattv import <path>`.")
		return exitNoImage
	}
	if errors.Is(err, app.ErrTerminalSmall) {
		fmt.Fprintf(c.errOut, "cattv: %v\n", err)
		fmt.Fprintln(c.errOut, "Resize the terminal or set COLUMNS/LINES.")
		return exitRuntime
	}
	fmt.Fprintf(c.errOut, "cattv: %v\n", err)
	return exitRuntime
}

// parseFlags allows options before or after positional arguments. The standard
// library stops at the first positional argument, which is surprising for a
// one-shot CLI like CatTV.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var flagArgs, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		flagArgs = append(flagArgs, arg)
		name := strings.TrimLeft(arg, "-")
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			continue
		}
		f := fs.Lookup(name)
		if f == nil {
			// Let fs.Parse report the unknown flag with its standard message.
			continue
		}
		if boolFlag, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && boolFlag.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	return positional, nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func addHelpFlag(fs *flag.FlagSet, target *bool) {
	fs.BoolVar(target, "help", false, "show this help")
	fs.BoolVar(target, "h", false, "show this help")
}
