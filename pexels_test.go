package imagefy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// buildPexelsOfficialJSON encodes photos into the Pexels official API response format.
func buildPexelsOfficialJSON(photos []pexelsOfficialPhoto) []byte {
	body, _ := json.Marshal(map[string]any{"photos": photos})
	return body
}

// buildPexelsInternalJSON encodes items into the Pexels internal API response format.
func buildPexelsInternalJSON(items []pexelsInternalItem) []byte {
	body, _ := json.Marshal(map[string]any{"data": items})
	return body
}

// TestPexelsProviderName verifies the provider name.
func TestPexelsProviderName(t *testing.T) {
	t.Parallel()

	p := &PexelsProvider{}
	if p.Name() != "pexels" {
		t.Errorf("Name() = %q, want %q", p.Name(), "pexels")
	}
}

// TestPexelsProviderSearch_OfficialAPI tests searchOfficial with httptest,
// verifies Authorization header and field mapping.
func TestPexelsProviderSearch_OfficialAPI(t *testing.T) {
	t.Parallel()

	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsOfficialJSON([]pexelsOfficialPhoto{
			{
				ID:  12345,
				Alt: "Beautiful sunset",
				URL: "https://www.pexels.com/photo/beautiful-sunset-12345/",
				Src: pexelsSrc{
					Large: "https://images.pexels.com/photos/12345/large.jpeg",
					Small: "https://images.pexels.com/photos/12345/small.jpeg",
				},
			},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{
		APIKey:       "test-api-key",
		HTTPClient:   srv.Client(),
		officialBase: srv.URL,
	}
	candidates, err := p.searchOfficial(context.Background(), srv.URL, "sunset", SearchOpts{})
	if err != nil {
		t.Fatalf("searchOfficial returned error: %v", err)
	}

	if capturedAuth != "test-api-key" {
		t.Errorf("Authorization header = %q, want %q", capturedAuth, "test-api-key")
	}
	if len(candidates) == 0 {
		t.Fatal("searchOfficial returned no candidates, expected 1")
	}

	got := candidates[0]
	if got.ImgURL != "https://images.pexels.com/photos/12345/large.jpeg" {
		t.Errorf("ImgURL = %q, want large src", got.ImgURL)
	}
	if got.Thumbnail != "https://images.pexels.com/photos/12345/small.jpeg" {
		t.Errorf("Thumbnail = %q, want small src", got.Thumbnail)
	}
	if got.Source != "https://www.pexels.com/photo/beautiful-sunset-12345/" {
		t.Errorf("Source = %q, want photo URL", got.Source)
	}
	if got.Title != "Beautiful sunset" {
		t.Errorf("Title = %q, want %q", got.Title, "Beautiful sunset")
	}
	if got.License != LicenseSafe {
		t.Errorf("License = %v, want LicenseSafe", got.License)
	}
}

// TestPexelsProviderSearch_InternalAPI tests searchInternal with httptest,
// verifies Secret-Key header and field mapping.
func TestPexelsProviderSearch_InternalAPI(t *testing.T) {
	t.Parallel()

	var capturedSecret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSecret = r.Header.Get("Secret-Key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsInternalJSON([]pexelsInternalItem{
			{Attributes: pexelsInternalAttrs{
				ID:    67890,
				Slug:  "mountain-lake",
				Title: "Mountain Lake",
				Image: pexelsInternalImage{
					Small:        "https://images.pexels.com/photos/67890/small.jpeg",
					DownloadLink: "https://images.pexels.com/photos/67890/download.jpeg",
				},
				User: pexelsInternalUser{Username: "photographer"},
			}},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{
		SecretKey:    "test-secret-key",
		HTTPClient:   srv.Client(),
		internalBase: srv.URL,
	}
	candidates, err := p.searchInternal(context.Background(), srv.URL, "mountain", SearchOpts{})
	if err != nil {
		t.Fatalf("searchInternal returned error: %v", err)
	}

	if capturedSecret != "test-secret-key" {
		t.Errorf("Secret-Key header = %q, want %q", capturedSecret, "test-secret-key")
	}
	if len(candidates) == 0 {
		t.Fatal("searchInternal returned no candidates, expected 1")
	}

	got := candidates[0]
	if got.ImgURL != "https://images.pexels.com/photos/67890/download.jpeg" {
		t.Errorf("ImgURL = %q, want download_link", got.ImgURL)
	}
	if got.Thumbnail != "https://images.pexels.com/photos/67890/small.jpeg" {
		t.Errorf("Thumbnail = %q, want small image", got.Thumbnail)
	}
	if got.Source != "https://www.pexels.com/photo/mountain-lake-67890/" {
		t.Errorf("Source = %q, want constructed photo URL", got.Source)
	}
	if got.Title != "Mountain Lake" {
		t.Errorf("Title = %q, want %q", got.Title, "Mountain Lake")
	}
	if got.License != LicenseSafe {
		t.Errorf("License = %v, want LicenseSafe", got.License)
	}
}

// TestPexelsProviderSearch_PrefersOfficialAPI verifies Search() calls official first when both keys set.
func TestPexelsProviderSearch_PrefersOfficialAPI(t *testing.T) {
	t.Parallel()

	var usedAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			usedAuth = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsOfficialJSON([]pexelsOfficialPhoto{
			{ID: 1, Alt: "Photo", URL: "https://pexels.com/photo/1/", Src: pexelsSrc{Large: "https://img.pexels.com/1.jpg"}},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{
		APIKey:       "key",
		SecretKey:    "secret",
		HTTPClient:   srv.Client(),
		officialBase: srv.URL,
		internalBase: srv.URL,
	}
	candidates, err := p.Search(context.Background(), "test", SearchOpts{})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !usedAuth {
		t.Error("expected official API (Authorization header) to be used first")
	}
	if len(candidates) == 0 {
		t.Error("expected candidates from official API")
	}
}

// TestPexelsProviderSearch_FallsBackToInternal verifies 429 on official triggers internal fallback.
func TestPexelsProviderSearch_FallsBackToInternal(t *testing.T) {
	t.Parallel()

	officialSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(officialSrv.Close)

	internalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsInternalJSON([]pexelsInternalItem{
			{Attributes: pexelsInternalAttrs{
				ID: 99, Slug: "fallback", Title: "Fallback Photo",
				Image: pexelsInternalImage{DownloadLink: "https://img.pexels.com/99.jpg"},
			}},
		}))
	}))
	t.Cleanup(internalSrv.Close)

	p := &PexelsProvider{
		APIKey:       "key",
		SecretKey:    "secret",
		HTTPClient:   http.DefaultClient,
		officialBase: officialSrv.URL,
		internalBase: internalSrv.URL,
	}
	candidates, err := p.Search(context.Background(), "test", SearchOpts{})
	if err != nil {
		t.Fatalf("Search returned error: %v (expected fallback to internal)", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected candidates from internal API fallback")
	}
	if candidates[0].Title != "Fallback Photo" {
		t.Errorf("Title = %q, want %q", candidates[0].Title, "Fallback Photo")
	}
}

// TestPexelsProviderSearch_InternalOnlyWhenNoAPIKey verifies no APIKey uses internal directly.
func TestPexelsProviderSearch_InternalOnlyWhenNoAPIKey(t *testing.T) {
	t.Parallel()

	var usedSecret bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Secret-Key") != "" {
			usedSecret = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buildPexelsInternalJSON([]pexelsInternalItem{
			{Attributes: pexelsInternalAttrs{
				ID: 42, Slug: "internal-only", Title: "Internal Only",
				Image: pexelsInternalImage{DownloadLink: "https://img.pexels.com/42.jpg"},
			}},
		}))
	}))
	t.Cleanup(srv.Close)

	p := &PexelsProvider{
		SecretKey:    "secret",
		HTTPClient:   srv.Client(),
		internalBase: srv.URL,
	}
	candidates, err := p.Search(context.Background(), "test", SearchOpts{})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !usedSecret {
		t.Error("expected internal API (Secret-Key header) to be used")
	}
	if len(candidates) == 0 {
		t.Error("expected candidates from internal API")
	}
}

// TestPexelsProviderSearch_NoBothKeys verifies that no keys configured returns an error.
func TestPexelsProviderSearch_NoBothKeys(t *testing.T) {
	t.Parallel()

	p := &PexelsProvider{}
	_, err := p.Search(context.Background(), "test", SearchOpts{})
	if err == nil {
		t.Error("expected error when no API key or secret key configured, got nil")
	}
}
