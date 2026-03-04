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

// --- Official API types ---

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

// --- Internal API types ---

type pexelsInternalImage struct {
	Small        string `json:"small"`
	DownloadLink string `json:"download_link"`
}

type pexelsInternalUser struct {
	Username string `json:"username"`
}

type pexelsInternalAttrs struct {
	ID          int                 `json:"id"`
	Slug        string              `json:"slug"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Width       int                 `json:"width"`
	Height      int                 `json:"height"`
	Image       pexelsInternalImage `json:"image"`
	User        pexelsInternalUser  `json:"user"`
}

type pexelsInternalItem struct {
	Attributes pexelsInternalAttrs `json:"attributes"`
}

// --- Official API search ---

func (p *PexelsProvider) searchOfficial(ctx context.Context, baseURL, query string, opts SearchOpts) ([]ImageCandidate, error) {
	page := opts.PageNumber
	if page < 1 {
		page = 1
	}

	searchURL := fmt.Sprintf("%s/search?query=%s&per_page=%d&page=%d",
		strings.TrimRight(baseURL, "/"), url.QueryEscape(query), pexelsPerPage, page)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", p.APIKey)
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
		return nil, fmt.Errorf("pexels official: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, pexelsBodyLimit))
	if err != nil {
		return nil, err
	}

	var searchResp struct {
		Photos []pexelsOfficialPhoto `json:"photos"`
	}
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, err
	}

	return filterOfficialResults(searchResp.Photos), nil
}

func filterOfficialResults(photos []pexelsOfficialPhoto) []ImageCandidate {
	var candidates []ImageCandidate
	for _, photo := range photos {
		if photo.Src.Large == "" {
			continue
		}
		if IsLogoOrBanner(strings.ToLower(photo.Src.Large)) {
			continue
		}
		candidates = append(candidates, ImageCandidate{
			ImgURL:    photo.Src.Large,
			Thumbnail: photo.Src.Small,
			Source:    photo.URL,
			Title:     photo.Alt,
			License:   LicenseSafe,
		})
	}
	return candidates
}
