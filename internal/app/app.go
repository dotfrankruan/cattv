// Package app coordinates catalog selection, on-demand fetching, decoding,
// rendering, and local library playback.
package app

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math/rand"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/dotfrankruan/cattv/internal/library"
	"github.com/dotfrankruan/cattv/internal/render"
	"github.com/dotfrankruan/cattv/internal/state"
	"github.com/dotfrankruan/cattv/internal/terminal"
)

// Sentinel categories make CLI guidance precise without coupling the app to
// flag parsing.
var (
	ErrCatalogMissing = errors.New("cat catalog not found")
	ErrCatalogInvalid = errors.New("cat catalog is invalid")
	ErrOffline        = errors.New("network unavailable")
	ErrNoImage        = errors.New("no displayable image")
	ErrLibraryEmpty   = errors.New("local library is empty")
	ErrTerminalSmall  = errors.New("terminal is too small")
)

const (
	minColumns = 8
	minRows    = 4
	candidates = 6
)

// Fetcher downloads image bytes. *fetch.Client implements this interface;
// tests and embedders can supply alternatives.
type Fetcher interface {
	Get(context.Context, string) ([]byte, error)
}

// Options describes one display operation.
type Options struct {
	CatalogPath string
	LibraryPath string
	StatePath   string

	Renderer   render.Kind
	Geometry   terminal.Geometry
	MaxColumns int
	MaxRows    int
	NoCaption  bool
	Color      bool

	Out  io.Writer
	Log  io.Writer
	Rand *rand.Rand
}

// App executes display operations using immutable option state.
type App struct {
	opts    Options
	fetcher Fetcher
	rng     *rand.Rand
}

// Shown describes a successfully rendered image.
type Shown struct {
	Kind     string
	ID       string
	Source   string
	Caption  string
	Renderer render.Kind
	Result   render.Result
}

// New validates options and constructs an app. Fetcher may be nil for purely
// local operations.
func New(opts Options, fetcher Fetcher) (*App, error) {
	if opts.Renderer == "" || opts.Renderer == render.KindAuto {
		return nil, errors.New("a concrete renderer must be selected before display")
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Log == nil {
		opts.Log = os.Stderr
	}
	if opts.Geometry.Columns <= 0 || opts.Geometry.Rows <= 0 {
		opts.Geometry.Columns, opts.Geometry.Rows = 80, 24
	}
	if opts.Geometry.CellWidth <= 0 {
		opts.Geometry.CellWidth = 8
	}
	if opts.Geometry.CellHeight <= 0 {
		opts.Geometry.CellHeight = 16
	}
	rng := opts.Rand
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &App{opts: opts, fetcher: fetcher, rng: rng}, nil
}

func (a *App) drawable() (render.Env, error) {
	cols := a.opts.Geometry.Columns
	rows := a.opts.Geometry.Rows
	if a.opts.MaxColumns > 0 && cols > a.opts.MaxColumns {
		cols = a.opts.MaxColumns
	}
	if a.opts.MaxRows > 0 && rows > a.opts.MaxRows {
		rows = a.opts.MaxRows
	}
	reserved := 1 // leave the cursor a line of its own below the image
	if !a.opts.NoCaption {
		reserved += 2 // attribution can wrap on narrow terminals
	}
	rows -= reserved
	if cols < minColumns || rows < minRows {
		return render.Env{}, fmt.Errorf("%w: need at least %dx%d cells; have %dx%d",
			ErrTerminalSmall, minColumns, minRows+reserved, cols, a.opts.Geometry.Rows)
	}
	return render.Env{
		Columns:    cols,
		Rows:       rows,
		CellWidth:  a.opts.Geometry.CellWidth,
		CellHeight: a.opts.Geometry.CellHeight,
	}, nil
}

func (a *App) renderImage(img image.Image, caption string) (render.Result, error) {
	env, err := a.drawable()
	if err != nil {
		return render.Result{}, err
	}
	r, err := render.New(a.opts.Renderer)
	if err != nil {
		return render.Result{}, err
	}
	result, err := r.Render(a.opts.Out, img, env)
	if err != nil {
		return render.Result{}, fmt.Errorf("render image: %w", err)
	}
	if !a.opts.NoCaption && strings.TrimSpace(caption) != "" {
		caption = sanitizeCaption(caption)
		if a.opts.Color {
			if _, err := fmt.Fprintf(a.opts.Out, "\x1b[2m%s\x1b[0m\n", caption); err != nil {
				return result, err
			}
		} else {
			if _, err := fmt.Fprintln(a.opts.Out, caption); err != nil {
				return result, err
			}
		}
	}
	return result, nil
}

// sourceWidth chooses a bounded CDN variant from the drawable terminal area.
// ANSI samples one source pixel per column and two per row; graphics use the
// terminal's reported device-pixel cell size.
func (a *App) sourceWidth(env render.Env) int {
	if a.opts.Renderer == render.KindANSI {
		return clampInt(maxInt(env.Columns, env.Rows*2), 320, 1280)
	}
	pixelW := env.Columns * env.CellWidth
	pixelH := env.Rows * env.CellHeight
	return clampInt(maxInt(pixelW, pixelH), 320, 1280)
}

func (a *App) stateStore() *state.Store {
	return state.NewStore(a.opts.StatePath, 32)
}

func (a *App) loadState() state.State {
	st, err := a.stateStore().Load()
	if err != nil {
		// History is an optimization, not a prerequisite for cat photos.
		fmt.Fprintf(a.opts.Log, "cattv: warning: ignoring display history: %v\n", err)
		return state.State{Version: state.Version}
	}
	return st
}

func (a *App) markSeen(key string) {
	if err := a.stateStore().MarkSeen(key); err != nil {
		fmt.Fprintf(a.opts.Log, "cattv: warning: could not update display history: %v\n", err)
	}
}

func (a *App) libraryStore() *library.Store {
	return library.NewStore(a.opts.LibraryPath)
}

func sanitizeCaption(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
