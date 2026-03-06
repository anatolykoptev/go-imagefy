package imagefy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockProvider is a SearchProvider that returns a fixed set of candidates or an error.
type mockProvider struct {
	name       string
	candidates []ImageCandidate
	err        error
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Search(_ context.Context, _ string, _ SearchOpts) ([]ImageCandidate, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.candidates, nil
}

// buildSearxngJSON encodes a slice of searxngResult items into the SearXNG JSON response format.
func buildSearxngJSON(items []searxngResult) []byte {
	body, _ := json.Marshal(map[string]any{"results": items})
	return body
}

// TestSearXNGProviderSearch_HappyPath verifies that a valid SearXNG response is parsed
// into candidates with the correct fields.
func TestSearXNGProviderSearch_HappyPath(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)
	imgURL := imgSrv.URL + "/photo.jpg"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildSearxngJSON([]searxngResult{
			{ImgSrc: imgURL, URL: imgSrv.URL + "/page", Title: "Nice Photo"},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &SearXNGProvider{URL: srv.URL, HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "nature", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("Search returned no candidates, expected 1")
	}
	if candidates[0].ImgURL != imgURL {
		t.Errorf("ImgURL = %q, want %q", candidates[0].ImgURL, imgURL)
	}
	if candidates[0].Title != "Nice Photo" {
		t.Errorf("Title = %q, want %q", candidates[0].Title, "Nice Photo")
	}
}

// TestSearXNGProviderSearch_BlockedLicenseExcluded verifies that results from blocked
// stock domains are filtered out by the provider before returning.
func TestSearXNGProviderSearch_BlockedLicenseExcluded(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildSearxngJSON([]searxngResult{
			{
				ImgSrc: "https://shutterstock.com/image/photo.jpg",
				URL:    "https://shutterstock.com/page/123",
				Title:  "Stock Photo",
			},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &SearXNGProvider{URL: srv.URL, HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "stock", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("got %d candidates for blocked domain, want 0", len(candidates))
	}
}

// TestSearXNGProviderSearch_LogoURLExcluded verifies that logo/banner URLs are filtered.
func TestSearXNGProviderSearch_LogoURLExcluded(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildSearxngJSON([]searxngResult{
			{ImgSrc: "https://example.com/logo.png", URL: "https://example.com/", Title: "Logo"},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &SearXNGProvider{URL: srv.URL, HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "logo", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("got %d candidates for logo URL, want 0", len(candidates))
	}
}

// TestSearXNGProviderSearch_EmptyImgSrcSkipped verifies results with no img_src are ignored.
func TestSearXNGProviderSearch_EmptyImgSrcSkipped(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildSearxngJSON([]searxngResult{
			{ImgSrc: "", URL: "https://example.com/page", Title: "No Image"},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &SearXNGProvider{URL: srv.URL, HTTPClient: srv.Client()}
	candidates, err := p.Search(context.Background(), "test", SearchOpts{})

	if err != nil {
		t.Fatalf("Search returned unexpected error: %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("got %d candidates for empty img_src, want 0", len(candidates))
	}
}

// TestSearXNGProviderSearch_HTTPError verifies that an HTTP/parse error is propagated as an error.
func TestSearXNGProviderSearch_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	p := &SearXNGProvider{URL: srv.URL, HTTPClient: srv.Client()}
	_, err := p.Search(context.Background(), "test", SearchOpts{})

	if err == nil {
		t.Error("expected error for 500 response, got nil")
	}
}

// TestSearXNGProviderSearch_PaginationParams verifies that PageNumber > 1 appends pageno param.
func TestSearXNGProviderSearch_PaginationParams(t *testing.T) {
	t.Parallel()

	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildSearxngJSON(nil))
	}))
	t.Cleanup(srv.Close)

	p := &SearXNGProvider{URL: srv.URL, HTTPClient: srv.Client()}
	_, _ = p.Search(context.Background(), "forest", SearchOpts{PageNumber: 3, Engines: []string{"bing"}})

	q, _ := parseQuery(capturedQuery)
	if q.Get("pageno") != "3" {
		t.Errorf("pageno = %q, want %q", q.Get("pageno"), "3")
	}
	if q.Get("engines") != "bing" {
		t.Errorf("engines = %q, want %q", q.Get("engines"), "bing")
	}
}

// parseQuery is a local helper to avoid import cycles; it parses a raw query string.
func parseQuery(raw string) (interface{ Get(string) string }, error) {
	type queryValues map[string][]string
	vals := make(queryValues)
	for _, kv := range splitAmpersand(raw) {
		if k, v, ok := splitEqual(kv); ok {
			vals[k] = append(vals[k], v)
		}
	}
	return &queryGetter{vals: vals}, nil
}

type queryGetter struct{ vals map[string][]string }

func (q *queryGetter) Get(key string) string {
	if v, ok := q.vals[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

func splitAmpersand(s string) []string {
	var out []string
	start := 0
	for i := range len(s) {
		if s[i] == '&' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func splitEqual(s string) (k, v string, ok bool) {
	for i := range len(s) {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

// TestBackwardCompatSearxngURL verifies that Config.SearxngURL alone (without Providers)
// still drives searches — the SearXNGProvider is auto-created.
func TestBackwardCompatSearxngURL(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)
	imgURL := imgSrv.URL + "/photo.jpg"

	searxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(searxngResponse([]map[string]string{
			{"img_src": imgURL, "url": imgSrv.URL + "/page", "title": "Back Compat Photo"},
		}))
	}))
	t.Cleanup(searxSrv.Close)

	cfg := &Config{
		SearxngURL: searxSrv.URL,
		HTTPClient: searxSrv.Client(),
		// Providers intentionally omitted
	}

	results := cfg.SearchImages(context.Background(), "compat test", 5)
	if len(results) == 0 {
		t.Error("backward compat: SearchImages returned no results, expected at least 1")
	}
}

// TestMultiProviderResultsMerged verifies that results from multiple providers are all returned.
func TestMultiProviderResultsMerged(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)

	cand1 := ImageCandidate{ImgURL: imgSrv.URL + "/photo1.jpg", Source: imgSrv.URL + "/p1", License: LicenseSafe}
	cand2 := ImageCandidate{ImgURL: imgSrv.URL + "/photo2.jpg", Source: imgSrv.URL + "/p2", License: LicenseSafe}

	cfg := &Config{
		HTTPClient: imgSrv.Client(),
		Providers: []SearchProvider{
			&mockProvider{name: "provider-a", candidates: []ImageCandidate{cand1}},
			&mockProvider{name: "provider-b", candidates: []ImageCandidate{cand2}},
		},
	}

	results := cfg.SearchImages(context.Background(), "multi", 10)
	if len(results) < 2 {
		t.Errorf("got %d results from two providers, want at least 2", len(results))
	}
}

// TestMultiProviderOneFailsOtherSucceeds verifies that if one provider errors, the remaining
// provider's results are still returned.
func TestMultiProviderOneFailsOtherSucceeds(t *testing.T) {
	t.Parallel()

	imgSrv := newJPEGServer(t)
	goodCand := ImageCandidate{ImgURL: imgSrv.URL + "/good.jpg", Source: imgSrv.URL + "/page", License: LicenseSafe}

	cfg := &Config{
		HTTPClient: imgSrv.Client(),
		Providers: []SearchProvider{
			&mockProvider{name: "failing", err: errProviderFailed},
			&mockProvider{name: "healthy", candidates: []ImageCandidate{goodCand}},
		},
	}

	results := cfg.SearchImages(context.Background(), "resilience", 5)
	if len(results) == 0 {
		t.Error("expected results from healthy provider after failing provider errored, got none")
	}
}

// errProviderFailed is a sentinel error for test mocks.
var errProviderFailed = &providerTestError{"mock provider failure"}

type providerTestError struct{ msg string }

func (e *providerTestError) Error() string { return e.msg }

// TestSearXNGProviderName verifies the provider name.
func TestSearXNGProviderName(t *testing.T) {
	t.Parallel()

	p := &SearXNGProvider{URL: "http://example.com"}
	if p.Name() != "searxng" {
		t.Errorf("Name() = %q, want %q", p.Name(), "searxng")
	}
}

// TestSearXNGProviderNilHTTPClientUsesDefault verifies that a nil HTTPClient falls back
// gracefully (the provider does not panic; it may fail to connect to a fake URL).
func TestSearXNGProviderNilHTTPClientUsesDefault(t *testing.T) {
	t.Parallel()

	p := &SearXNGProvider{URL: "http://127.0.0.1:1"} // nothing listening
	_, err := p.Search(context.Background(), "test", SearchOpts{})
	// We expect an error (connection refused), not a panic.
	if err == nil {
		t.Error("expected a connection error with nothing listening on port 1, got nil")
	}
}
