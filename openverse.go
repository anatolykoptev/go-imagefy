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

const (
	openverseDefaultURL   = "https://api.openverse.org/v1"
	openverseBodyLimit    = 1 * 1024 * 1024 // 1MB
	openverseDefaultLimit = 20
)

// openverseResult is the JSON shape of a single Openverse image result.
type openverseResult struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	URL                string `json:"url"`
	Thumbnail          string `json:"thumbnail"`
	ForeignLandingURL  string `json:"foreign_landing_url"`
	Source             string `json:"source"`
	License            string `json:"license"`
}

// OpenverseProvider searches openly-licensed images via the Openverse API.
// All returned images are pre-licensed (CC or public domain) so they receive LicenseSafe.
// Engines from SearchOpts are ignored — Openverse has its own source catalog.
// See: https://api.openverse.org/v1/
type OpenverseProvider struct {
	BaseURL    string       // default: "https://api.openverse.org/v1"
	HTTPClient *http.Client // optional (nil = http.DefaultClient)
	UserAgent  string       // optional
}

// Name returns the provider name.
func (p *OpenverseProvider) Name() string { return "openverse" }

// Search queries the Openverse API for images matching query and returns filtered candidates.
// Logo/banner URLs are excluded before returning. All results receive LicenseSafe.
func (p *OpenverseProvider) Search(ctx context.Context, query string, opts SearchOpts) ([]ImageCandidate, error) {
	results, err := p.fetch(ctx, query, opts)
	if err != nil {
		return nil, err
	}
	return p.filter(results), nil
}

func (p *OpenverseProvider) fetch(ctx context.Context, query string, opts SearchOpts) ([]openverseResult, error) {
	searchURL := p.buildURL(query, opts)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req) //nolint:gosec // G107: URL is cfg-supplied by design — SSRF is caller's responsibility
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openverse: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, openverseBodyLimit))
	if err != nil {
		return nil, err
	}

	var searchResp struct {
		Results []openverseResult `json:"results"`
	}
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, err
	}

	return searchResp.Results, nil
}

func (p *OpenverseProvider) buildURL(query string, opts SearchOpts) string {
	base := p.BaseURL
	if base == "" {
		base = openverseDefaultURL
	}
	base = strings.TrimRight(base, "/")

	page := opts.PageNumber
	if page < 1 {
		page = 1
	}

	return fmt.Sprintf("%s/images/?q=%s&page=%d&page_size=%d",
		base,
		url.QueryEscape(query),
		page,
		openverseDefaultLimit,
	)
}

func (p *OpenverseProvider) filter(results []openverseResult) []ImageCandidate {
	var candidates []ImageCandidate
	for _, r := range results {
		if r.URL == "" {
			continue
		}
		if IsLogoOrBanner(strings.ToLower(r.URL)) {
			continue
		}

		candidates = append(candidates, ImageCandidate{
			ImgURL:    r.URL,
			Thumbnail: r.Thumbnail,
			Source:    r.ForeignLandingURL,
			Title:     r.Title,
			License:   LicenseSafe,
		})
	}
	return candidates
}
