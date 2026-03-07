package imagefy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	oxBodyLimit        = 2 * 1024 * 1024 // 2MB
	oxDefaultMaxResults = 10
)

// OxBrowserProvider searches images via ox-browser REST API.
// ox-browser handles Cloudflare bypass, engine rotation, and proxy routing internally.
type OxBrowserProvider struct {
	BaseURL    string       // e.g. "http://ox-browser:8901" or "http://127.0.0.1:8901"
	Engines    []string     // e.g. ["bing", "ddg", "yandex", "brave"]
	MaxResults int          // default: 10
	Client     *http.Client // optional (nil = http.DefaultClient)
}

// Name returns the provider name.
func (p *OxBrowserProvider) Name() string { return "ox-browser" }

// Search queries the ox-browser image search API and returns filtered candidates.
// Blocked-license and logo/banner URLs are excluded before returning.
func (p *OxBrowserProvider) Search(ctx context.Context, query string, _ SearchOpts) ([]ImageCandidate, error) {
	results, err := p.fetch(ctx, query)
	if err != nil {
		return nil, err
	}
	return p.filter(results), nil
}

// oxSearchRequest is the JSON body sent to ox-browser /images/search.
type oxSearchRequest struct {
	Query      string   `json:"query"`
	Engines    []string `json:"engines,omitempty"`
	MaxResults int      `json:"max_results"`
}

// oxImageResult is a single image result from the ox-browser API.
type oxImageResult struct {
	URL       string `json:"url"`
	Thumbnail string `json:"thumbnail"`
	Source    string `json:"source"`
	Title     string `json:"title"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Engine    string `json:"engine"`
}

// oxSearchResponse is the JSON response from ox-browser /images/search.
type oxSearchResponse struct {
	Images     []oxImageResult `json:"images"`
	EnginesUsed []string       `json:"engines_used"`
	ElapsedMS  int64           `json:"elapsed_ms"`
}

func (p *OxBrowserProvider) fetch(ctx context.Context, query string) ([]oxImageResult, error) {
	maxResults := p.MaxResults
	if maxResults <= 0 {
		maxResults = oxDefaultMaxResults
	}

	payload := oxSearchRequest{
		Query:      query,
		Engines:    p.Engines,
		MaxResults: maxResults,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ox-browser: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(p.BaseURL, "/") + "/images/search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req) //nolint:gosec // G107: URL is cfg-supplied by design — SSRF is caller's responsibility
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ox-browser: unexpected status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, oxBodyLimit))
	if err != nil {
		return nil, err
	}

	var searchResp oxSearchResponse
	if err := json.Unmarshal(respBody, &searchResp); err != nil {
		return nil, fmt.Errorf("ox-browser: unmarshal response: %w", err)
	}

	return searchResp.Images, nil
}

func (p *OxBrowserProvider) filter(results []oxImageResult) []ImageCandidate {
	var candidates []ImageCandidate
	for _, r := range results {
		if r.URL == "" {
			continue
		}
		if IsLogoOrBanner(strings.ToLower(r.URL)) {
			continue
		}

		license := CheckLicense(r.URL, r.Source)
		if license == LicenseBlocked {
			continue
		}

		candidates = append(candidates, ImageCandidate{
			ImgURL:    r.URL,
			Thumbnail: r.Thumbnail,
			Source:    r.Source,
			Title:     r.Title,
			License:   license,
		})
	}
	return candidates
}
