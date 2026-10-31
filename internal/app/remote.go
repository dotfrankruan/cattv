package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/dotfrankruan/cattv/internal/catalog"
	"github.com/dotfrankruan/cattv/internal/fetch"
	"github.com/dotfrankruan/cattv/internal/render"
	"github.com/dotfrankruan/cattv/internal/state"
)

// Next fetches and renders one random remote cat. The metadata catalog is
// local; the image itself is downloaded once and released after rendering.
func (a *App) Next(ctx context.Context) (*Shown, error) {
	if a.fetcher == nil {
		return nil, errors.New("remote display has no downloader configured")
	}
	env, err := a.drawable()
	if err != nil {
		return nil, err
	}

	cat, err := catalog.Load(a.opts.CatalogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrCatalogMissing, a.opts.CatalogPath)
		}
		return nil, fmt.Errorf("%w: %v", ErrCatalogInvalid, err)
	}

	st := a.loadState()
	exclude := st.ExcludeIDs(state.RemotePrefix)
	choices := cat.Candidates(candidates, a.rng, exclude)
	if len(choices) == 0 {
		// Very small catalogs can eventually be entirely in the history. There
		// are no alternatives left, so relax the preference rather than fail.
		choices = cat.Candidates(candidates, a.rng, nil)
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("%w: catalog has no candidates", ErrNoImage)
	}

	target := a.sourceWidth(env)
	attempted := 0
	var lastErr error
	for _, photo := range choices {
		urls := photo.CandidateImageURLs(target)
		for _, imageURL := range urls {
			attempted++
			data, err := a.fetcher.Get(ctx, imageURL)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				if fetch.IsNetwork(err) {
					return nil, fmt.Errorf("%w: %v", ErrOffline, err)
				}
				lastErr = err
				continue
			}
			img, _, err := render.DecodeBytes(data)
			if err != nil {
				lastErr = fmt.Errorf("decode %s: %w", photo.ID, err)
				continue
			}
			caption := photo.Credit()
			detail := photo.Detail()
			result, err := a.renderImage(img, caption, detail)
			if err != nil {
				return nil, err
			}
			a.markSeen(state.RemoteKey(photo.ID))
			return &Shown{
				Kind:     "remote",
				ID:       photo.ID,
				Source:   photo.PageURL,
				Caption:  caption,
				Detail:   detail,
				Renderer: a.opts.Renderer,
				Result:   result,
			}, nil
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no candidate had a usable image URL")
	}
	return nil, fmt.Errorf("%w: tried %d request(s): %v", ErrNoImage, attempted, lastErr)
}
