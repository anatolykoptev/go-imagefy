package imagefy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	ddgBodyLimit = 2 * 1024 * 1024 // 2MB
	ddgBaseURL   = "https://duckduckgo.com"
)

// DDGImageProvider searches images via DuckDuckGo Images API.
// Uses a two-step flow: first obtains a vqd token, then queries the image API.
type DDGImageProvider struct {
	HTTPClient *http.Client // required (use go-stealth proxied client)
	UserAgent  string       // optional
}

// Name returns the provider name.
func (p *DDGImageProvider) Name() string { return "ddg" }

// Search queries DuckDuckGo Images and returns filtered candidates.
func (p *DDGImageProvider) Search(ctx context.Context, query string, _ SearchOpts) ([]ImageCandidate, error) {
	token, err := p.fetchToken(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ddg token: %w", err)
	}

	results, err := p.fetchImages(ctx, query, token)
	if err != nil {
		return nil, fmt.Errorf("ddg images: %w", err)
	}

	return p.filter(results), nil
}

// ddgImageResult is a single result from the DDG image API.
type ddgImageResult struct {
	Image     string `json:"image"`     // direct image URL
	Thumbnail string `json:"thumbnail"` // thumbnail URL
	URL       string `json:"url"`       // source page URL
	Title     string `json:"title"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

var vqdRe = regexp.MustCompile(`vqd=([0-9a-f-]+)`)

func (p *DDGImageProvider) fetchToken(ctx context.Context, query string) (string, error) {
	u := fmt.Sprintf("%s/?q=%s&iax=images&ia=images",
		ddgBaseURL, url.QueryEscape(query))

	body, err := p.get(ctx, u)
	if err != nil {
		return "", err
	}

	m := vqdRe.FindSubmatch(body)
	if len(m) < 2 {
		return "", fmt.Errorf("vqd token not found in DDG response (%d bytes)", len(body))
	}
	return string(m[1]), nil
}

func (p *DDGImageProvider) fetchImages(ctx context.Context, query, token string) ([]ddgImageResult, error) {
	u := fmt.Sprintf("%s/i.js?l=ru-ru&o=json&q=%s&vqd=%s&f=,,,,,&p=1",
		ddgBaseURL, url.QueryEscape(query), url.QueryEscape(token))

	body, err := p.get(ctx, u)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Results []ddgImageResult `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("ddg json: %w (body: %.200s)", err, body)
	}
	return resp.Results, nil
}

func (p *DDGImageProvider) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}

	ua := p.UserAgent
	if ua == "" {
		ua = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,*/*")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Referer", ddgBaseURL+"/")

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}

	return io.ReadAll(io.LimitReader(resp.Body, ddgBodyLimit))
}

func (p *DDGImageProvider) filter(results []ddgImageResult) []ImageCandidate {
	var candidates []ImageCandidate
	for _, r := range results {
		if r.Image == "" {
			continue
		}
		if IsLogoOrBanner(strings.ToLower(r.Image)) {
			continue
		}

		license := CheckLicense(r.Image, r.URL)
		if license == LicenseBlocked {
			continue
		}

		candidates = append(candidates, ImageCandidate{
			ImgURL:    r.Image,
			Thumbnail: r.Thumbnail,
			Source:    r.URL,
			Title:     r.Title,
			License:   license,
		})
	}
	return candidates
}
