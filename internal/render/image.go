package render

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// MaxPixels bounds decoded image allocations. Remote downloads are already
// size-limited; this additionally protects against decompression bombs and
// absurd local files.
const MaxPixels int64 = 60_000_000

// Decode decodes a raster image from a seekable source and rejects images
// above MaxPixels before allocating the full bitmap.
func Decode(r io.ReadSeeker) (image.Image, string, error) {
	cfg, format, err := image.DecodeConfig(r)
	if err != nil {
		return nil, "", fmt.Errorf("unsupported or corrupt image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, "", errors.New("image has invalid dimensions")
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, "", fmt.Errorf("image is too large to decode (%dx%d, limit %d megapixels)", cfg.Width, cfg.Height, MaxPixels/1_000_000)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, "", fmt.Errorf("rewind image: %w", err)
	}
	img, decodedFormat, err := image.Decode(r)
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}
	if decodedFormat != "" {
		format = decodedFormat
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		return nil, "", errors.New("image decoded to an empty canvas")
	}
	return img, format, nil
}

// DecodeBytes decodes a remote image held in memory.
func DecodeBytes(data []byte) (image.Image, string, error) {
	return Decode(bytes.NewReader(data))
}

// DecodeFile opens and decodes a local image.
func DecodeFile(path string) (image.Image, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	return Decode(f)
}

// fitDimensions scales width and height uniformly to fit inside maxW x maxH.
func fitDimensions(width, height, maxW, maxH int) (int, int) {
	if width <= 0 || height <= 0 || maxW <= 0 || maxH <= 0 {
		return 0, 0
	}
	if width <= maxW && height <= maxH {
		return width, height
	}
	scaleW := float64(maxW) / float64(width)
	scaleH := float64(maxH) / float64(height)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}
	w := int(float64(width)*scale + 0.5)
	h := int(float64(height)*scale + 0.5)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w > maxW {
		w = maxW
	}
	if h > maxH {
		h = maxH
	}
	return w, h
}

// fillDimensions scales width and height uniformly to use the full maxW x maxH
// box, upscaling when necessary. Rendering, not decoding, decides whether an
// upscale actually has to be materialized.
func fillDimensions(width, height, maxW, maxH int) (int, int) {
	if width <= 0 || height <= 0 || maxW <= 0 || maxH <= 0 {
		return 0, 0
	}
	scaleW := float64(maxW) / float64(width)
	scaleH := float64(maxH) / float64(height)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}
	w := int(float64(width)*scale + 0.5)
	h := int(float64(height)*scale + 0.5)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if w > maxW {
		w = maxW
	}
	if h > maxH {
		h = maxH
	}
	return w, h
}

func scaleTo(img image.Image, width, height int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Src, nil)
	return dst
}

func scaleToFit(img image.Image, maxW, maxH int) (*image.NRGBA, int, int) {
	b := img.Bounds()
	w, h := fitDimensions(b.Dx(), b.Dy(), maxW, maxH)
	return scaleTo(img, w, h), w, h
}

// graphicLayout computes both the terminal cell rectangle and a bounded raster
// size. Source images smaller than the target are left at native size and
// scaled by the terminal; larger images are downsampled once in Go.
func graphicLayout(img image.Image, env Env) (targetW, targetH, cols, rows int) {
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
	targetW, targetH = fillDimensions(b.Dx(), b.Dy(), maxCols*cellW, maxRows*cellH)
	cols = (targetW + cellW - 1) / cellW
	rows = (targetH + cellH - 1) / cellH
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
	return targetW, targetH, cols, rows
}
