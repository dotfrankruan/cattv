package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
)

type iTerm2Renderer struct{}

func (iTerm2Renderer) Kind() Kind { return KindITerm2 }

func (iTerm2Renderer) Render(w io.Writer, img image.Image, env Env) (Result, error) {
	targetW, targetH, cols, rows := graphicLayout(img, env)
	b := img.Bounds()
	display := img
	if b.Dx() > targetW || b.Dy() > targetH {
		display = scaleTo(img, targetW, targetH)
	}

	var pngBuf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&pngBuf, display); err != nil {
		return Result{}, fmt.Errorf("encode iTerm2 PNG: %w", err)
	}
	name := base64.StdEncoding.EncodeToString([]byte("cattv.png"))
	data := base64.StdEncoding.EncodeToString(pngBuf.Bytes())
	args := fmt.Sprintf("inline=1;size=%d;name=%s;width=%d;height=%d;preserveAspectRatio=1",
		pngBuf.Len(), name, cols, rows)
	if _, err := fmt.Fprintf(w, "\x1b]1337;File=%s:%s\a", args, data); err != nil {
		return Result{}, err
	}
	if _, err := io.WriteString(w, "\r\n"); err != nil {
		return Result{}, err
	}
	return Result{Columns: cols, Rows: rows, Width: display.Bounds().Dx(), Height: display.Bounds().Dy()}, nil
}
