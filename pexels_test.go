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
