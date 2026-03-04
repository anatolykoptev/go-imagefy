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
	officialBase string     // test override
	internalBase string     // test override
}

// Name returns the provider name.
func (p *PexelsProvider) Name() string { return "pexels" }

func (p *PexelsProvider) officialURL() string {
	if p.officialBase != "" {
		return p.officialBase
	}
	return pexelsOfficialURL
}

func (p *PexelsProvider) internalURL() string {
	if p.internalBase != "" {
		return p.internalBase
	}
	return pexelsInternalURL
}

// Search queries the Pexels API for images. It tries the official API first (if APIKey is set),
// falling back to the internal API (if SecretKey is set) on failure.
func (p *PexelsProvider) Search(ctx context.Context, query string, opts SearchOpts) ([]ImageCandidate, error) {
	if p.APIKey != "" {
		if c, err := p.searchOfficial(ctx, p.officialURL(), query, opts); err == nil {
			return c, nil
		}
	}
	if p.SecretKey != "" {
		return p.searchInternal(ctx, p.internalURL(), query, opts)
	}
	if p.APIKey != "" {
		return p.searchOfficial(ctx, p.officialURL(), query, opts)
	}
	return nil, fmt.Errorf("pexels: no API key or secret key configured")
}

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

func (p *PexelsProvider) doGet(ctx context.Context, rawURL, hdrKey, hdrVal string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set(hdrKey, hdrVal)
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

func pexelsPage(opts SearchOpts) int {
	if opts.PageNumber < 1 {
		return 1
	}
	return opts.PageNumber
}

func (p *PexelsProvider) searchOfficial(ctx context.Context, base, query string, opts SearchOpts) ([]ImageCandidate, error) {
	u := fmt.Sprintf("%s/search?query=%s&per_page=%d&page=%d",
		strings.TrimRight(base, "/"), url.QueryEscape(query), pexelsPerPage, pexelsPage(opts))
	body, err := p.doGet(ctx, u, "Authorization", p.APIKey)
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
	var out []ImageCandidate
	for _, p := range photos {
		if p.Src.Large == "" || IsLogoOrBanner(strings.ToLower(p.Src.Large)) {
			continue
		}
		out = append(out, ImageCandidate{
			ImgURL: p.Src.Large, Thumbnail: p.Src.Small,
			Source: p.URL, Title: p.Alt, License: LicenseSafe,
		})
	}
	return out
}

func (p *PexelsProvider) searchInternal(ctx context.Context, base, query string, opts SearchOpts) ([]ImageCandidate, error) {
	u := fmt.Sprintf("%s?query=%s&per_page=%d&page=%d",
		strings.TrimRight(base, "/"), url.QueryEscape(query), pexelsPerPage, pexelsPage(opts))
	body, err := p.doGet(ctx, u, "Secret-Key", p.SecretKey)
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
	var out []ImageCandidate
	for _, item := range items {
		a := item.Attributes
		if a.Image.DownloadLink == "" || IsLogoOrBanner(strings.ToLower(a.Image.DownloadLink)) {
			continue
		}
		out = append(out, ImageCandidate{
			ImgURL:    a.Image.DownloadLink,
			Thumbnail: a.Image.Small,
			Source:    fmt.Sprintf("https://www.pexels.com/photo/%s-%d/", a.Slug, a.ID),
			Title:     a.Title,
			License:   LicenseSafe,
		})
	}
	return out
}
