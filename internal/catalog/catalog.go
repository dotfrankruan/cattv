// Package catalog stores the local, compact photo metadata index used for
// fast random selection. It intentionally does not store image pixels.
package catalog

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dotfrankruan/cattv/internal/fileutil"
)

const (
	// SchemaVersion identifies the on-disk catalog format. Version 2 adds
	// optional capture location and submission date metadata. Version 1
	// catalogs remain readable but display no location/date detail.
	SchemaVersion = 2
	// MetaKind is the JSONL discriminator for catalog metadata.
	MetaKind = "meta"
	// PhotoKind is the JSONL discriminator for a photo record.
	PhotoKind = "photo"
)

// Meta describes a catalog import. It is written as the final JSONL record so
// a streaming import never needs to buffer or rewrite photo records.
type Meta struct {
	Kind        string    `json:"kind"`
	Schema      int       `json:"schema"`
	CreatedAt   time.Time `json:"created_at"`
	Source      string    `json:"source"`
	Records     int       `json:"records"`
	Curated     int       `json:"curated"`
	AppVersion  string    `json:"app_version,omitempty"`
	Description string    `json:"description,omitempty"`
}

// Photo is the runtime metadata for one remote photograph. Image pixels are
// always fetched on demand and are never stored in the catalog.
type Photo struct {
	Kind                 string   `json:"kind"`
	ID                   string   `json:"id"`
	PageURL              string   `json:"page_url,omitempty"`
	ImageURL             string   `json:"image_url,omitempty"`
	URL320               string   `json:"url_320,omitempty"`
	URL640               string   `json:"url_640,omitempty"`
	URL1280              string   `json:"url_1280,omitempty"`
	Width                int      `json:"width,omitempty"`
	Height               int      `json:"height,omitempty"`
	Orientation          string   `json:"orientation,omitempty"`
	Description          string   `json:"description,omitempty"`
	Attribution          string   `json:"attribution,omitempty"`
	Photographer         string   `json:"photographer,omitempty"`
	PhotographerUsername string   `json:"photographer_username,omitempty"`
	Date                 string   `json:"date,omitempty"`
	LocationName         string   `json:"location_name,omitempty"`
	Country              string   `json:"country,omitempty"`
	City                 string   `json:"city,omitempty"`
	Latitude             *float64 `json:"latitude,omitempty"`
	Longitude            *float64 `json:"longitude,omitempty"`
	QualityScore         float64  `json:"quality_score,omitempty"`
	QualityTier          string   `json:"quality_tier,omitempty"`
	Curated              bool     `json:"curated,omitempty"`
}

// Catalog is a fully loaded metadata index. Loading a few megabytes of JSONL
// is fast enough for a single-shot CLI and keeps the runtime dependency-free.
type Catalog struct {
	Meta   Meta
	Photos []Photo

	curated []int
}

// URLVariant is a supplied image URL and its nominal pixel width.
type URLVariant struct {
	Width int
	URL   string
}

// Load decodes and validates a gzipped JSONL catalog.
func Load(path string) (*Catalog, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("open catalog %s: %w", path, err)
	}
	defer gz.Close()

	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	var (
		meta     *Meta
		photos   []Photo
		seen     = make(map[string]struct{})
		line     int
		curated  int
		firstErr error
	)
	for scanner.Scan() {
		line++
		var head struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &head); err != nil {
			firstErr = fmt.Errorf("catalog line %d: %w", line, err)
			break
		}
		switch head.Kind {
		case MetaKind:
			if meta != nil {
				firstErr = fmt.Errorf("catalog line %d: duplicate metadata record", line)
				break
			}
			var m Meta
			if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
				firstErr = fmt.Errorf("catalog line %d: %w", line, err)
				break
			}
			if m.Schema < 1 || m.Schema > SchemaVersion {
				firstErr = fmt.Errorf("catalog schema %d is not supported (supported: 1-%d); re-run cattv init", m.Schema, SchemaVersion)
				break
			}
			meta = &m
		case PhotoKind:
			var p Photo
			if err := json.Unmarshal(scanner.Bytes(), &p); err != nil {
				firstErr = fmt.Errorf("catalog line %d: %w", line, err)
				break
			}
			p.Kind = PhotoKind
			if err := p.validate(); err != nil {
				firstErr = fmt.Errorf("catalog line %d: %w", line, err)
				break
			}
			if _, ok := seen[p.ID]; ok {
				firstErr = fmt.Errorf("catalog line %d: duplicate photo id %s", line, p.ID)
				break
			}
			seen[p.ID] = struct{}{}
			if p.Curated {
				curated++
			}
			photos = append(photos, p)
		default:
			firstErr = fmt.Errorf("catalog line %d: unknown record kind %q", line, head.Kind)
		}
		if firstErr != nil {
			break
		}
	}
	if firstErr == nil {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read catalog: %w", err)
		}
		if meta == nil {
			return nil, errors.New("catalog has no metadata record; re-run cattv init")
		}
		if len(photos) == 0 {
			return nil, errors.New("catalog contains no photos; re-run cattv init")
		}
		if meta.Records != len(photos) {
			return nil, fmt.Errorf("catalog is incomplete: metadata says %d photos, found %d", meta.Records, len(photos))
		}
		if meta.Curated != curated {
			return nil, fmt.Errorf("catalog is inconsistent: metadata says %d curated photos, found %d", meta.Curated, curated)
		}
		c := &Catalog{Meta: *meta, Photos: photos}
		c.index()
		return c, nil
	}

	// Deliberately ignore scanner/close errors when a structural error already
	// gives the user something actionable.
	return nil, firstErr
}

// Save atomically writes a gzipped JSONL catalog. Metadata is the final line,
// with counts derived from photos rather than trusted from the caller.
func Save(path string, meta Meta, photos []Photo) (Meta, error) {
	if len(photos) == 0 {
		return Meta{}, errors.New("refusing to save an empty catalog")
	}
	meta.Kind = MetaKind
	meta.Schema = SchemaVersion
	meta.Records = len(photos)
	meta.Curated = 0
	seen := make(map[string]struct{}, len(photos))
	for i := range photos {
		p := &photos[i]
		p.Kind = PhotoKind
		if err := p.validate(); err != nil {
			return Meta{}, fmt.Errorf("photo %d: %w", i+1, err)
		}
		if _, ok := seen[p.ID]; ok {
			return Meta{}, fmt.Errorf("duplicate photo id %s", p.ID)
		}
		seen[p.ID] = struct{}{}
		if p.Curated {
			meta.Curated++
		}
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}

	err := fileutil.WriteAtomic(path, 0o644, func(w io.Writer) error {
		gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(gz)
		enc.SetEscapeHTML(false)
		for i := range photos {
			if err := enc.Encode(&photos[i]); err != nil {
				_ = gz.Close()
				return fmt.Errorf("encode catalog: %w", err)
			}
		}
		if err := enc.Encode(&meta); err != nil {
			_ = gz.Close()
			return fmt.Errorf("encode catalog metadata: %w", err)
		}
		if err := gz.Close(); err != nil {
			return fmt.Errorf("finish compressed catalog: %w", err)
		}
		return nil
	})
	if err != nil {
		return Meta{}, err
	}
	return meta, nil
}

func (c *Catalog) index() {
	c.curated = c.curated[:0]
	for i := range c.Photos {
		if c.Photos[i].Curated {
			c.curated = append(c.curated, i)
		}
	}
}

// Count returns the number of photos and the number of curated picks.
func (c *Catalog) Count() (total, curated int) {
	return len(c.Photos), len(c.curated)
}

// Candidates returns up to n distinct random photos, excluding IDs in exclude.
// Curated picks are preferred; non-curated records are used only when there
// are not enough eligible curated picks. Uniform selection avoids turning the
// metadata quality score into an aesthetic popularity contest.
func (c *Catalog) Candidates(n int, rng *rand.Rand, exclude map[string]bool) []Photo {
	if n <= 0 || len(c.Photos) == 0 {
		return nil
	}
	if n > len(c.Photos) {
		n = len(c.Photos)
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	curated := c.eligible(c.curated, exclude)
	all := c.eligible(allIndices(len(c.Photos)), exclude)
	if len(curated) == 0 && len(all) == 0 {
		return nil
	}

	taken := make(map[int]bool, n)
	out := make([]Photo, 0, n)
	sample := func(pool []int) {
		if len(pool) == 0 || len(out) >= n {
			return
		}
		// Partial Fisher-Yates: O(k) where k is the number requested.
		for i := 0; i < len(pool) && len(out) < n; i++ {
			j := i + rng.Intn(len(pool)-i)
			pool[i], pool[j] = pool[j], pool[i]
			idx := pool[i]
			if taken[idx] || exclude[c.Photos[idx].ID] {
				continue
			}
			taken[idx] = true
			out = append(out, c.Photos[idx])
		}
	}
	sample(curated)
	sample(all)
	return out
}

func (c *Catalog) eligible(indices []int, exclude map[string]bool) []int {
	out := make([]int, 0, len(indices))
	for _, idx := range indices {
		if exclude[c.Photos[idx].ID] {
			continue
		}
		out = append(out, idx)
	}
	return out
}

func allIndices(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// BestImageURL chooses the smallest supplied variant that is at least
// targetWidth pixels, falling back to the largest supplied variant. If the
// source URL is known but no pre-sized variants exist, it constructs a bounded
// Unsplash CDN URL instead of ever requesting an original.
func (p Photo) BestImageURL(targetWidth int) string {
	variants := p.Variants()
	if len(variants) == 0 {
		return p.derivedURL(targetWidth)
	}
	if targetWidth <= 0 {
		targetWidth = 1280
	}
	for _, v := range variants {
		if v.Width >= targetWidth {
			return v.URL
		}
	}
	return variants[len(variants)-1].URL
}

// CandidateImageURLs returns all distinct supplied sizes in order of
// suitability, followed by the bounded base-URL variant. If the best size is
// unavailable, another size of the same photograph may still work.
func (p Photo) CandidateImageURLs(targetWidth int) []string {
	variants := p.Variants()
	if len(variants) == 0 {
		if u := p.derivedURL(targetWidth); u != "" {
			return []string{u}
		}
		return nil
	}
	if targetWidth <= 0 {
		targetWidth = 1280
	}
	seen := make(map[string]bool, len(variants)+1)
	out := make([]string, 0, len(variants)+1)
	appendURL := func(u string) {
		if u != "" && !seen[u] && len(out) < 4 {
			seen[u] = true
			out = append(out, u)
		}
	}
	best := p.BestImageURL(targetWidth)
	appendURL(best)

	rest := make([]URLVariant, 0, len(variants))
	for _, v := range variants {
		if v.URL != best {
			rest = append(rest, v)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		di := absInt(rest[i].Width - targetWidth)
		dj := absInt(rest[j].Width - targetWidth)
		if di == dj {
			return rest[i].Width > rest[j].Width
		}
		return di < dj
	})
	for _, v := range rest {
		appendURL(v.URL)
	}
	if p.ImageURL != "" {
		appendURL(p.derivedURL(targetWidth))
	}
	return out
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Variants returns non-empty supplied URL variants ordered by width.
func (p Photo) Variants() []URLVariant {
	out := []URLVariant{
		{Width: 320, URL: strings.TrimSpace(p.URL320)},
		{Width: 640, URL: strings.TrimSpace(p.URL640)},
		{Width: 1280, URL: strings.TrimSpace(p.URL1280)},
	}
	filtered := out[:0]
	for _, v := range out {
		if v.URL != "" {
			filtered = append(filtered, v)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Width < filtered[j].Width })
	return filtered
}

func (p Photo) validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("photo has no id")
	}
	urls := []struct {
		label string
		value string
	}{
		{"base", p.ImageURL},
		{"320", p.URL320},
		{"640", p.URL640},
		{"1280", p.URL1280},
	}
	usable := 0
	for _, candidate := range urls {
		raw := strings.TrimSpace(candidate.value)
		if raw == "" {
			continue
		}
		if len(raw) > 16*1024 {
			return fmt.Errorf("photo %s has an oversized %s URL", p.ID, candidate.label)
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("photo %s has an invalid %s URL", p.ID, candidate.label)
		}
		usable++
	}
	if usable == 0 {
		return fmt.Errorf("photo %s has no image URL", p.ID)
	}
	return nil
}

func (p Photo) derivedURL(targetWidth int) string {
	base := strings.TrimSpace(p.ImageURL)
	if base == "" {
		return ""
	}
	if targetWidth <= 0 {
		targetWidth = 1280
	}
	if targetWidth > 1280 {
		targetWidth = 1280
	}
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	q := u.Query()
	q.Set("w", fmt.Sprintf("%d", targetWidth))
	q.Set("q", "85")
	q.Set("fm", "jpg")
	u.RawQuery = q.Encode()
	return u.String()
}

// Place returns the most human-friendly location available. Textual names are
// preferred over raw coordinates. Location and date metadata are optional in
// both the source dataset and legacy catalogs.
func (p Photo) Place() string {
	if place := strings.TrimSpace(p.LocationName); place != "" {
		return place
	}
	city := strings.TrimSpace(p.City)
	country := strings.TrimSpace(p.Country)
	switch {
	case city != "" && country != "":
		return city + ", " + country
	case city != "":
		return city
	case country != "":
		return country
	}
	if p.Latitude != nil && p.Longitude != nil {
		return fmt.Sprintf("%.4f, %.4f", *p.Latitude, *p.Longitude)
	}
	return ""
}

// Detail returns optional location and submission-date text for the muted
// second part of the attribution caption.
func (p Photo) Detail() string {
	parts := make([]string, 0, 2)
	if place := p.Place(); place != "" {
		parts = append(parts, place)
	}
	if date := strings.TrimSpace(p.Date); date != "" {
		parts = append(parts, date)
	}
	return strings.Join(parts, " · ")
}

// Credit returns an attribution line suitable for display. The supplied
// attribution field wins because it may contain the photographer's preferred
// wording and link.
func (p Photo) Credit() string {
	if s := strings.TrimSpace(p.Attribution); s != "" {
		return s
	}
	name := strings.TrimSpace(p.Photographer)
	if name == "" {
		name = strings.TrimSpace(p.PhotographerUsername)
	}
	var parts []string
	if name != "" {
		parts = append(parts, "Photo by "+name+" on Unsplash")
	} else {
		parts = append(parts, "Photo from Unsplash")
	}
	if page := strings.TrimSpace(p.PageURL); page != "" {
		parts = append(parts, page)
	}
	return strings.Join(parts, " · ")
}
