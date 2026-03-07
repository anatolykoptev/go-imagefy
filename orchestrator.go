package imagefy

import (
	"context"
	"errors"
	"log/slog"
)

// FallbackProvider tries providers in order until one returns results.
// The first provider that returns a non-empty result set wins; subsequent
// providers are not called. If all providers fail or return empty results,
// the last error is returned (or a sentinel if all returned empty slices).
type FallbackProvider struct {
	Providers    []SearchProvider
	FallbackName string // display name; defaults to "fallback" when empty
}

// Compile-time check that FallbackProvider satisfies SearchProvider.
var _ SearchProvider = (*FallbackProvider)(nil)

// Name returns the provider name for logging.
func (f *FallbackProvider) Name() string {
	if f.FallbackName != "" {
		return f.FallbackName
	}
	return "fallback"
}

// Search tries each provider in order and returns the first non-empty result set.
// If a provider returns an error or empty results, a warning is logged and the
// next provider is tried. If all providers are exhausted, the last error is
// returned, or errors.New("all providers failed") if none returned an error.
func (f *FallbackProvider) Search(ctx context.Context, query string, opts SearchOpts) ([]ImageCandidate, error) {
	var lastErr error
	for _, p := range f.Providers {
		results, err := p.Search(ctx, query, opts)
		if err != nil {
			slog.Warn("imagefy: fallback provider failed", "provider", p.Name(), "error", err.Error())
			lastErr = err
			continue
		}
		if len(results) == 0 {
			slog.Warn("imagefy: fallback provider returned no results", "provider", p.Name())
			continue
		}
		return results, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("all providers failed")
}
