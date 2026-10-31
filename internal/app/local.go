package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dotfrankruan/cattv/internal/library"
	"github.com/dotfrankruan/cattv/internal/render"
	"github.com/dotfrankruan/cattv/internal/state"
)

// ShowFile displays a user-supplied local image without any network access.
func (a *App) ShowFile(path string) (*Shown, error) {
	if path == "" {
		return nil, errors.New("no image path supplied")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open image: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory; provide an image file", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	img, format, err := render.DecodeFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	b := img.Bounds()
	caption := fmt.Sprintf("%s · %s %d×%d", path, format, b.Dx(), b.Dy())
	result, err := a.renderImage(img, caption)
	if err != nil {
		return nil, err
	}
	return &Shown{
		Kind:     "file",
		Source:   path,
		Caption:  caption,
		Renderer: a.opts.Renderer,
		Result:   result,
	}, nil
}

// Local displays a random image from the user's local library without network
// access.
func (a *App) Local(ctx context.Context) (*Shown, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := a.drawable(); err != nil {
		return nil, err
	}
	store := a.libraryStore()
	idx, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("read local library: %w", err)
	}
	if len(idx.Items) == 0 {
		return nil, ErrLibraryEmpty
	}

	st := a.loadState()
	choices := idx.Candidates(10, a.rng, st.Exclude())
	if len(choices) == 0 {
		choices = idx.Candidates(10, a.rng, nil)
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("%w: local library has no candidates", ErrNoImage)
	}

	var lastErr error
	for _, item := range choices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		img, _, err := render.DecodeFile(item.Path)
		if err != nil {
			lastErr = err
			continue
		}
		b := img.Bounds()
		caption := fmt.Sprintf("%s · %d×%d", item.Path, b.Dx(), b.Dy())
		result, err := a.renderImage(img, caption)
		if err != nil {
			return nil, err
		}
		a.markSeen(state.LocalKey(item.ID))
		return &Shown{
			Kind:     "local",
			ID:       item.ID,
			Source:   item.Path,
			Caption:  caption,
			Renderer: a.opts.Renderer,
			Result:   result,
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("every candidate is missing")
	}
	return nil, fmt.Errorf("%w: local library images are unreadable: %v; run `cattv import <path>` again",
		ErrNoImage, lastErr)
}

// LibraryIndex returns the current local library for status commands.
func (a *App) LibraryIndex() (*library.Index, error) {
	return a.libraryStore().Load()
}
