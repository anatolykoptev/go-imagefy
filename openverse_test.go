package imagefy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// buildOpenverseJSON encodes a slice of openverseResult items into the Openverse JSON response format.
func buildOpenverseJSON(items []openverseResult) []byte {
	body, _ := json.Marshal(map[string]any{"results": items})
	return body
}

// TestOpenverseProviderName verifies the provider name.
func TestOpenverseProviderName(t *testing.T) {
	t.Parallel()

	p := &OpenverseProvider{}
	if p.Name() != "openverse" {
		t.Errorf("Name() = %q, want %q", p.Name(), "openverse")
	}
}

// TestOpenverseProviderSearch_HappyPath verifies that a valid Openverse response is parsed
// into candidates with the correct fields.
func TestOpenverseProviderSearch_HappyPath(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOpenverseJSON([]openverseResult{
			{
				ID:                "abc123",
				Title:             "Sunny Meadow",
				URL:               "https://live.staticflickr.com/abc/photo.jpg",
				Thumbnail:         "https://api.openverse.org/v1/images/abc123/thumb/",
				ForeignLandingURL: "https://www.flickr.com/photos/user/abc123",
				Source:            "flickr",
				License:           "cc0",
			},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &OpenverseProvider{BaseURL: srv.URL, HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "meadow", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("Search returned no candidates, expected 1")
	}

	got := candidates[0]
	if got.ImgURL != "https://live.staticflickr.com/abc/photo.jpg" {
		t.Errorf("ImgURL = %q, want direct image URL", got.ImgURL)
	}
	if got.Thumbnail != "https://api.openverse.org/v1/images/abc123/thumb/" {
		t.Errorf("Thumbnail = %q, unexpected value", got.Thumbnail)
	}
	if got.Source != "https://www.flickr.com/photos/user/abc123" {
		t.Errorf("Source = %q, want foreign_landing_url", got.Source)
	}
	if got.Title != "Sunny Meadow" {
		t.Errorf("Title = %q, want %q", got.Title, "Sunny Meadow")
	}
}

// TestOpenverseProviderSearch_AllResultsAreLicenseSafe verifies that every returned
// candidate has LicenseSafe — Openverse only serves CC/PD content.
func TestOpenverseProviderSearch_AllResultsAreLicenseSafe(t *testing.T) {
	t.Parallel()

	results := []openverseResult{
		{URL: "https://live.staticflickr.com/1/a.jpg", ForeignLandingURL: "https://flickr.com/a", Title: "A", License: "cc-by"},
		{URL: "https://live.staticflickr.com/2/b.jpg", ForeignLandingURL: "https://flickr.com/b", Title: "B", License: "cc0"},
		{URL: "https://live.staticflickr.com/3/c.jpg", ForeignLandingURL: "https://flickr.com/c", Title: "C", License: "pdm"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOpenverseJSON(results))
	}))
	t.Cleanup(srv.Close)

	p := &OpenverseProvider{BaseURL: srv.URL, HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "nature", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) != len(results) {
		t.Fatalf("got %d candidates, want %d", len(candidates), len(results))
	}
	for i, c := range candidates {
		if c.License != LicenseSafe {
			t.Errorf("candidates[%d].License = %v, want LicenseSafe", i, c.License)
		}
	}
}

// TestOpenverseProviderSearch_Pagination verifies that PageNumber is passed as the page param.
func TestOpenverseProviderSearch_Pagination(t *testing.T) {
	t.Parallel()

	var capturedRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOpenverseJSON(nil))
	}))
	t.Cleanup(srv.Close)

	p := &OpenverseProvider{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, _ = p.Search(context.Background(), "forest", SearchOpts{PageNumber: 3})

	q, _ := url.ParseQuery(capturedRawQuery)
	if q.Get("page") != "3" {
		t.Errorf("page param = %q, want %q", q.Get("page"), "3")
	}
}

// TestOpenverseProviderSearch_PageNumberDefaultsToOne verifies that a zero PageNumber
// sends page=1 in the request.
func TestOpenverseProviderSearch_PageNumberDefaultsToOne(t *testing.T) {
	t.Parallel()

	var capturedRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOpenverseJSON(nil))
	}))
	t.Cleanup(srv.Close)

	p := &OpenverseProvider{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, _ = p.Search(context.Background(), "sky", SearchOpts{PageNumber: 0})

	q, _ := url.ParseQuery(capturedRawQuery)
	if q.Get("page") != "1" {
		t.Errorf("page param = %q, want %q (zero should default to 1)", q.Get("page"), "1")
	}
}

// TestOpenverseProviderSearch_LogoBannerFiltered verifies that logo/banner URLs are excluded.
func TestOpenverseProviderSearch_LogoBannerFiltered(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOpenverseJSON([]openverseResult{
			{URL: "https://example.com/logo.png", ForeignLandingURL: "https://example.com/", Title: "Logo"},
			{URL: "https://example.com/banner.jpg", ForeignLandingURL: "https://example.com/", Title: "Banner"},
			{URL: "https://example.com/real-photo.jpg", ForeignLandingURL: "https://example.com/p", Title: "Real Photo"},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &OpenverseProvider{BaseURL: srv.URL, HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "logo test", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1 (only real-photo.jpg should pass)", len(candidates))
	}
	if candidates[0].Title != "Real Photo" {
		t.Errorf("surviving candidate Title = %q, want %q", candidates[0].Title, "Real Photo")
	}
}

// TestOpenverseProviderSearch_EmptyURLSkipped verifies that results with no URL are ignored.
func TestOpenverseProviderSearch_EmptyURLSkipped(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOpenverseJSON([]openverseResult{
			{URL: "", Title: "No URL result"},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &OpenverseProvider{BaseURL: srv.URL, HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "test", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("got %d candidates for empty URL, want 0", len(candidates))
	}
}

// TestOpenverseProviderSearch_HTTPError verifies that a non-200 status propagates as an error.
func TestOpenverseProviderSearch_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	p := &OpenverseProvider{BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := p.Search(context.Background(), "test", SearchOpts{})

	if err == nil {
		t.Error("expected error for 500 response, got nil")
	}
}

// TestOpenverseProviderSearch_ConnectionError verifies that a connection error is returned.
func TestOpenverseProviderSearch_ConnectionError(t *testing.T) {
	t.Parallel()

	p := &OpenverseProvider{BaseURL: "http://127.0.0.1:1"} // nothing listening
	_, err := p.Search(context.Background(), "test", SearchOpts{})

	if err == nil {
		t.Error("expected a connection error with nothing listening on port 1, got nil")
	}
}

// TestOpenverseProviderSearch_EmptyResults verifies that an empty results array returns nil.
func TestOpenverseProviderSearch_EmptyResults(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOpenverseJSON(nil))
	}))
	t.Cleanup(srv.Close)

	p := &OpenverseProvider{BaseURL: srv.URL, HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "nothing here", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("got %d candidates for empty results, want 0", len(candidates))
	}
}

// TestOpenverseProviderSearch_DefaultBaseURL verifies that empty BaseURL falls back to the default.
func TestOpenverseProviderSearch_DefaultBaseURL(t *testing.T) {
	t.Parallel()

	// We just verify that the provider builds the right URL structure by checking buildURL directly.
	p := &OpenverseProvider{} // BaseURL intentionally empty
	got := p.buildURL("cats", SearchOpts{PageNumber: 1})
	want := "https://api.openverse.org/v1/images/?q=cats&page=1&page_size=20"
	if got != want {
		t.Errorf("buildURL() = %q, want %q", got, want)
	}
}

// TestOpenverseProviderSearch_NilHTTPClient verifies that nil HTTPClient falls back gracefully.
func TestOpenverseProviderSearch_NilHTTPClient(t *testing.T) {
	t.Parallel()

	p := &OpenverseProvider{BaseURL: "http://127.0.0.1:1"} // nothing listening; nil HTTPClient
	_, err := p.Search(context.Background(), "test", SearchOpts{})
	// We expect an error (connection refused), not a panic.
	if err == nil {
		t.Error("expected a connection error with nil HTTPClient and nothing listening on port 1, got nil")
	}
}
