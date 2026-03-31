package imagefy

import (
	"context"
	"strings"

	"github.com/anatolykoptev/go-stealth/imagesearch"
)

const nativeMaxResults = 10

// NativeImageProvider searches images using go-stealth/imagesearch engines directly (in-process).
type NativeImageProvider struct {
	search *imagesearch.MultiSearch
}

// NewNativeImageProvider creates a provider backed by native Go image search engines.
func NewNativeImageProvider(ms *imagesearch.MultiSearch) *NativeImageProvider {
	return &NativeImageProvider{search: ms}
}

// Name returns the provider name.
func (p *NativeImageProvider) Name() string { return "native" }

// Search queries all native engines, converts results, and applies license + logo filters.
func (p *NativeImageProvider) Search(ctx context.Context, query string, _ SearchOpts) ([]ImageCandidate, error) {
	results := p.search.Search(ctx, query, nativeMaxResults)

	candidates := make([]ImageCandidate, 0, len(results))
	for _, r := range results {
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
			Width:     r.Width,
			Height:    r.Height,
			Engine:    r.Engine,
		})
	}
	return candidates, nil
}
