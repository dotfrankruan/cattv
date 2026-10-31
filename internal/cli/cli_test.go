package cli

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotfrankruan/cattv/internal/catalog"
)

func runTestCLI(t *testing.T, dataDir string, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("CATTV_DATA_DIR", dataDir)
	t.Setenv("CATTV_CATALOG", "")
	t.Setenv("CATTV_LIBRARY", "")
	t.Setenv("CATTV_STATE", "")
	t.Setenv("CATTV_PARQUET", "")
	t.Setenv("CATTV_DUCKDB", "")
	t.Setenv("CATTV_RENDERER", "")
	t.Setenv("CATTV_TIMEOUT", "")
	t.Setenv("CATTV_RETRIES", "")
	t.Setenv("CATTV_NO_PROBE", "1")
	t.Setenv("COLUMNS", "80")
	t.Setenv("LINES", "24")

	outFile, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatal(err)
	}
	defer outFile.Close()
	var errOut bytes.Buffer
	code := Run(context.Background(), args, os.Stdin, outFile, &errOut)
	if _, err := outFile.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, string(data), errOut.String()
}

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 12; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 20), G: uint8(y * 30), B: 200, A: 255})
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestHelpAndVersion(t *testing.T) {
	dir := t.TempDir()
	code, out, errOut := runTestCLI(t, dir, "help")
	if code != exitOK || errOut != "" {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	for _, want := range []string{"cattv show", "cattv init", "cattv doctor"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
	code, out, errOut = runTestCLI(t, dir, "version")
	if code != exitOK || errOut != "" || !strings.Contains(out, "cattv ") {
		t.Fatalf("version: code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestNextWithoutCatalogIsHelpful(t *testing.T) {
	code, out, errOut := runTestCLI(t, t.TempDir())
	if code != exitSetup {
		t.Fatalf("code = %d, stderr = %q", code, errOut)
	}
	if out != "" {
		t.Fatalf("unexpected stdout: %q", out)
	}
	if !strings.Contains(errOut, "cattv init") || !strings.Contains(errOut, "catalog") {
		t.Fatalf("unhelpful error: %q", errOut)
	}
}

func TestInitWithoutParquet(t *testing.T) {
	code, _, errOut := runTestCLI(t, t.TempDir(), "init")
	if code != exitUsage {
		t.Fatalf("code = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(errOut, "no Parquet dataset supplied") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestShowLocalImageWithTrailingFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cat.png")
	writeTestPNG(t, path)
	code, out, errOut := runTestCLI(t, dir, "show", path, "--renderer", "ansi", "--width", "20", "--height", "10", "--no-caption")
	if code != exitOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "\x1b[38;2;") || !strings.Contains(out, "▀") {
		t.Fatalf("show did not emit ANSI image output: %q", out[:minInt(len(out), 80)])
	}
	if filepath.Base(path) == "" {
		t.Fatal("unreachable")
	}
}

func TestImportListAndLocal(t *testing.T) {
	dir := t.TempDir()
	pictures := filepath.Join(dir, "pictures")
	writeTestPNG(t, filepath.Join(pictures, "one.png"))
	writeTestPNG(t, filepath.Join(pictures, "nested", "two.png"))

	code, out, errOut := runTestCLI(t, dir, "import", pictures)
	if code != exitOK {
		t.Fatalf("import code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "Imported 2 new image(s)") {
		t.Fatalf("import output = %q", out)
	}

	code, out, errOut = runTestCLI(t, dir, "library", "list")
	if code != exitOK {
		t.Fatalf("list code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "one.png") || !strings.Contains(out, "two.png") {
		t.Fatalf("list output = %q", out)
	}

	code, out, errOut = runTestCLI(t, dir, "local", "--renderer", "ansi", "--width", "20", "--height", "10", "--no-caption")
	if code != exitOK {
		t.Fatalf("local code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(out, "▀") {
		t.Fatalf("local did not render an image")
	}
}

func TestInvalidRendererIsUsageError(t *testing.T) {
	code, _, errOut := runTestCLI(t, t.TempDir(), "show", "whatever.png", "--renderer", "sixel")
	if code != exitUsage {
		t.Fatalf("code = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(errOut, "unknown renderer") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestRemoteOfflineError(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.jsonl.gz")
	photo := catalog.Photo{
		Kind:        catalog.PhotoKind,
		ID:          "offline",
		PageURL:     "https://unsplash.com/photos/offline",
		URL320:      "http://127.0.0.1:1/cat.jpg",
		Attribution: "Photo by Test",
		Curated:     true,
	}
	if _, err := catalog.Save(catPath, catalog.Meta{}, []catalog.Photo{photo}); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runTestCLI(t, dir, "next", "--renderer", "ansi", "--timeout", "2s", "--retries", "0")
	if code != exitNetwork {
		t.Fatalf("code = %d, stderr = %q", code, errOut)
	}
	if !strings.Contains(errOut, "network connection") && !strings.Contains(errOut, "network unavailable") {
		t.Fatalf("offline guidance missing: %q", errOut)
	}
}

func TestDoctor(t *testing.T) {
	code, out, errOut := runTestCLI(t, t.TempDir(), "doctor")
	if code != exitOK {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	for _, want := range []string{"Renderer:", "Catalog:", "Terminal:", "Image cache:"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor missing %q", want)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
