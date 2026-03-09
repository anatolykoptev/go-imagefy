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
//  2. Extracts og:image (if PageURL is set and no OGImageProvider in Providers)
//  3. Accepts external candidates (e.g. WP media library)
//  4. Merges all, sorts by license (safe first)
//  5. Runs the full filter pipeline (validate, dedup, metadata, LLM vision)
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

	// 2. OG image (if PageURL set and no OGImageProvider already in Providers).
	if opts.PageURL != "" && !cfg.hasOGProvider() {
		ogP := &OGImageProvider{HTTPClient: cfg.HTTPClient}
		ogCandidates, _ := ogP.Search(ctx, "", SearchOpts{PageURL: opts.PageURL})
		candidates = append(candidates, ogCandidates...)
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

// hasOGProvider checks if an OGImageProvider is already in the Providers list.
func (cfg *Config) hasOGProvider() bool {
	for _, p := range cfg.Providers {
		if p.Name() == "og" {
			return true
		}
	}
	return false
}
