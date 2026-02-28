package imagefy

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

const (
	searxngTimeout      = 15 * time.Second
	validationSemaphore = 3
)

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

	timeout := searxngTimeout
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

// gatherCandidates collects image candidates from all providers.
// Each provider is called sequentially; errors are logged and skipped so
// that remaining providers still contribute results.
func (cfg *Config) gatherCandidates(ctx context.Context, providers []SearchProvider, query string, opts SearchOpts) []ImageCandidate {
	var all []ImageCandidate
	for _, p := range providers {
		results, err := p.Search(ctx, query, opts)
		if err != nil {
			slog.Warn("imagefy: provider search failed", "provider", p.Name(), "error", err.Error())
			continue
		}
		all = append(all, results...)
	}
	return all
}

func (cfg *Config) validateCandidates(ctx context.Context, toValidate []ImageCandidate, maxResults int) []ImageCandidate {
	sem := make(chan struct{}, validationSemaphore)
	var mu sync.Mutex
	var validated []ImageCandidate
	dedup := &dedupFilter{}

	var wg sync.WaitGroup
	for _, c := range toValidate {
		mu.Lock()
		enough := len(validated) >= maxResults
		mu.Unlock()
		if enough {
			break
		}

		wg.Add(1)
		go func(cand ImageCandidate) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cfg.validateOne(ctx, cand, maxResults, &mu, &validated, dedup)
		}(c)
	}
	wg.Wait()

	return validated
}

// validateOne validates a single candidate and appends it to validated if it passes all checks.
// Recovers from panics to protect the goroutine pool.
//
// Pipeline stages:
//  1. ValidateImageURL — HTTP probe (dimensions, content-type, logo/banner check)
//  2. downloadForValidation — single download for dedup + metadata
//  3. Perceptual dedup — reject visual duplicates
//  4. ExtractImageMetadata + AssessLicense — domain + metadata signals
//  5. IsRealPhoto (LLM) — fallback for unknown license
func (cfg *Config) validateOne(ctx context.Context, cand ImageCandidate, maxResults int, mu *sync.Mutex, validated *[]ImageCandidate, dedup *dedupFilter) {
	defer func() {
		if r := recover(); r != nil {
			if cfg.OnPanic != nil {
				cfg.OnPanic("imageValidation", r)
			}
		}
	}()

	if !cfg.ValidateImageURL(ctx, cand.ImgURL) {
		return
	}

	// Download once for both dedup and metadata extraction.
	data, img := cfg.downloadForValidation(ctx, cand.ImgURL)

	// Dedup check using perceptual hash.
	if img != nil {
		if dedup.isDuplicate(img) {
			slog.Debug("imagefy: dedup rejected", "url", cand.ImgURL)
			return
		}
	}

	// Extract metadata and assess license.
	meta := ExtractImageMetadata(data)
	assessment := cfg.AssessLicense(cand, meta)

	if assessment.License == LicenseBlocked {
		slog.Debug("imagefy: blocked by license assessment", "url", cand.ImgURL, "signals", assessment.Signals)
		if cfg.OnClassification != nil {
			cfg.OnClassification(ClassificationEvent{
				URL:    cand.ImgURL,
				Class:  ClassStock,
				Source: "license_assessment",
			})
		}
		return
	}

	if assessment.License == LicenseSafe {
		slog.Debug("imagefy: safe by license assessment", "url", cand.ImgURL, "signals", assessment.Signals)
		if cfg.OnClassification != nil {
			cfg.OnClassification(ClassificationEvent{
				URL:        cand.ImgURL,
				Class:      ClassPhoto,
				Confidence: 1.0,
				Source:     "license_assessment",
			})
		}
		mu.Lock()
		if len(*validated) < maxResults {
			*validated = append(*validated, cand)
		}
		mu.Unlock()
		return
	}

	// Unknown license — fall through to LLM classification.
	if !cfg.IsRealPhoto(ctx, cand.ImgURL) {
		slog.Debug("imagefy: vision rejected", "url", cand.ImgURL)
		return
	}
	mu.Lock()
	if len(*validated) < maxResults {
		*validated = append(*validated, cand)
	}
	mu.Unlock()
}
