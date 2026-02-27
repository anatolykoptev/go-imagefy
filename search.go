package imagefy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	searxngTimeout      = 15 * time.Second
	searxngBodyLimit    = 1 * 1024 * 1024 // 1MB
	validationSemaphore = 3
)

// ImageCandidate holds a SearXNG image result with metadata.
type ImageCandidate struct {
	ImgURL    string       // direct image URL
	Thumbnail string       // thumbnail URL
	Source    string       // page URL
	Title     string       // image/page title
	License   ImageLicense // license classification
}

// SearchImages queries SearXNG for images and returns up to maxResults validated candidates.
// Stock photo results (LicenseBlocked) are removed entirely.
// Results are sorted with LicenseSafe first, then LicenseUnknown.
func (cfg *Config) SearchImages(ctx context.Context, query string, maxResults int) []ImageCandidate {
	if query == "" {
		return nil
	}

	cfg.defaults()

	if cfg.OnImageSearch != nil {
		cfg.OnImageSearch()
	}

	ctx, cancel := context.WithTimeout(ctx, searxngTimeout)
	defer cancel()

	results, err := cfg.fetchSearxngResults(ctx, query)
	if err != nil {
		slog.Warn("imagefy: SearXNG request failed", "error", err.Error())
		return nil
	}

	toValidate := cfg.filterCandidates(results)
	if len(toValidate) == 0 {
		return nil
	}

	// Sort: safe sources first, then unknown.
	sort.SliceStable(toValidate, func(i, j int) bool {
		return toValidate[i].License < toValidate[j].License
	})

	return cfg.validateCandidates(ctx, toValidate, maxResults)
}

type searxngResult struct {
	ImgSrc    string `json:"img_src"`
	Thumbnail string `json:"thumbnail_src"`
	URL       string `json:"url"`
	Title     string `json:"title"`
}

func (cfg *Config) fetchSearxngResults(ctx context.Context, query string) ([]searxngResult, error) {
	searchURL := fmt.Sprintf("%s/search?q=%s&format=json&categories=images",
		strings.TrimRight(cfg.SearxngURL, "/"), url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := cfg.HTTPClient
	resp, err := client.Do(req) //nolint:gosec // G107: URL is cfg-supplied by design — SSRF is caller's responsibility
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, searxngBodyLimit))
	if err != nil {
		return nil, err
	}

	var searchResp struct {
		Results []searxngResult `json:"results"`
	}
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, err
	}

	return searchResp.Results, nil
}

func (cfg *Config) filterCandidates(results []searxngResult) []ImageCandidate {
	var candidates []ImageCandidate
	for _, r := range results {
		if r.ImgSrc == "" {
			continue
		}
		if IsLogoOrBanner(strings.ToLower(r.ImgSrc)) {
			continue
		}

		license := CheckLicense(r.ImgSrc, r.URL)
		if license == LicenseBlocked {
			continue
		}

		candidates = append(candidates, ImageCandidate{
			ImgURL:    r.ImgSrc,
			Thumbnail: r.Thumbnail,
			Source:    r.URL,
			Title:     r.Title,
			License:   license,
		})
	}
	return candidates
}

func (cfg *Config) validateCandidates(ctx context.Context, toValidate []ImageCandidate, maxResults int) []ImageCandidate {
	sem := make(chan struct{}, validationSemaphore)
	var mu sync.Mutex
	var validated []ImageCandidate

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

			cfg.validateOne(ctx, cand, maxResults, &mu, &validated)
		}(c)
	}
	wg.Wait()

	return validated
}

// validateOne validates a single candidate and appends it to validated if it passes all checks.
// Recovers from panics to protect the goroutine pool.
func (cfg *Config) validateOne(ctx context.Context, cand ImageCandidate, maxResults int, mu *sync.Mutex, validated *[]ImageCandidate) {
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
