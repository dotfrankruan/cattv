// Package library maintains a lightweight index of user-owned local images.
// It records paths and metadata only; files stay wherever the user put them.
package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dotfrankruan/cattv/internal/fileutil"
	_ "golang.org/x/image/webp"
)

// Version is the local library index schema.
const Version = 1

const indexKind = "cattv-library"

var supportedExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".jfif": true,
	".png": true, ".gif": true, ".webp": true,
}

// Item is one indexed local image.
type Item struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Format  string    `json:"format"`
	Width   int       `json:"width"`
	Height  int       `json:"height"`
	Bytes   int64     `json:"bytes"`
	AddedAt time.Time `json:"added_at"`
}

// Index is the serialized local library.
type Index struct {
	Kind    string `json:"kind"`
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}

// ImportReport summarizes an import operation.
type ImportReport struct {
	Added    int
	Updated  int
	Skipped  int
	Warnings []string
}

// Store persists an index at a user-configurable path.
type Store struct {
	Path string
}

// NewStore constructs a local library store.
func NewStore(path string) *Store {
	return &Store{Path: path}
}

// Load reads the index. A missing file yields an empty library.
func (s *Store) Load() (*Index, error) {
	if s == nil || s.Path == "" {
		return &Index{Kind: indexKind, Version: Version}, nil
	}
	data, err := fileutil.ReadFile(s.Path, true)
	if err != nil {
		return nil, fmt.Errorf("read local library: %w", err)
	}
	if len(data) == 0 {
		return &Index{Kind: indexKind, Version: Version}, nil
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse local library: %w", err)
	}
	if idx.Kind != "" && idx.Kind != indexKind {
		return nil, fmt.Errorf("file is %q, not a CatTV local library", idx.Kind)
	}
	if idx.Version != Version {
		return nil, fmt.Errorf("local library schema %d is not supported", idx.Version)
	}
	idx.Kind = indexKind
	seen := make(map[string]bool, len(idx.Items))
	for i := range idx.Items {
		item := &idx.Items[i]
		if item.ID == "" || item.Path == "" {
			return nil, errors.New("local library contains an item without an id or path")
		}
		if seen[item.ID] {
			return nil, fmt.Errorf("local library contains duplicate id %s", item.ID)
		}
		seen[item.ID] = true
	}
	return &idx, nil
}

// Save atomically writes the index.
func (s *Store) Save(idx *Index) error {
	if s == nil || s.Path == "" {
		return errors.New("local library path is not configured")
	}
	if idx == nil {
		return errors.New("nil local library")
	}
	idx.Kind = indexKind
	idx.Version = Version
	sort.SliceStable(idx.Items, func(i, j int) bool { return idx.Items[i].Path < idx.Items[j].Path })
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return fileutil.WriteAtomic(s.Path, 0o600, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// Import scans files and directories without cancellation support. It is a
// convenience wrapper around ImportContext.
func (s *Store) Import(paths []string) (*Index, ImportReport, error) {
	return s.ImportContext(context.Background(), paths)
}

// ImportContext scans files and directories for supported images,
// deduplicating by absolute path, and atomically updates the index.
func (s *Store) ImportContext(ctx context.Context, paths []string) (*Index, ImportReport, error) {
	if len(paths) == 0 {
		return nil, ImportReport{}, errors.New("no files or directories specified")
	}
	if err := ctx.Err(); err != nil {
		return nil, ImportReport{}, err
	}
	idx, err := s.Load()
	if err != nil {
		return nil, ImportReport{}, err
	}
	report := ImportReport{}
	existing := make(map[string]int, len(idx.Items))
	for i, item := range idx.Items {
		existing[item.Path] = i
	}
	visited := make(map[string]bool)
	for _, root := range paths {
		if err := ctx.Err(); err != nil {
			return nil, report, err
		}
		if err := walkImportRoot(ctx, root, func(path string) {
			if visited[path] {
				return
			}
			visited[path] = true
			s.importFile(path, idx, existing, &report)
		}, &report); err != nil {
			if ctx.Err() != nil {
				return nil, report, ctx.Err()
			}
			report.Warnings = append(report.Warnings, err.Error())
		}
	}
	if report.Added+report.Updated > 0 {
		if err := s.Save(idx); err != nil {
			return nil, report, err
		}
	}
	return idx, report, nil
}

func walkImportRoot(ctx context.Context, root string, visit func(string), report *ImportReport) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("%s: %w", root, err)
	}
	if !info.IsDir() {
		if !supportedExtensions[strings.ToLower(filepath.Ext(root))] {
			return fmt.Errorf("%s: unsupported image extension", root)
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("%s: %w", root, err)
		}
		visit(filepath.Clean(abs))
		return nil
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %v", path, walkErr))
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !supportedExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		visit(filepath.Clean(abs))
		return nil
	})
}

func (s *Store) importFile(path string, idx *Index, existing map[string]int, report *ImportReport) {
	f, err := os.Open(path)
	if err != nil {
		report.Skipped++
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %v", path, err))
		return
	}
	cfg, format, decodeErr := image.DecodeConfig(f)
	_ = f.Close()
	if decodeErr != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		report.Skipped++
		detail := "invalid image"
		if decodeErr != nil {
			detail = decodeErr.Error()
		}
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %s", path, detail))
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		report.Skipped++
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %v", path, err))
		return
	}
	id := itemID(path)
	if at, ok := existing[path]; ok {
		item := &idx.Items[at]
		item.Format = format
		item.Width = cfg.Width
		item.Height = cfg.Height
		item.Bytes = info.Size()
		if item.AddedAt.IsZero() {
			item.AddedAt = time.Now().UTC()
		}
		// Repair an index produced by an older ID scheme without creating a
		// duplicate logical entry.
		item.ID = id
		report.Updated++
		return
	}
	idx.Items = append(idx.Items, Item{
		ID:      id,
		Path:    path,
		Format:  format,
		Width:   cfg.Width,
		Height:  cfg.Height,
		Bytes:   info.Size(),
		AddedAt: time.Now().UTC(),
	})
	existing[path] = len(idx.Items) - 1
	report.Added++
}

func itemID(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8])
}

// Remove deletes an item by id, absolute path, or path relative to the current
// directory. It reports how many entries were removed.
func (s *Store) Remove(identifier string) (*Index, int, error) {
	idx, err := s.Load()
	if err != nil {
		return nil, 0, err
	}
	abs := identifier
	if candidate, err := filepath.Abs(identifier); err == nil {
		abs = filepath.Clean(candidate)
	}
	kept := idx.Items[:0]
	removed := 0
	for _, item := range idx.Items {
		if item.ID == identifier || item.Path == abs {
			removed++
			continue
		}
		kept = append(kept, item)
	}
	idx.Items = kept
	if removed > 0 {
		if err := s.Save(idx); err != nil {
			return nil, 0, err
		}
	}
	return idx, removed, nil
}

// Clear removes every item from the local library.
func (s *Store) Clear() error {
	return s.Save(&Index{Kind: indexKind, Version: Version})
}

// Candidates returns up to n random items that are not in exclude.
func (idx *Index) Candidates(n int, rng *rand.Rand, exclude map[string]bool) []Item {
	if n <= 0 {
		return nil
	}
	pool := make([]int, 0, len(idx.Items))
	for i := range idx.Items {
		if !exclude[idx.Items[i].ID] {
			pool = append(pool, i)
		}
	}
	if n > len(pool) {
		n = len(pool)
	}
	if n == 0 {
		return nil
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	out := make([]Item, 0, n)
	for i := 0; i < n; i++ {
		j := i + rng.Intn(len(pool)-i)
		pool[i], pool[j] = pool[j], pool[i]
		out = append(out, idx.Items[pool[i]])
	}
	return out
}
