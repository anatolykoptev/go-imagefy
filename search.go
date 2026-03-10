package imagefy

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

const searchTimeout = 30 * time.Second

// ImageCandidate holds an image result with metadata.
type ImageCandidate struct {
	ImgURL    string       // direct image URL
	Thumbnail string       // thumbnail URL
	Source    string       // page URL
	Title     string       // image/page title
	License   ImageLicense // license classification
}

// SearchImages queries configured search providers for images and returns up to maxResults validated candidates.
// Stock photo results (LicenseBlocked) are removed entirely.
// Results are sorted with LicenseSafe first, then LicenseUnknown.
func (cfg *Config) SearchImages(ctx context.Context, query string, maxResults int) []ImageCandidate {
	return cfg.SearchImagesWithOpts(ctx, query, maxResults, SearchOpts{})
}

// SearchImagesWithOpts is like SearchImages but accepts SearchOpts for pagination,
// engine selection and custom timeout.
func (cfg *Config) SearchImagesWithOpts(ctx context.Context, query string, maxResults int, opts SearchOpts) []ImageCandidate {
	if query == "" {
		return nil
	}

	cfg.defaults()

	if cfg.OnImageSearch != nil {
		cfg.OnImageSearch()
	}

	timeout := searchTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	providers := cfg.resolveProviders()
	candidates := cfg.gatherCandidates(ctx, providers, query, opts)

	if len(candidates) == 0 {
		return nil
	}

	// Sort: safe sources first, then unknown.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].License < candidates[j].License
	})

	return cfg.validateCandidates(ctx, candidates, maxResults)
}

// resolveProviders returns the effective provider list.
// If Providers is set, it is used directly. Otherwise a SearXNGProvider is
// auto-created from SearxngURL for backward compatibility.
func (cfg *Config) resolveProviders() []SearchProvider {
	if len(cfg.Providers) > 0 {
		return cfg.Providers
	}
	if cfg.SearxngURL != "" {
		return []SearchProvider{&SearXNGProvider{
			URL:        cfg.SearxngURL,
			HTTPClient: cfg.HTTPClient,
			UserAgent:  cfg.UserAgent,
		}}
	}
	return nil
}

// gatherCandidates collects image candidates from all providers in parallel.
// Each provider runs in its own goroutine; errors are logged and skipped so
// that remaining providers still contribute results.
func (cfg *Config) gatherCandidates(ctx context.Context, providers []SearchProvider, query string, opts SearchOpts) []ImageCandidate {
	var mu sync.Mutex
	var all []ImageCandidate
	var wg sync.WaitGroup
	for _, p := range providers {
		wg.Add(1)
		go func(p SearchProvider) {
			defer wg.Done()
			results, err := p.Search(ctx, query, opts)
			if err != nil {
				slog.Warn("imagefy: provider search failed", "provider", p.Name(), "error", err)
				return
			}
			mu.Lock()
			all = append(all, results...)
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	return all
}

// ValidateCandidates runs external image candidates through the full filter
// pipeline: URL validation, license check, dedup, metadata assessment, and
// LLM vision classification. Use this to validate images from sources outside
// the built-in search providers (e.g. WP media library, user-supplied URLs).
func (cfg *Config) ValidateCandidates(ctx context.Context, candidates []ImageCandidate, maxResults int) []ImageCandidate {
	if len(candidates) == 0 {
		return nil
	}
	cfg.defaults()
	return cfg.validateCandidates(ctx, candidates, maxResults)
}
