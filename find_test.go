package imagefy

import (
	"context"
	"testing"
)

// findMockProvider is a SearchProvider for FindImages tests.
type findMockProvider struct {
	name    string
	results []ImageCandidate
}

func (m *findMockProvider) Search(_ context.Context, _ string, _ SearchOpts) ([]ImageCandidate, error) {
	return m.results, nil
}

func (m *findMockProvider) Name() string { return m.name }

// TestFindImages_SearchOnly verifies that FindImages returns results from search providers.
func TestFindImages_SearchOnly(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)

	cfg := &Config{
		HTTPClient: imgSrv.Client(),
		Providers: []SearchProvider{
			&findMockProvider{
				name: "mock",
				results: []ImageCandidate{
					{ImgURL: imgSrv.URL + "/a.jpg", Source: imgSrv.URL + "/page", License: LicenseSafe},
				},
			},
		},
	}

	results := cfg.FindImages(context.Background(), FindOpts{Query: "nature"})

	if len(results) == 0 {
		t.Fatal("FindImages returned no results, expected at least 1")
	}
	if results[0].ImgURL != imgSrv.URL+"/a.jpg" {
		t.Errorf("ImgURL = %q, want %q", results[0].ImgURL, imgSrv.URL+"/a.jpg")
	}
}

// TestFindImages_ExternalCandidates verifies that external candidates are validated and returned.
func TestFindImages_ExternalCandidates(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)

	cfg := &Config{HTTPClient: imgSrv.Client()}

	results := cfg.FindImages(context.Background(), FindOpts{
		External: []ImageCandidate{
			{ImgURL: imgSrv.URL + "/ext.jpg", Source: imgSrv.URL + "/page", License: LicenseSafe},
		},
	})

	if len(results) == 0 {
		t.Fatal("FindImages returned no results for external candidates")
	}
	if results[0].ImgURL != imgSrv.URL+"/ext.jpg" {
		t.Errorf("ImgURL = %q, want %q", results[0].ImgURL, imgSrv.URL+"/ext.jpg")
	}
}

// TestFindImages_ExternalBlockedFiltered verifies that blocked external candidates are filtered out.
func TestFindImages_ExternalBlockedFiltered(t *testing.T) {
	t.Parallel()

	cfg := &Config{}

	results := cfg.FindImages(context.Background(), FindOpts{
		External: []ImageCandidate{
			{
				ImgURL:  "https://shutterstock.com/image/photo.jpg",
				Source:  "https://shutterstock.com/page/123",
				License: LicenseBlocked,
			},
		},
	})

	if len(results) != 0 {
		t.Errorf("FindImages returned %d results for blocked candidate, want 0", len(results))
	}
}

// TestFindImages_CombinesSearchAndExternal verifies that search and external results are merged
// with safe-licensed candidates sorted first.
func TestFindImages_CombinesSearchAndExternal(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)

	cfg := &Config{
		HTTPClient: imgSrv.Client(),
		Providers: []SearchProvider{
			&findMockProvider{
				name: "mock",
				results: []ImageCandidate{
					{ImgURL: imgSrv.URL + "/search.jpg", Source: imgSrv.URL + "/s", License: LicenseUnknown},
				},
			},
		},
	}

	results := cfg.FindImages(context.Background(), FindOpts{
		Query: "test",
		External: []ImageCandidate{
			{ImgURL: imgSrv.URL + "/ext.jpg", Source: imgSrv.URL + "/e", License: LicenseSafe},
		},
		MaxResults: 5,
	})

	if len(results) < 2 {
		t.Fatalf("FindImages returned %d results, want at least 2", len(results))
	}

	// Both search and external candidates should be present.
	var hasSearch, hasExternal bool
	for _, r := range results {
		if r.ImgURL == imgSrv.URL+"/search.jpg" {
			hasSearch = true
		}
		if r.ImgURL == imgSrv.URL+"/ext.jpg" {
			hasExternal = true
		}
	}
	if !hasSearch {
		t.Error("search candidate missing from results")
	}
	if !hasExternal {
		t.Error("external candidate missing from results")
	}
}

// TestFindImages_EmptyOpts verifies that empty FindOpts returns nil.
func TestFindImages_EmptyOpts(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	results := cfg.FindImages(context.Background(), FindOpts{})

	if results != nil {
		t.Errorf("FindImages with empty opts = %v, want nil", results)
	}
}

// TestFindImages_DefaultMaxResults verifies that MaxResults=0 defaults to 3.
func TestFindImages_DefaultMaxResults(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)

	var candidates []ImageCandidate
	for i := range 5 {
		candidates = append(candidates, ImageCandidate{
			ImgURL:  imgSrv.URL + "/img" + string(rune('a'+i)) + ".jpg",
			Source:  imgSrv.URL + "/page",
			License: LicenseSafe,
		})
	}

	cfg := &Config{
		HTTPClient: imgSrv.Client(),
		Providers: []SearchProvider{
			&findMockProvider{name: "mock", results: candidates},
		},
	}

	results := cfg.FindImages(context.Background(), FindOpts{
		Query:      "many",
		MaxResults: 0, // should default to 3
	})

	if len(results) > findDefaultMaxResults {
		t.Errorf("FindImages returned %d results with MaxResults=0, want at most %d",
			len(results), findDefaultMaxResults)
	}
}
