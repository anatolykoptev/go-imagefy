package imagefy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
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

// captureRequestURL runs a search against a mock server and returns the parsed URL
// that the server received, so tests can inspect query parameters.
func captureRequestURL(t *testing.T, cfg *Config, opts SearchOpts) *url.URL {
	t.Helper()
	var captured *url.URL

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(searxngResponse(nil))
	}))
	t.Cleanup(srv.Close)

	cfg.SearxngURL = srv.URL
	cfg.HTTPClient = srv.Client()
	_ = cfg.SearchImagesWithOpts(context.Background(), "test", 5, opts)

	if captured == nil {
		t.Fatal("mock server was never reached")
	}
	return captured
}

func TestSearchImagesWithOptsDefaultsMatchSearchImages(t *testing.T) {
	t.Parallel()

	// Zero SearchOpts → same URL as SearchImages (page 1, no engines param).
	got := captureRequestURL(t, &Config{}, SearchOpts{})

	if v := got.Query().Get("pageno"); v != "" {
		t.Errorf("pageno param present with zero opts, want absent, got %q", v)
	}
	if v := got.Query().Get("engines"); v != "" {
		t.Errorf("engines param present with zero opts, want absent, got %q", v)
	}
}

func TestSearchImagesWithOptsPagination(t *testing.T) {
	t.Parallel()

	got := captureRequestURL(t, &Config{}, SearchOpts{PageNumber: 3})

	if v := got.Query().Get("pageno"); v != "3" {
		t.Errorf("pageno = %q, want %q", v, "3")
	}
}

func TestSearchImagesWithOptsPageOne_NoPagenoParam(t *testing.T) {
	t.Parallel()

	// PageNumber=1 is default — should NOT add &pageno=1.
	got := captureRequestURL(t, &Config{}, SearchOpts{PageNumber: 1})

	if v := got.Query().Get("pageno"); v != "" {
		t.Errorf("pageno param present for PageNumber=1, want absent, got %q", v)
	}
}

func TestSearchImagesWithOptsEngines(t *testing.T) {
	t.Parallel()

	got := captureRequestURL(t, &Config{}, SearchOpts{Engines: []string{"bing", "google"}})

	engines := got.Query().Get("engines")
	if engines != "bing,google" {
		t.Errorf("engines = %q, want %q", engines, "bing,google")
	}
}

func TestSearchImagesWithOptsPaginationAndEngines(t *testing.T) {
	t.Parallel()

	got := captureRequestURL(t, &Config{}, SearchOpts{PageNumber: 2, Engines: []string{"flickr"}})

	if v := got.Query().Get("pageno"); v != "2" {
		t.Errorf("pageno = %q, want %q", v, "2")
	}
	if v := got.Query().Get("engines"); v != "flickr" {
		t.Errorf("engines = %q, want %q", v, "flickr")
	}
}

func TestSearchImagesWithOptsCustomTimeout(t *testing.T) {
	t.Parallel()

	// A very short timeout should cause the search to time out quickly.
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client context is cancelled.
		<-r.Context().Done()
		w.WriteHeader(http.StatusRequestTimeout)
	}))
	defer slowSrv.Close()

	cfg := &Config{
		SearxngURL: slowSrv.URL,
		HTTPClient: slowSrv.Client(),
	}

	start := time.Now()
	results := cfg.SearchImagesWithOpts(context.Background(), "test", 5, SearchOpts{
		Timeout: 50 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if results != nil {
		t.Errorf("expected nil results on timeout, got %v", results)
	}
	const maxWait = 2 * time.Second
	if elapsed >= maxWait {
		t.Errorf("timed out after %v; custom timeout of 50ms should have fired much sooner", elapsed)
	}
}

func TestSearchImagesWithOptsZeroTimeout_UsesDefault(t *testing.T) {
	t.Parallel()

	// Zero Timeout in opts should fall back to searxngTimeout (15s).
	// We just verify the search completes normally (no immediate context cancellation).
	imgSrv := newJPEGServer(t)
	imgURL := imgSrv.URL + "/photo.jpg"

	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(searxngResponse([]map[string]string{
			{"img_src": imgURL, "url": imgSrv.URL + "/page", "title": "Photo"},
		}))
	}))
	defer searxSrv.Close()

	cfg := &Config{
		SearxngURL: searxSrv.URL,
		HTTPClient: searxSrv.Client(),
	}

	results := cfg.SearchImagesWithOpts(context.Background(), "test", 5, SearchOpts{Timeout: 0})
	if len(results) == 0 {
		t.Error("expected at least 1 result with zero Timeout (should use default 15s)")
	}
}

func TestSearchImages_ExtraBlockedDomain(t *testing.T) {
	t.Parallel()

	// Image server that serves a valid JPEG.
	imgSrv := newJPEGServer(t)
	imgURL := imgSrv.URL + "/photo.jpg"

	// SearXNG returns a result from a custom domain (not in built-in blocked list).
	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(searxngResponse([]map[string]string{
			{
				"img_src": imgURL,
				"url":     "https://mystock.example.com/page/123",
				"title":   "Custom Stock Photo",
			},
		}))
	}))
	defer searxSrv.Close()

	var mu sync.Mutex
	var events []ClassificationEvent

	cfg := &Config{
		SearxngURL:          searxSrv.URL,
		HTTPClient:          searxSrv.Client(),
		ExtraBlockedDomains: []string{"mystock.example.com"},
		OnClassification: func(ev ClassificationEvent) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		},
	}

	results := cfg.SearchImages(context.Background(), "custom stock", 5)
	if len(results) != 0 {
		t.Errorf("SearchImages returned %d results for extra blocked domain, want 0", len(results))
	}

	mu.Lock()
	defer mu.Unlock()
	// Verify the classification event was emitted with the correct source.
	found := false
	for _, ev := range events {
		if ev.Source == "license_assessment" && ev.Class == ClassStock {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ClassificationEvent with Source=%q and Class=%q, got events: %+v",
			"license_assessment", ClassStock, events)
	}
}

func TestSearchImages_MetadataPassthrough(t *testing.T) {
	t.Parallel()

	// Image server that serves a plain JPEG (no metadata).
	imgSrv := newJPEGServer(t)
	imgURL := imgSrv.URL + "/photo.jpg"

	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(searxngResponse([]map[string]string{
			{
				"img_src": imgURL,
				"url":     imgSrv.URL + "/page",
				"title":   "Plain Photo",
			},
		}))
	}))
	defer searxSrv.Close()

	cfg := &Config{
		SearxngURL: searxSrv.URL,
		HTTPClient: searxSrv.Client(),
	}

	results := cfg.SearchImages(context.Background(), "plain photo", 5)
	if len(results) == 0 {
		t.Error("SearchImages returned no results, expected at least 1 with plain JPEG (no blocking metadata)")
	}
	if len(results) > 0 && results[0].ImgURL != imgURL {
		t.Errorf("result ImgURL = %q, want %q", results[0].ImgURL, imgURL)
	}
}
