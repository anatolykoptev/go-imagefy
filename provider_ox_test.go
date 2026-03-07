package imagefy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// buildOxResponse encodes a slice of oxImageResult items into the ox-browser JSON response format.
func buildOxResponse(images []oxImageResult) []byte {
	body, _ := json.Marshal(oxSearchResponse{Images: images})
	return body
}

// TestOxBrowserProvider_Name verifies the provider name.
func TestOxBrowserProvider_Name(t *testing.T) {
	t.Parallel()

	p := &OxBrowserProvider{}
	if p.Name() != "ox-browser" {
		t.Errorf("Name() = %q, want %q", p.Name(), "ox-browser")
	}
}

// TestOxBrowserProvider_Search verifies happy path: 3 images returned, fields mapped correctly.
func TestOxBrowserProvider_Search(t *testing.T) {
	t.Parallel()

	var capturedBody oxSearchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOxResponse([]oxImageResult{
			{
				URL:       "https://example.com/photo1.jpg",
				Thumbnail: "https://example.com/thumb1.jpg",
				Source:    "https://example.com/page1",
				Title:     "Mountain Sunrise",
				Width:     1920,
				Height:    1080,
				Engine:    "bing",
			},
			{
				URL:       "https://example.org/photo2.jpg",
				Thumbnail: "https://example.org/thumb2.jpg",
				Source:    "https://example.org/page2",
				Title:     "Forest Path",
				Width:     1600,
				Height:    900,
				Engine:    "ddg",
			},
			{
				URL:       "https://example.net/photo3.jpg",
				Thumbnail: "https://example.net/thumb3.jpg",
				Source:    "https://example.net/page3",
				Title:     "Ocean Waves",
				Width:     2560,
				Height:    1440,
				Engine:    "yandex",
			},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &OxBrowserProvider{
		BaseURL:    srv.URL,
		Engines:    []string{"bing", "ddg", "yandex"},
		MaxResults: 5,
		Client:     srv.Client(),
	}
	candidates, err := p.Search(context.Background(), "nature", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("got %d candidates, want 3", len(candidates))
	}

	// Verify request body was sent correctly.
	if capturedBody.Query != "nature" {
		t.Errorf("request query = %q, want %q", capturedBody.Query, "nature")
	}
	if capturedBody.MaxResults != 5 {
		t.Errorf("request max_results = %d, want 5", capturedBody.MaxResults)
	}

	// Verify first candidate fields.
	got := candidates[0]
	if got.ImgURL != "https://example.com/photo1.jpg" {
		t.Errorf("ImgURL = %q, unexpected value", got.ImgURL)
	}
	if got.Thumbnail != "https://example.com/thumb1.jpg" {
		t.Errorf("Thumbnail = %q, unexpected value", got.Thumbnail)
	}
	if got.Source != "https://example.com/page1" {
		t.Errorf("Source = %q, unexpected value", got.Source)
	}
	if got.Title != "Mountain Sunrise" {
		t.Errorf("Title = %q, want %q", got.Title, "Mountain Sunrise")
	}
}

// TestOxBrowserProvider_DefaultMaxResults verifies that zero MaxResults defaults to 10.
func TestOxBrowserProvider_DefaultMaxResults(t *testing.T) {
	t.Parallel()

	var capturedBody oxSearchRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOxResponse(nil))
	}))
	t.Cleanup(srv.Close)

	p := &OxBrowserProvider{BaseURL: srv.URL, Client: srv.Client()} // MaxResults deliberately zero
	_, _ = p.Search(context.Background(), "test", SearchOpts{})

	if capturedBody.MaxResults != oxDefaultMaxResults {
		t.Errorf("max_results = %d, want %d (default)", capturedBody.MaxResults, oxDefaultMaxResults)
	}
}

// TestOxBrowserProvider_EmptyResults verifies that an empty images array returns nil candidates without error.
func TestOxBrowserProvider_EmptyResults(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOxResponse(nil))
	}))
	t.Cleanup(srv.Close)

	p := &OxBrowserProvider{BaseURL: srv.URL, Client: srv.Client()}
	candidates, err := p.Search(context.Background(), "nothing here", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("got %d candidates for empty results, want 0", len(candidates))
	}
}

// TestOxBrowserProvider_ServerError verifies that a 500 response propagates as an error.
func TestOxBrowserProvider_ServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	p := &OxBrowserProvider{BaseURL: srv.URL, Client: srv.Client()}
	_, err := p.Search(context.Background(), "test", SearchOpts{})

	if err == nil {
		t.Error("expected error for 500 response, got nil")
	}
}

// TestOxBrowserProvider_FilterBlockedDomain verifies that images from blocked stock sites are excluded.
func TestOxBrowserProvider_FilterBlockedDomain(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOxResponse([]oxImageResult{
			{
				URL:    "https://www.shutterstock.com/image-photo/mountains-12345.jpg",
				Source: "https://www.shutterstock.com/photos/mountains",
				Title:  "Shutterstock Mountains",
			},
			{
				URL:    "https://example.com/real-photo.jpg",
				Source: "https://example.com/gallery",
				Title:  "Real Photo",
			},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &OxBrowserProvider{BaseURL: srv.URL, Client: srv.Client()}
	candidates, err := p.Search(context.Background(), "mountains", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1 (shutterstock must be filtered)", len(candidates))
	}
	if candidates[0].Title != "Real Photo" {
		t.Errorf("surviving candidate Title = %q, want %q", candidates[0].Title, "Real Photo")
	}
	if candidates[0].License == LicenseBlocked {
		t.Errorf("surviving candidate License = %v, should not be blocked", candidates[0].License)
	}
}

// TestOxBrowserProvider_FilterLogoBanner verifies that logo/banner URLs are excluded.
func TestOxBrowserProvider_FilterLogoBanner(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildOxResponse([]oxImageResult{
			{URL: "https://example.com/logo.png", Source: "https://example.com/", Title: "Logo"},
			{URL: "https://example.com/photo.jpg", Source: "https://example.com/gallery", Title: "Photo"},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &OxBrowserProvider{BaseURL: srv.URL, Client: srv.Client()}
	candidates, err := p.Search(context.Background(), "test", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1 (logo must be filtered)", len(candidates))
	}
	if candidates[0].Title != "Photo" {
		t.Errorf("surviving candidate Title = %q, want %q", candidates[0].Title, "Photo")
	}
}

// TestOxBrowserProvider_NilClient verifies that nil Client falls back to http.DefaultClient without panic.
func TestOxBrowserProvider_NilClient(t *testing.T) {
	t.Parallel()

	p := &OxBrowserProvider{BaseURL: "http://127.0.0.1:1"} // nothing listening; nil Client
	_, err := p.Search(context.Background(), "test", SearchOpts{})
	if err == nil {
		t.Error("expected a connection error with nothing listening on port 1, got nil")
	}
}
