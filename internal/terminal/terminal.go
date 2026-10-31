// Package terminal discovers terminal dimensions and graphics capabilities.
package terminal

import (
	"os"
	"strconv"

	"golang.org/x/term"
)

const (
	defaultColumns    = 80
	defaultRows       = 24
	defaultCellWidth  = 8
	defaultCellHeight = 16
)

// Geometry describes the output area. Cell dimensions are expressed in device
// pixels and are useful for sizing raster images before upload.
type Geometry struct {
	Columns     int
	Rows        int
	CellWidth   int
	CellHeight  int
	PixelWidth  int
	PixelHeight int
	TTY         bool
}

// Detect inspects the output terminal. It is safe to call with a redirected
// stdout: in that case it returns an 80x24 synthetic geometry.
func Detect(out *os.File) Geometry {
	g := Geometry{
		Columns:    defaultColumns,
		Rows:       defaultRows,
		CellWidth:  defaultCellWidth,
		CellHeight: defaultCellHeight,
	}
	if out != nil && term.IsTerminal(int(out.Fd())) {
		g.TTY = true
		if cols, rows, err := term.GetSize(int(out.Fd())); err == nil {
			if cols > 0 {
				g.Columns = cols
			}
			if rows > 0 {
				g.Rows = rows
			}
		}
		if w, h := pixelSize(out); w > 0 && h > 0 {
			g.PixelWidth, g.PixelHeight = w, h
			if cw := w / g.Columns; cw > 0 {
				g.CellWidth = cw
			}
			if ch := h / g.Rows; ch > 0 {
				g.CellHeight = ch
			}
		}
	} else {
		g.Columns = envInt("COLUMNS", defaultColumns, 1, 1000)
		g.Rows = envInt("LINES", defaultRows, 1, 1000)
	}
	g.CellWidth = envInt("CATTV_CELL_WIDTH", g.CellWidth, 1, 256)
	g.CellHeight = envInt("CATTV_CELL_HEIGHT", g.CellHeight, 1, 256)
	if g.PixelWidth <= 0 {
		g.PixelWidth = g.Columns * g.CellWidth
	}
	if g.PixelHeight <= 0 {
		g.PixelHeight = g.Rows * g.CellHeight
	}
	return g
}

// IsTTY reports whether the stream is an interactive terminal.
func IsTTY(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}

func envInt(name string, fallback, min, max int) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return fallback
	}
	return n
}
