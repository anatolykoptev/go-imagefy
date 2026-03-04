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
	pexelsOfficialURL = "https://api.pexels.com/v1"
	pexelsInternalURL = "https://www.pexels.com/en-us/api/v3/search/photos"
	pexelsBodyLimit   = 2 * 1024 * 1024
	pexelsPerPage     = 40
)

// PexelsProvider searches images via the Pexels API.
// Supports both official API (with APIKey) and internal API (with SecretKey).
// When both keys are set, official API is tried first with fallback to internal.
type PexelsProvider struct {
	APIKey     string       // official API key (Authorization header)
	SecretKey  string       // internal API key (Secret-Key header)
	HTTPClient *http.Client // optional (nil = http.DefaultClient)
	UserAgent  string       // optional

	officialBase string // test override
	internalBase string // test override
}

// Name returns the provider name.
func (p *PexelsProvider) Name() string { return "pexels" }

// --- Types ---

type pexelsSrc struct {
	Large string `json:"large"`
	Small string `json:"small"`
}

type pexelsOfficialPhoto struct {
	ID  int       `json:"id"`
	Alt string    `json:"alt"`
	URL string    `json:"url"`
	Src pexelsSrc `json:"src"`
}

type pexelsInternalImage struct {
	Small        string `json:"small"`
	DownloadLink string `json:"download_link"`
}

type pexelsInternalUser struct {
	Username string `json:"username"`
}

type pexelsInternalAttrs struct {
	ID    int                 `json:"id"`
	Slug  string              `json:"slug"`
	Title string              `json:"title"`
	Image pexelsInternalImage `json:"image"`
	User  pexelsInternalUser  `json:"user"`
}

type pexelsInternalItem struct {
	Attributes pexelsInternalAttrs `json:"attributes"`
}

// --- Shared HTTP fetch ---

func (p *PexelsProvider) doGet(ctx context.Context, rawURL, headerKey, headerVal string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(headerKey, headerVal)
	if p.UserAgent != "" {
		req.Header.Set("User-Agent", p.UserAgent)
	}

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req) //nolint:gosec // G107: URL is cfg-supplied by design
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pexels: unexpected status %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, pexelsBodyLimit))
}

func pexelsPageParam(opts SearchOpts) int {
	if opts.PageNumber < 1 {
		return 1
	}
	return opts.PageNumber
}

// --- Official API search ---

func (p *PexelsProvider) searchOfficial(ctx context.Context, baseURL, query string, opts SearchOpts) ([]ImageCandidate, error) {
	searchURL := fmt.Sprintf("%s/search?query=%s&per_page=%d&page=%d",
		strings.TrimRight(baseURL, "/"), url.QueryEscape(query), pexelsPerPage, pexelsPageParam(opts))

	body, err := p.doGet(ctx, searchURL, "Authorization", p.APIKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Photos []pexelsOfficialPhoto `json:"photos"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return filterOfficialResults(resp.Photos), nil
}

func filterOfficialResults(photos []pexelsOfficialPhoto) []ImageCandidate {
	var candidates []ImageCandidate
	for _, photo := range photos {
		if photo.Src.Large == "" || IsLogoOrBanner(strings.ToLower(photo.Src.Large)) {
			continue
		}
		candidates = append(candidates, ImageCandidate{
			ImgURL: photo.Src.Large, Thumbnail: photo.Src.Small,
			Source: photo.URL, Title: photo.Alt, License: LicenseSafe,
		})
	}
	return candidates
}

// --- Internal API search ---

func (p *PexelsProvider) searchInternal(ctx context.Context, baseURL, query string, opts SearchOpts) ([]ImageCandidate, error) {
	searchURL := fmt.Sprintf("%s?query=%s&per_page=%d&page=%d",
		strings.TrimRight(baseURL, "/"), url.QueryEscape(query), pexelsPerPage, pexelsPageParam(opts))

	body, err := p.doGet(ctx, searchURL, "Secret-Key", p.SecretKey)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []pexelsInternalItem `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return filterInternalResults(resp.Data), nil
}

func filterInternalResults(items []pexelsInternalItem) []ImageCandidate {
	var candidates []ImageCandidate
	for _, item := range items {
		a := item.Attributes
		if a.Image.DownloadLink == "" || IsLogoOrBanner(strings.ToLower(a.Image.DownloadLink)) {
			continue
		}
		candidates = append(candidates, ImageCandidate{
			ImgURL:    a.Image.DownloadLink,
			Thumbnail: a.Image.Small,
			Source:    fmt.Sprintf("https://www.pexels.com/photo/%s-%d/", a.Slug, a.ID),
			Title:     a.Title,
			License:   LicenseSafe,
		})
	}
	return candidates
}
