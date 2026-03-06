package imagefy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const searxngBodyLimit = 1 * 1024 * 1024 // 1MB

// SearchProvider abstracts an image search backend.
// Implementations must return candidates with License already set (LicenseBlocked excluded).
type SearchProvider interface {
	// Search returns image candidates matching the query.
	Search(ctx context.Context, query string, opts SearchOpts) ([]ImageCandidate, error)
	// Name returns the provider name for logging.
	Name() string
}

// SearXNGProvider searches images via a SearXNG instance.
type SearXNGProvider struct {
	URL        string       // SearXNG base URL (required)
	HTTPClient *http.Client // optional (nil = http.DefaultClient)
	UserAgent  string       // optional
}

// Name returns the provider name.
func (p *SearXNGProvider) Name() string { return "searxng" }

// Search queries a SearXNG instance for images matching query and returns filtered candidates.
// Blocked-license and logo/banner URLs are excluded before returning.
func (p *SearXNGProvider) Search(ctx context.Context, query string, opts SearchOpts) ([]ImageCandidate, error) {
	results, err := p.fetch(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	return p.filter(results), nil
}

// searxngResult is the JSON shape of a single SearXNG image result.
type searxngResult struct {
	ImgSrc    string `json:"img_src"`
	Thumbnail string `json:"thumbnail_src"`
	URL       string `json:"url"`
	Title     string `json:"title"`
}

func (p *SearXNGProvider) fetch(ctx context.Context, query string, opts SearchOpts) ([]searxngResult, error) {
	searchURL := p.buildURL(query, opts)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

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

func (p *SearXNGProvider) buildURL(query string, opts SearchOpts) string {
	searchURL := fmt.Sprintf("%s/search?q=%s&format=json&categories=images",
		strings.TrimRight(p.URL, "/"), url.QueryEscape(query))

	if opts.PageNumber > 1 {
		searchURL += fmt.Sprintf("&pageno=%d", opts.PageNumber)
	}
	if len(opts.Engines) > 0 {
		searchURL += "&engines=" + url.QueryEscape(strings.Join(opts.Engines, ","))
	}
	return searchURL
}

func (p *SearXNGProvider) filter(results []searxngResult) []ImageCandidate {
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
