package imagefy

import (
	"context"
	"sort"
)

// FindOpts configures a unified image search across all sources.
type FindOpts struct {
	Query      string           // search query for providers
	PageURL    string           // page URL for OG image extraction
	External   []ImageCandidate // external candidates (WP media, user-supplied URLs)
	MaxResults int              // max results to return (default: 3)
	SearchOpts SearchOpts       // additional search options (timeout, engines, etc.)
}

const findDefaultMaxResults = 3

// FindImages is the unified entry point for image acquisition. It:
//  1. Queries configured search providers (if Query is set)
//  2. Extracts content images from PageURL (og:image, JSON-LD, <img> tags) if PageURL is set
//     and no ContentImageProvider or OGImageProvider is already in Providers.
//  3. Accepts external candidates (e.g. WP media library)
//  4. Merges all, sorts by license (safe first)
//  5. Runs the full filter pipeline (validate, dedup, metadata, LLM vision)
//
// Backward compat: when PageURL is set but ContentImageProvider finds only og:image,
// the result is identical to the old OGImageProvider-only behaviour.
func (cfg *Config) FindImages(ctx context.Context, opts FindOpts) []ImageCandidate {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = findDefaultMaxResults
	}

	cfg.defaults()

	var candidates []ImageCandidate

	// 1. Search providers (if query is set).
	if opts.Query != "" {
		searchOpts := opts.SearchOpts
		if searchOpts.PageURL == "" {
			searchOpts.PageURL = opts.PageURL
		}
		providers := cfg.resolveProviders()
		candidates = append(candidates, cfg.gatherCandidates(ctx, providers, opts.Query, searchOpts)...)
	}

	// 2. Content image extraction (replaces bare OGImageProvider).
	//    ContentImageProvider already includes og:image as a low-priority fallback,
	//    so this covers both cases with a single HTTP fetch.
	//    Skip if the caller has already wired a content or og provider explicitly.
	if opts.PageURL != "" && !cfg.hasContentProvider() && !cfg.hasOGProvider() {
		cp := &ContentImageProvider{HTTPClient: cfg.HTTPClient}
		cpCandidates, _ := cp.Search(ctx, opts.Query, SearchOpts{PageURL: opts.PageURL})
		candidates = append(candidates, cpCandidates...)
	}

	// 3. External candidates.
	candidates = append(candidates, opts.External...)

	if len(candidates) == 0 {
		return nil
	}

	// Sort: safe first.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].License < candidates[j].License
	})

	return cfg.validateCandidates(ctx, candidates, maxResults)
}

// hasContentProvider checks if a ContentImageProvider is already in the Providers list.
func (cfg *Config) hasContentProvider() bool {
	for _, p := range cfg.Providers {
		if p.Name() == "content" {
			return true
		}
	}
	return false
}

// hasOGProvider checks if an OGImageProvider is already in the Providers list.
func (cfg *Config) hasOGProvider() bool {
	for _, p := range cfg.Providers {
		if p.Name() == "og" {
			return true
		}
	}
	return false
}
