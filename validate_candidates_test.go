package imagefy

import (
	"context"
	"testing"
)

func TestValidateCandidates_EmptyInput(t *testing.T) {
	t.Parallel()

	cfg := &Config{}

	if got := cfg.ValidateCandidates(context.Background(), nil, 5); got != nil {
		t.Errorf("ValidateCandidates(nil) = %v, want nil", got)
	}
	if got := cfg.ValidateCandidates(context.Background(), []ImageCandidate{}, 5); got != nil {
		t.Errorf("ValidateCandidates([]) = %v, want nil", got)
	}
}

func TestValidateCandidates_FiltersBlocked(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)

	candidates := []ImageCandidate{
		{
			ImgURL: "https://shutterstock.com/image/stock.jpg",
			Source: "https://shutterstock.com/page/123",
			Title:  "Stock Photo",
		},
		{
			ImgURL: imgSrv.URL + "/photo.jpg",
			Source: imgSrv.URL + "/page",
			Title:  "Free Photo",
		},
	}

	cfg := &Config{
		HTTPClient: imgSrv.Client(),
	}

	results := cfg.ValidateCandidates(context.Background(), candidates, 5)

	// The shutterstock candidate should be filtered out.
	for _, r := range results {
		if r.ImgURL == "https://shutterstock.com/image/stock.jpg" {
			t.Error("blocked domain candidate was not filtered out")
		}
	}
}

func TestValidateCandidates_RespectsMaxResults(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)

	candidates := make([]ImageCandidate, 5)
	for i := range candidates {
		candidates[i] = ImageCandidate{
			ImgURL: imgSrv.URL + "/photo.jpg",
			Source: imgSrv.URL + "/page",
			Title:  "Photo",
		}
	}

	cfg := &Config{
		HTTPClient: imgSrv.Client(),
	}

	const maxResults = 2
	results := cfg.ValidateCandidates(context.Background(), candidates, maxResults)
	if len(results) > maxResults {
		t.Errorf("got %d results, want at most %d", len(results), maxResults)
	}
}
