package render

import (
	"bufio"
	"fmt"
	"image"
	"io"
)

type ansiRenderer struct{}

func (ansiRenderer) Kind() Kind { return KindANSI }

// Render draws the image with U+2580 (upper half block). Foreground and
// background true-colour escapes represent the top and bottom pixels. Alpha is
// composited over black because a terminal's background colour cannot be
// queried portably here.
func (ansiRenderer) Render(w io.Writer, img image.Image, env Env) (Result, error) {
	cellW, cellH := env.CellWidth, env.CellHeight
	if cellW <= 0 {
		cellW = 8
	}
	if cellH <= 0 {
		cellH = 16
	}
	maxCols, maxRows := env.Columns, env.Rows
	if maxCols < 1 {
		maxCols = 1
	}
	if maxRows < 1 {
		maxRows = 1
	}

	b := img.Bounds()
	physW, physH := fillDimensions(b.Dx(), b.Dy(), maxCols*cellW, maxRows*cellH)
	cols := (physW + cellW - 1) / cellW
	rows := (physH + cellH - 1) / cellH
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	if cols > maxCols {
		cols = maxCols
	}
	if rows > maxRows {
		rows = maxRows
	}

	// One character is one sample column and two vertical samples. Scale
	// anisotropically so the *physical* aspect ratio (not the character grid
	// ratio) matches the source when cell dimensions are non-square.
	raster := scaleTo(img, cols, rows*2)
	bw := bufio.NewWriterSize(w, 64*1024)
	for y := 0; y < rows; y++ {
		lastFG, lastBG := -1, -1
		for x := 0; x < cols; x++ {
			topR, topG, topB := compositeNRGBA(raster, x, y*2)
			bottomR, bottomG, bottomB := compositeNRGBA(raster, x, y*2+1)
			fg := packRGB(topR, topG, topB)
			bg := packRGB(bottomR, bottomG, bottomB)
			if fg != lastFG {
				if _, err := fmt.Fprintf(bw, "\x1b[38;2;%d;%d;%dm", topR, topG, topB); err != nil {
					return Result{}, err
				}
				lastFG = fg
			}
			if bg != lastBG {
				if _, err := fmt.Fprintf(bw, "\x1b[48;2;%d;%d;%dm", bottomR, bottomG, bottomB); err != nil {
					return Result{}, err
				}
				lastBG = bg
			}
			if _, err := bw.WriteString("▀"); err != nil {
				return Result{}, err
			}
		}
		if _, err := bw.WriteString("\x1b[0m\r\n"); err != nil {
			return Result{}, err
		}
	}
	if err := bw.Flush(); err != nil {
		return Result{}, err
	}
	return Result{Columns: cols, Rows: rows, Width: cols, Height: rows * 2}, nil
}

func compositeNRGBA(img *image.NRGBA, x, y int) (uint8, uint8, uint8) {
	off := img.PixOffset(x, y)
	r, g, b, a := img.Pix[off], img.Pix[off+1], img.Pix[off+2], img.Pix[off+3]
	if a == 0 {
		return 0, 0, 0
	}
	if a == 255 {
		return r, g, b
	}
	// Composite onto black: c' = c * alpha / 255.
	alpha := uint32(a)
	return uint8(uint32(r) * alpha / 255), uint8(uint32(g) * alpha / 255), uint8(uint32(b) * alpha / 255)
}

func packRGB(r, g, b uint8) int {
	return int(r)<<16 | int(g)<<8 | int(b)
}
