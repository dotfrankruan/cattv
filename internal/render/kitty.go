package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
)

// Kitty protocol payload chunks are limited to 4096 base64 characters.
const kittyChunkSize = 4096

type kittyRenderer struct{}

func (kittyRenderer) Kind() Kind { return KindKitty }

func (kittyRenderer) Render(w io.Writer, img image.Image, env Env) (Result, error) {
	targetW, targetH, cols, rows := graphicLayout(img, env)
	b := img.Bounds()
	display := img
	if b.Dx() > targetW || b.Dy() > targetH {
		display = scaleTo(img, targetW, targetH)
	}

	var pngBuf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&pngBuf, display); err != nil {
		return Result{}, fmt.Errorf("encode kitty PNG: %w", err)
	}
	payload := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	firstControl := fmt.Sprintf("a=T,f=100,t=d,q=2,C=1,c=%d,r=%d", cols, rows)
	if err := writeKittyChunks(w, payload, firstControl); err != nil {
		return Result{}, err
	}
	if _, err := io.WriteString(w, "\r"+repeatNewline(rows)); err != nil {
		return Result{}, err
	}
	return Result{Columns: cols, Rows: rows, Width: display.Bounds().Dx(), Height: display.Bounds().Dy()}, nil
}

func writeKittyChunks(w io.Writer, payload, firstControl string) error {
	if payload == "" {
		return fmt.Errorf("empty kitty payload")
	}
	control := firstControl
	first := true
	for len(payload) > 0 {
		n := kittyChunkSize
		if len(payload) < n {
			n = len(payload)
		}
		chunk := payload[:n]
		payload = payload[n:]
		more := "0"
		if len(payload) > 0 {
			more = "1"
		}
		if first {
			if _, err := fmt.Fprintf(w, "\x1b_G%s,m=%s;%s\x1b\\", control, more, chunk); err != nil {
				return err
			}
			first = false
		} else {
			if _, err := fmt.Fprintf(w, "\x1b_Gm=%s;%s\x1b\\", more, chunk); err != nil {
				return err
			}
		}
	}
	return nil
}

func repeatNewline(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		buf = append(buf, '\r', '\n')
	}
	return string(buf)
}
