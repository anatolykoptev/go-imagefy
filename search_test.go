package imagefy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// searxngResponse builds a JSON response body for the mock SearXNG server.
func searxngResponse(results []map[string]string) []byte {
	type resultItem struct {
		ImgSrc    string `json:"img_src"`
		Thumbnail string `json:"thumbnail_src"`
		URL       string `json:"url"`
		Title     string `json:"title"`
	}
	var items []resultItem
	for _, r := range results {
		items = append(items, resultItem{
			ImgSrc:    r["img_src"],
			Thumbnail: r["thumbnail_src"],
			URL:       r["url"],
			Title:     r["title"],
		})
	}
	body, _ := json.Marshal(map[string]any{"results": items})
	return body
}

// newJPEGServer starts a test HTTP server that serves a minimal JPEG payload.
// The server is closed automatically via t.Cleanup.
func newJPEGServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		// 1 KB payload — enough to pass MinBytes=0 check.
		_, _ = w.Write(make([]byte, 1024))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSearchImagesReturnsResults(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)
	imgURL := imgSrv.URL + "/photo.jpg"

	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(searxngResponse([]map[string]string{
			{"img_src": imgURL, "url": imgSrv.URL + "/page", "title": "Test Photo"},
		}))
	}))
	defer searxSrv.Close()

	cfg := &Config{
		SearxngURL: searxSrv.URL,
		HTTPClient: searxSrv.Client(),
	}

	results := cfg.SearchImages(context.Background(), "test photo", 5)
	if len(results) == 0 {
		t.Error("SearchImages returned no results, expected at least 1")
	}
	if len(results) > 0 && results[0].ImgURL != imgURL {
		t.Errorf("result ImgURL = %q, want %q", results[0].ImgURL, imgURL)
	}
}

func TestSearchImagesBlockedDomainsExcluded(t *testing.T) {
	t.Parallel()

	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(searxngResponse([]map[string]string{
			{
				"img_src": "https://shutterstock.com/image/photo.jpg",
				"url":     "https://shutterstock.com/page/123",
				"title":   "Stock Photo",
			},
		}))
	}))
	defer searxSrv.Close()

	cfg := &Config{
		SearxngURL: searxSrv.URL,
		HTTPClient: searxSrv.Client(),
	}

	results := cfg.SearchImages(context.Background(), "stock photo", 5)
	if len(results) != 0 {
		t.Errorf("SearchImages returned %d results for blocked domain, want 0", len(results))
	}
}

func TestSearchImagesEmptyQueryReturnsNil(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		SearxngURL: "http://localhost:9999", // never reached
	}

	results := cfg.SearchImages(context.Background(), "", 5)
	if results != nil {
		t.Errorf("SearchImages with empty query = %v, want nil", results)
	}
}

func TestSearchImagesSearxngErrorReturnsNil(t *testing.T) {
	t.Parallel()

	// Server that always returns 500 — JSON parse fails → nil.
	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer searxSrv.Close()

	cfg := &Config{
		SearxngURL: searxSrv.URL,
		HTTPClient: searxSrv.Client(),
	}

	results := cfg.SearchImages(context.Background(), "test", 5)
	if results != nil {
		t.Errorf("SearchImages with SearXNG error = %v, want nil", results)
	}
}

func TestSearchImagesOnImageSearchCallback(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)

	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(searxngResponse([]map[string]string{
			{"img_src": imgSrv.URL + "/photo.jpg", "url": imgSrv.URL + "/page", "title": "Photo"},
		}))
	}))
	defer searxSrv.Close()

	callCount := 0
	cfg := &Config{
		SearxngURL:    searxSrv.URL,
		HTTPClient:    searxSrv.Client(),
		OnImageSearch: func() { callCount++ },
	}

	cfg.SearchImages(context.Background(), "test", 5)

	if callCount != 1 {
		t.Errorf("OnImageSearch called %d times, want 1", callCount)
	}
}

func TestSearchImagesLogoURLSkipped(t *testing.T) {
	t.Parallel()

	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(searxngResponse([]map[string]string{
			{"img_src": "https://example.com/logo.jpg", "url": "https://example.com/page", "title": "Logo"},
		}))
	}))
	defer searxSrv.Close()

	cfg := &Config{
		SearxngURL: searxSrv.URL,
		HTTPClient: searxSrv.Client(),
	}

	results := cfg.SearchImages(context.Background(), "logo", 5)
	if len(results) != 0 {
		t.Errorf("SearchImages returned %d results for logo URL, want 0", len(results))
	}
}
