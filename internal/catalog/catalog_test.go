package catalog

import (
	"compress/gzip"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPhoto(id string, curated bool) Photo {
	return Photo{
		Kind:         PhotoKind,
		ID:           id,
		PageURL:      "https://unsplash.com/photos/" + id,
		URL320:       "https://images.example/" + id + "?w=320",
		URL640:       "https://images.example/" + id + "?w=640",
		URL1280:      "https://images.example/" + id + "?w=1280",
		Width:        1600,
		Height:       1067,
		Attribution:  "Photo by Test on Unsplash",
		QualityScore: 75,
		Curated:      curated,
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.jsonl.gz")
	photos := []Photo{testPhoto("one", true), testPhoto("two", false), testPhoto("three", true)}
	meta, err := Save(path, Meta{Source: "/data/cats.parquet", AppVersion: "test"}, photos)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Records != 3 || meta.Curated != 2 {
		t.Fatalf("meta counts = %d/%d", meta.Records, meta.Curated)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	total, curated := loaded.Count()
	if total != 3 || curated != 2 {
		t.Fatalf("counts = %d/%d", total, curated)
	}
	if loaded.Meta.Source != "/data/cats.parquet" || loaded.Meta.Schema != SchemaVersion {
		t.Fatalf("meta = %+v", loaded.Meta)
	}
	if loaded.Photos[1].Credit() != "Photo by Test on Unsplash" {
		t.Fatalf("credit = %q", loaded.Photos[1].Credit())
	}
}

func TestLoadRejectsIncompleteCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.jsonl.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	_, _ = io.WriteString(gz, `{"kind":"photo","id":"one","url_320":"https://example/one.jpg"}`+"\n")
	_, _ = io.WriteString(gz, `{"kind":"meta","schema":1,"records":2,"curated":0}`+"\n")
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("error = %v", err)
	}
}

func TestCandidatesPreferCuratedAndAvoidRecent(t *testing.T) {
	photos := []Photo{
		testPhoto("a", true), testPhoto("b", true), testPhoto("c", true),
		testPhoto("x", false), testPhoto("y", false),
	}
	c := &Catalog{Photos: photos}
	c.index()
	rng := rand.New(rand.NewSource(7))
	got := c.Candidates(2, rng, map[string]bool{"b": true})
	if len(got) != 2 {
		t.Fatalf("got %d candidates", len(got))
	}
	for _, p := range got {
		if !p.Curated {
			t.Fatalf("selected non-curated photo while curated alternatives exist: %s", p.ID)
		}
		if p.ID == "b" {
			t.Fatal("selected excluded photo")
		}
	}

	allCuratedExcluded := map[string]bool{"a": true, "b": true, "c": true}
	got = c.Candidates(2, rng, allCuratedExcluded)
	if len(got) != 2 {
		t.Fatalf("fallback got %d candidates", len(got))
	}
	for _, p := range got {
		if p.Curated {
			t.Fatalf("selected excluded photo %s", p.ID)
		}
	}
}

func TestCandidatesDoNotRepeat(t *testing.T) {
	c := &Catalog{Photos: []Photo{testPhoto("a", true), testPhoto("b", true)}}
	c.index()
	got := c.Candidates(8, rand.New(rand.NewSource(1)), nil)
	if len(got) != 2 {
		t.Fatalf("got %d candidates", len(got))
	}
	if got[0].ID == got[1].ID {
		t.Fatal("duplicate candidate")
	}
}

func TestBestImageURL(t *testing.T) {
	p := testPhoto("one", true)
	tests := []struct {
		target int
		want   string
	}{
		{target: 0, want: p.URL1280},
		{target: 200, want: p.URL320},
		{target: 640, want: p.URL640},
		{target: 1000, want: p.URL1280},
		{target: 5000, want: p.URL1280},
	}
	for _, tt := range tests {
		if got := p.BestImageURL(tt.target); got != tt.want {
			t.Errorf("target %d: got %q, want %q", tt.target, got, tt.want)
		}
	}
}

func TestBestImageURLUsesBoundedDerivedURL(t *testing.T) {
	p := Photo{ImageURL: "https://images.unsplash.com/photo-123"}
	got := p.BestImageURL(5000)
	if !strings.HasPrefix(got, "https://images.unsplash.com/photo-123?") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "w=1280") {
		t.Fatalf("derived URL is not bounded: %q", got)
	}
}

func TestCandidateImageURLsFallbackChain(t *testing.T) {
	p := testPhoto("one", true)
	got := p.CandidateImageURLs(1000)
	want := []string{p.URL1280, p.URL640, p.URL320}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fallback[%d] = %q, want %q (all: %#v)", i, got[i], want[i], got)
		}
	}
}

func TestCandidateImageURLsIncludesLargerVariant(t *testing.T) {
	p := testPhoto("one", true)
	got := p.CandidateImageURLs(320)
	want := []string{p.URL320, p.URL640, p.URL1280}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fallback[%d] = %q, want %q (all: %#v)", i, got[i], want[i], got)
		}
	}
}

func TestCreditFallbacks(t *testing.T) {
	p := Photo{Photographer: "Alice", PageURL: "https://unsplash.com/photos/x"}
	if got := p.Credit(); !strings.Contains(got, "Alice") || !strings.Contains(got, "https://unsplash.com/photos/x") {
		t.Fatalf("credit = %q", got)
	}
	p = Photo{}
	if got := p.Credit(); !strings.Contains(got, "Unsplash") {
		t.Fatalf("credit = %q", got)
	}
}

func TestParseDuckDBPhotos(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"id":"a","url_320":"https://example/a.jpg","curated":true}`,
		`{"id":"b","image_url":"https://images.unsplash.com/photo-b","photographer":"Bob"}`,
	}, "\n"))
	photos, err := parseDuckDBPhotos(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(photos) != 2 || photos[1].Photographer != "Bob" {
		t.Fatalf("photos = %#v", photos)
	}
}

func TestParseDuckDBPhotosRejectsDuplicates(t *testing.T) {
	input := strings.NewReader(`{"id":"a","url_320":"https://example/a.jpg"}
{"id":"a","url_640":"https://example/a2.jpg"}
`)
	if _, err := parseDuckDBPhotos(input); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestSQLiteral(t *testing.T) {
	got, err := sqlLiteral("/data/Frank's cats/cats.parquet")
	if err != nil {
		t.Fatal(err)
	}
	if got != "'/data/Frank''s cats/cats.parquet'" {
		t.Fatalf("got %q", got)
	}
	if _, err := sqlLiteral("bad\x00path"); err == nil {
		t.Fatal("expected NUL error")
	}
}

func TestMetaCreatedAtPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.jsonl.gz")
	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	meta, err := Save(path, Meta{CreatedAt: want}, []Photo{testPhoto("a", true)})
	if err != nil {
		t.Fatal(err)
	}
	if !meta.CreatedAt.Equal(want) {
		t.Fatalf("created at = %v", meta.CreatedAt)
	}
}

func TestLoadRejectsInvalidImageURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.jsonl.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	_, _ = io.WriteString(gz, `{"kind":"photo","id":"bad","url_320":"not-a-url"}`+"\n")
	_, _ = io.WriteString(gz, `{"kind":"meta","schema":1,"records":1,"curated":0}`+"\n")
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error = %v", err)
	}
}

func TestSaveRejectsInvalidImageURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.jsonl.gz")
	bad := testPhoto("bad", true)
	bad.URL320 = "file:///tmp/cat.png"
	if _, err := Save(path, Meta{}, []Photo{bad}); err == nil {
		t.Fatal("expected URL validation error")
	}
}
