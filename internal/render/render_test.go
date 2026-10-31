package render

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"
)

func testImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 255 / max(1, w-1)),
				G: uint8(y * 255 / max(1, h-1)),
				B: 128,
				A: uint8(128 + (x+y)%128),
			})
		}
	}
	return img
}

func TestANSIRender(t *testing.T) {
	var out bytes.Buffer
	result, err := ansiRenderer{}.Render(&out, testImage(400, 300), Env{
		Columns: 80, Rows: 24, CellWidth: 8, CellHeight: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 400x300 fits a 640x384 physical box at 512x384, or 64 columns by 24 rows.
	if result.Columns != 64 || result.Rows != 24 {
		t.Fatalf("result = %+v", result)
	}
	s := out.String()
	if !strings.Contains(s, "▀") {
		t.Fatal("output does not use half blocks")
	}
	if !strings.Contains(s, "\x1b[38;2;") || !strings.Contains(s, "\x1b[48;2;") {
		t.Fatal("output is not true-colour ANSI")
	}
	if got := strings.Count(s, "\r\n"); got != result.Rows {
		t.Fatalf("line count = %d, want %d", got, result.Rows)
	}
	if !strings.HasSuffix(s, "\x1b[0m\r\n") {
		t.Fatal("output does not reset terminal attributes")
	}
}

func TestANSIPhysicalAspectForPortrait(t *testing.T) {
	var out bytes.Buffer
	result, err := ansiRenderer{}.Render(&out, testImage(300, 600), Env{
		Columns: 80, Rows: 20, CellWidth: 8, CellHeight: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Columns != 20 || result.Rows != 20 {
		t.Fatalf("portrait fit = %+v", result)
	}
}

func TestKittyRenderProtocol(t *testing.T) {
	var out bytes.Buffer
	result, err := kittyRenderer{}.Render(&out, testImage(400, 300), Env{
		Columns: 80, Rows: 24, CellWidth: 8, CellHeight: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Columns < 1 || result.Rows < 1 {
		t.Fatalf("bad result: %+v", result)
	}
	s := out.String()
	if !strings.HasPrefix(s, "\x1b_G") {
		t.Fatalf("missing kitty APC prefix: %q", s[:min(30, len(s))])
	}
	if !strings.Contains(s, "a=T,f=100,t=d") || !strings.Contains(s, "C=1") {
		t.Fatalf("missing key kitty controls: %q", s[:min(200, len(s))])
	}
	if !strings.Contains(s, "c="+itoa(result.Columns)) || !strings.Contains(s, "r="+itoa(result.Rows)) {
		t.Fatal("missing display dimensions")
	}
	payload := extractKittyPayload(t, s)
	img, format, err := image.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decode transmitted PNG: %v", err)
	}
	if format != "png" || img.Bounds().Dx() != 400 || img.Bounds().Dy() != 300 {
		t.Fatalf("transmitted image = %s %v", format, img.Bounds())
	}
	if !strings.HasSuffix(s, strings.Repeat("\r\n", result.Rows)) {
		t.Fatal("kitty renderer did not reserve its rows")
	}
}

func TestKittyChunking(t *testing.T) {
	var out bytes.Buffer
	payload := strings.Repeat("QUJD", 5000) // 20,000 chars
	if err := writeKittyChunks(&out, payload, "a=T,f=100,t=d,q=2"); err != nil {
		t.Fatal(err)
	}
	chunks := parseKittyChunks(t, out.String())
	if len(chunks) != 5 {
		t.Fatalf("chunks = %d, want 5", len(chunks))
	}
	var joined strings.Builder
	for i, c := range chunks {
		if len(c.data) > kittyChunkSize {
			t.Fatalf("chunk %d has %d bytes", i, len(c.data))
		}
		if len(c.data)%4 != 0 && i != len(chunks)-1 {
			t.Fatalf("non-final chunk %d is not base64-aligned", i)
		}
		if i == 0 {
			if !strings.Contains(c.control, "a=T") || !strings.Contains(c.control, "m=1") {
				t.Fatalf("first control = %q", c.control)
			}
		} else {
			wantMore := "1"
			if i == len(chunks)-1 {
				wantMore = "0"
			}
			if c.control != "m="+wantMore {
				t.Fatalf("chunk %d control = %q", i, c.control)
			}
		}
		joined.WriteString(c.data)
	}
	if joined.String() != payload {
		t.Fatal("chunk payload corrupted")
	}
}

func TestITerm2RenderProtocol(t *testing.T) {
	var out bytes.Buffer
	result, err := iTerm2Renderer{}.Render(&out, testImage(200, 100), Env{
		Columns: 80, Rows: 24, CellWidth: 8, CellHeight: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	prefix := "\x1b]1337;File=inline=1;"
	s := out.String()
	if !strings.HasPrefix(s, prefix) {
		t.Fatalf("missing OSC 1337 prefix: %q", s[:min(40, len(s))])
	}
	if !strings.Contains(s, "preserveAspectRatio=1") {
		t.Fatal("missing aspect ratio control")
	}
	colon := strings.Index(s, ":")
	bel := strings.LastIndex(s, "\a")
	if colon < 0 || bel < 0 || bel <= colon {
		t.Fatalf("malformed OSC sequence")
	}
	data := s[colon+1 : bel]
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		t.Fatal(err)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 100 {
		t.Fatalf("image bounds = %v", img.Bounds())
	}
	if result.Columns < 1 || result.Rows < 1 {
		t.Fatalf("bad result: %+v", result)
	}
	if !strings.HasSuffix(s, "\r\n") {
		t.Fatal("missing trailing CRLF")
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	if _, _, err := DecodeBytes([]byte("not an image")); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDecodePNG(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, testImage(3, 2)); err != nil {
		t.Fatal(err)
	}
	img, format, err := DecodeBytes(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if format != "png" || img.Bounds().Dx() != 3 || img.Bounds().Dy() != 2 {
		t.Fatalf("decoded %s %v", format, img.Bounds())
	}
}

func TestRendererWriteError(t *testing.T) {
	_, err := ansiRenderer{}.Render(errorWriter{}, testImage(20, 20), Env{Columns: 10, Rows: 5, CellWidth: 8, CellHeight: 16})
	if err == nil {
		t.Fatal("expected write error")
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type kittyChunk struct {
	control string
	data    string
}

func parseKittyChunks(t *testing.T, s string) []kittyChunk {
	t.Helper()
	var chunks []kittyChunk
	for {
		start := strings.Index(s, "\x1b_G")
		if start < 0 {
			break
		}
		s = s[start+3:]
		end := strings.Index(s, "\x1b\\")
		if end < 0 {
			t.Fatalf("unterminated kitty chunk")
		}
		body := s[:end]
		s = s[end+2:]
		control, data, ok := strings.Cut(body, ";")
		if !ok {
			t.Fatalf("kitty chunk has no payload separator: %q", body)
		}
		chunks = append(chunks, kittyChunk{control: control, data: data})
	}
	return chunks
}

func extractKittyPayload(t *testing.T, s string) []byte {
	t.Helper()
	var joined strings.Builder
	for _, c := range parseKittyChunks(t, s) {
		joined.WriteString(c.data)
	}
	raw, err := base64.StdEncoding.DecodeString(joined.String())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
