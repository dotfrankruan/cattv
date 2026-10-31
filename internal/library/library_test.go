package library

import (
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 9), G: uint8(y * 9), B: 100, A: 255})
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

func TestImportDirectoriesAndDeduplicate(t *testing.T) {
	dir := t.TempDir()
	pictures := filepath.Join(dir, "pictures")
	writePNG(t, filepath.Join(pictures, "a.png"), 20, 10)
	writePNG(t, filepath.Join(pictures, "nested", "b.png"), 10, 20)
	if err := os.WriteFile(filepath.Join(pictures, "broken.png"), []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pictures, "ignore.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewStore(filepath.Join(dir, "library.json"))
	idx, report, err := store.Import([]string{pictures, filepath.Join(pictures, "a.png")})
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 2 || report.Updated != 0 || report.Skipped != 1 {
		t.Fatalf("report = %+v", report)
	}
	if len(idx.Items) != 2 {
		t.Fatalf("items = %#v", idx.Items)
	}
	if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "broken.png") {
		t.Fatalf("warnings = %#v", report.Warnings)
	}
	for _, item := range idx.Items {
		if !filepath.IsAbs(item.Path) || item.Width < 1 || item.Height < 1 || item.ID == "" {
			t.Fatalf("bad item: %+v", item)
		}
	}

	// Re-importing the same trees updates rather than duplicating.
	idx, report, err = store.Import([]string{pictures})
	if err != nil {
		t.Fatal(err)
	}
	if report.Added != 0 || report.Updated != 2 || len(idx.Items) != 2 {
		t.Fatalf("reimport report = %+v, items = %d", report, len(idx.Items))
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Items) != 2 {
		t.Fatalf("loaded %d items", len(loaded.Items))
	}
}

func TestCandidatesAndRemove(t *testing.T) {
	dir := t.TempDir()
	pictures := filepath.Join(dir, "pictures")
	writePNG(t, filepath.Join(pictures, "a.png"), 4, 4)
	writePNG(t, filepath.Join(pictures, "b.png"), 4, 4)
	store := NewStore(filepath.Join(dir, "library.json"))
	idx, _, err := store.Import([]string{pictures})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Items) != 2 {
		t.Fatalf("items = %d", len(idx.Items))
	}

	got := idx.Candidates(1, rand.New(rand.NewSource(1)), map[string]bool{idx.Items[0].ID: true})
	if len(got) != 1 || got[0].ID != idx.Items[1].ID {
		t.Fatalf("candidates = %#v", got)
	}
	if got := idx.Candidates(1, rand.New(rand.NewSource(1)), map[string]bool{idx.Items[0].ID: true, idx.Items[1].ID: true}); len(got) != 0 {
		t.Fatalf("expected no candidates, got %#v", got)
	}

	removedID := idx.Items[0].ID
	idx, removed, err := store.Remove(removedID)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d", removed)
	}
	idx, removed, err = store.Remove(idx.Items[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 || len(idx.Items) != 0 {
		t.Fatalf("removed = %d, items = %d", removed, len(idx.Items))
	}
}

func TestImportMissingPathWarns(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "library.json"))
	_, report, err := store.Import([]string{filepath.Join(t.TempDir(), "missing")})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 1 || report.Added != 0 {
		t.Fatalf("report = %+v", report)
	}
}
