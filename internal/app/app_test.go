package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotfrankruan/cattv/internal/catalog"
	"github.com/dotfrankruan/cattv/internal/fetch"
	"github.com/dotfrankruan/cattv/internal/library"
	"github.com/dotfrankruan/cattv/internal/render"
	"github.com/dotfrankruan/cattv/internal/state"
	"github.com/dotfrankruan/cattv/internal/terminal"
	"github.com/dotfrankruan/cattv/internal/textsafe"
)

type fetchFunc func(context.Context, string) ([]byte, error)

func (f fetchFunc) Get(ctx context.Context, url string) ([]byte, error) {
	return f(ctx, url)
}

func samplePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 16, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 15), G: uint8(y * 25), B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testPhoto(id string, curated bool) catalog.Photo {
	base := "https://images.example/" + id
	return catalog.Photo{
		Kind:        catalog.PhotoKind,
		ID:          id,
		PageURL:     "https://unsplash.com/photos/" + id,
		URL320:      base + "?w=320",
		URL640:      base + "?w=640",
		URL1280:     base + "?w=1280",
		Attribution: "Photo by Test on Unsplash",
		Curated:     curated,
	}
}

func testOptions(catalogPath, statePath string) Options {
	return Options{
		CatalogPath: catalogPath,
		StatePath:   statePath,
		Renderer:    render.KindANSI,
		Geometry: terminal.Geometry{
			Columns: 60, Rows: 18, CellWidth: 8, CellHeight: 16, PixelWidth: 480, PixelHeight: 288,
		},
		NoCaption: true,
		Out:       &bytes.Buffer{},
		Log:       &bytes.Buffer{},
		Rand:      rand.New(rand.NewSource(42)),
	}
}

func saveTestCatalog(t *testing.T, path string, photos ...catalog.Photo) {
	t.Helper()
	if _, err := catalog.Save(path, catalog.Meta{}, photos); err != nil {
		t.Fatal(err)
	}
}

func TestNextFetchesAndAvoidsRepeat(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.gz")
	statePath := filepath.Join(dir, "state.json")
	saveTestCatalog(t, catPath, testPhoto("one", true), testPhoto("two", true))
	data := samplePNG(t)
	var fetched []string
	fetcher := fetchFunc(func(_ context.Context, url string) ([]byte, error) {
		fetched = append(fetched, url)
		return data, nil
	})
	opts := testOptions(catPath, statePath)
	a, err := New(opts, fetcher)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || second.ID == "" {
		t.Fatalf("missing IDs: %q %q", first.ID, second.ID)
	}
	if first.ID == second.ID {
		t.Fatalf("repeated photo %s while an alternative existed", first.ID)
	}
	if len(fetched) != 2 {
		t.Fatalf("fetches = %d", len(fetched))
	}
	// ANSI at 60 columns requests the smallest appropriate 320px variant.
	if !strings.HasSuffix(fetched[0], "w=320") {
		t.Fatalf("requested %q, want 320px variant", fetched[0])
	}
}

func TestNextExcludesRecentPhotoIDs(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.gz")
	statePath := filepath.Join(dir, "state.json")
	saveTestCatalog(t, catPath, testPhoto("one", true), testPhoto("two", true))
	if err := state.NewStore(statePath, 32).MarkSeen(state.RemoteKey("one")); err != nil {
		t.Fatal(err)
	}
	data := samplePNG(t)
	a, err := New(testOptions(catPath, statePath), fetchFunc(func(context.Context, string) ([]byte, error) {
		return data, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "two" {
		t.Fatalf("selected %q despite recent history excluding it", got.ID)
	}
}

func TestNextReportsOffline(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.gz")
	saveTestCatalog(t, catPath, testPhoto("one", true))
	a, err := New(testOptions(catPath, filepath.Join(dir, "state.json")),
		fetchFunc(func(context.Context, string) ([]byte, error) {
			return nil, &fetch.Error{Kind: fetch.KindNetwork, Err: errors.New("dial failed")}
		}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Next(context.Background())
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "dial failed") {
		t.Fatalf("offline error lost detail: %v", err)
	}
}

func TestNextReportsMissingCatalog(t *testing.T) {
	dir := t.TempDir()
	a, err := New(testOptions(filepath.Join(dir, "missing.gz"), filepath.Join(dir, "state.json")),
		fetchFunc(func(context.Context, string) ([]byte, error) { return nil, errors.New("unused") }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Next(context.Background())
	if !errors.Is(err, ErrCatalogMissing) {
		t.Fatalf("error = %v", err)
	}
}

func TestNextFallsBackToAnotherImageURL(t *testing.T) {
	var goodPNG = samplePNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/bad") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("this is not a png"))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(goodPNG)
	}))
	defer srv.Close()

	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.gz")
	// Only a 1280 URL and a base URL are supplied. The first decodes badly;
	// the second must be attempted before giving up on the photo.
	photo := catalog.Photo{
		Kind:        catalog.PhotoKind,
		ID:          "fallback",
		PageURL:     "https://unsplash.com/photos/fallback",
		ImageURL:    srv.URL + "/good.png",
		URL1280:     srv.URL + "/bad.png",
		Attribution: "Photo by Test",
		Curated:     true,
	}
	saveTestCatalog(t, catPath, photo)
	client := fetch.New(2*time.Second, 0, 1<<20, "cattv/test")
	a, err := New(testOptions(catPath, filepath.Join(dir, "state.json")), client)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "fallback" {
		t.Fatalf("ID = %q", got.ID)
	}
}

func TestNextReturnsNoImageForCorruptPayload(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.gz")
	saveTestCatalog(t, catPath, testPhoto("one", true))
	a, err := New(testOptions(catPath, filepath.Join(dir, "state.json")),
		fetchFunc(func(context.Context, string) ([]byte, error) { return []byte("not image"), nil }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Next(context.Background())
	if !errors.Is(err, ErrNoImage) {
		t.Fatalf("error = %v", err)
	}
}

func TestShowFileDoesNotNeedNetwork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cat.png")
	if err := writeImageFile(path, samplePNG(t)); err != nil {
		t.Fatal(err)
	}
	opts := testOptions("", "")
	opts.Renderer = render.KindANSI
	a, err := New(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.ShowFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "file" || !strings.Contains(got.Caption, "16×10") {
		t.Fatalf("shown = %+v", got)
	}
	if _, err := a.ShowFile(filepath.Join(dir, "missing.png")); err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestLocalAvoidsRepeat(t *testing.T) {
	dir := t.TempDir()
	pictures := filepath.Join(dir, "pictures")
	if err := writeImageFile(filepath.Join(pictures, "one.png"), samplePNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := writeImageFile(filepath.Join(pictures, "two.png"), samplePNG(t)); err != nil {
		t.Fatal(err)
	}
	store := library.NewStore(filepath.Join(dir, "library.json"))
	if _, _, err := store.Import([]string{pictures}); err != nil {
		t.Fatal(err)
	}
	opts := testOptions("", filepath.Join(dir, "state.json"))
	opts.LibraryPath = filepath.Join(dir, "library.json")
	a, err := New(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.Local(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Local(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("repeated local image %s", first.ID)
	}
}

func TestLocalExcludesRecentLibraryIDs(t *testing.T) {
	dir := t.TempDir()
	pictures := filepath.Join(dir, "pictures")
	if err := writeImageFile(filepath.Join(pictures, "one.png"), samplePNG(t)); err != nil {
		t.Fatal(err)
	}
	if err := writeImageFile(filepath.Join(pictures, "two.png"), samplePNG(t)); err != nil {
		t.Fatal(err)
	}
	libraryPath := filepath.Join(dir, "library.json")
	store := library.NewStore(libraryPath)
	idx, _, err := store.Import([]string{pictures})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Items) != 2 {
		t.Fatalf("items = %d", len(idx.Items))
	}
	statePath := filepath.Join(dir, "state.json")
	if err := state.NewStore(statePath, 32).MarkSeen(state.LocalKey(idx.Items[0].ID)); err != nil {
		t.Fatal(err)
	}
	opts := testOptions("", statePath)
	opts.LibraryPath = libraryPath
	a, err := New(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.Local(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != idx.Items[1].ID {
		t.Fatalf("selected recent local id %s, want %s", got.ID, idx.Items[1].ID)
	}
}

func TestLocalEmptyLibrary(t *testing.T) {
	dir := t.TempDir()
	opts := testOptions("", filepath.Join(dir, "state.json"))
	opts.LibraryPath = filepath.Join(dir, "library.json")
	a, err := New(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Local(context.Background())
	if !errors.Is(err, ErrLibraryEmpty) {
		t.Fatalf("error = %v", err)
	}
}

func TestTerminalTooSmall(t *testing.T) {
	dir := t.TempDir()
	catPath := filepath.Join(dir, "catalog.gz")
	saveTestCatalog(t, catPath, testPhoto("one", true))
	opts := testOptions(catPath, filepath.Join(dir, "state.json"))
	opts.Geometry.Columns = 5
	a, err := New(opts, fetchFunc(func(context.Context, string) ([]byte, error) { return nil, errors.New("unused") }))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Next(context.Background())
	if !errors.Is(err, ErrTerminalSmall) {
		t.Fatalf("error = %v", err)
	}
}

func TestSanitizeCaption(t *testing.T) {
	got := textsafe.Terminal("Photo by\x1b[31m Evil\nName\t")
	if strings.ContainsRune(got, '\x1b') || strings.ContainsRune(got, '\n') || strings.ContainsRune(got, '\t') {
		t.Fatalf("unsafe caption: %q", got)
	}
}

func writeImageFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}
