package imagefy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	ogFetchTimeout = 10 * time.Second
	ogBodyLimit    = 2 * 1024 * 1024
)

// OGImageProvider extracts og:image from a page URL and returns it as a candidate.
// The page URL is passed via SearchOpts.PageURL; the query parameter is ignored.
type OGImageProvider struct {
	HTTPClient *http.Client
}

// Name returns the provider name.
func (p *OGImageProvider) Name() string { return "og" }

// Search fetches the page at opts.PageURL, extracts og:image, and returns it
// as a filtered candidate. Returns empty (not error) on any failure.
func (p *OGImageProvider) Search(ctx context.Context, _ string, opts SearchOpts) ([]ImageCandidate, error) {
	if opts.PageURL == "" {
		return nil, nil
	}

	imgURL := p.fetchOG(ctx, opts.PageURL)
	if imgURL == "" {
		return nil, nil
	}

	if IsLogoOrBanner(strings.ToLower(imgURL)) {
		return nil, nil
	}

	license := CheckLicense(imgURL, opts.PageURL)
	if license == LicenseBlocked {
		return nil, nil
	}

	return []ImageCandidate{{
		ImgURL:  imgURL,
		Source:  opts.PageURL,
		Title:   "og:image",
		License: license,
	}}, nil
}

func (p *OGImageProvider) fetchOG(ctx context.Context, pageURL string) string {
	ctx, cancel := context.WithTimeout(ctx, ogFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; go-imagefy/1.0)")

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req) //nolint:gosec // G107: URL is caller-supplied
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, ogBodyLimit))
	if err != nil {
		return ""
	}

	return ExtractOGImageURL(string(body))
}
