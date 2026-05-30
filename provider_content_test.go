package imagefy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pageWithBothImages is an HTML fixture that has:
// - a placeholder og:image (kp.ru logo style)
// - a real content <img> on the same CDN domain as the page
// - a second content image that slug-matches the page path
const pageWithBothImages = `<!DOCTYPE html>
<html>
<head>
  <meta property="og:image" content="https://cdn.example.com/placeholder/kp.jpg">
  <meta name="twitter:image" content="https://cdn.example.com/placeholder/kp.jpg">
</head>
<body>
  <article>
    <img src="https://cdn.example.com/events/igrok-foto-teatr-2024.jpg" width="800" height="600" alt="Спектакль Игрок">
    <img src="https://cdn.example.com/events/second-photo-teatr.jpg" width="1024" height="768" alt="Сцена">
    <img src="https://cdn.example.com/icons/arrow.png" width="16" height="16" alt="arrow">
  </article>
</body>
</html>`

// pageWithPlaceholderOnly has only og:image and no real content images.
const pageWithPlaceholderOnly = `<!DOCTYPE html>
<html><head>
  <meta property="og:image" content="https://cdn.example.com/og-photo.jpg">
</head><body></body></html>`

// pageWithJSONLD has a JSON-LD structured data block with an image field.
const pageWithJSONLD = `<!DOCTYPE html>
<html><head>
  <meta property="og:image" content="https://cdn.example.com/placeholder/logo.jpg">
  <script type="application/ld+json">
  {"@type":"Event","name":"Opera","image":"https://cdn.example.com/events/opera-full.jpg"}
  </script>
</head><body></body></html>`

// pageWithTinyThumbnails tests that tiny thumbnails are skipped.
const pageWithTinyThumbnails = `<!DOCTYPE html>
<html><head>
  <meta property="og:image" content="https://cdn.example.com/placeholder/logo.jpg">
</head><body>
  <img src="https://cdn.example.com/events/photo-100x75.jpg" alt="tiny">
  <img src="https://cdn.example.com/events/photo-fullsize.jpg" width="1200" height="900" alt="full">
</body></html>`

// TestContentImageProvider_Name checks the provider name constant.
func TestContentImageProvider_Name(t *testing.T) {
	t.Parallel()
	p := &ContentImageProvider{}
	if got := p.Name(); got != "content" {
		t.Errorf("Name() = %q, want %q", got, "content")
	}
}

// TestContentImageProvider_NoPageURL returns nil when PageURL is empty.
func TestContentImageProvider_NoPageURL(t *testing.T) {
	t.Parallel()
	p := &ContentImageProvider{}
	results, err := p.Search(context.Background(), "query", SearchOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("got %v, want nil", results)
	}
}

// TestContentImageProvider_ContentBeforeOG verifies that a real content <img>
// outranks the og:image placeholder in the returned candidates.
func TestContentImageProvider_ContentBeforeOG(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve the page with og: placeholder + real content img.
		// Both URLs point to the same test server (same registrable domain).
		body := strings.ReplaceAll(pageWithBothImages,
			"https://cdn.example.com",
			"http://"+r.Host)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := &ContentImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "spektakl igrok teatr",
		SearchOpts{PageURL: srv.URL + "/events/igrok-teatr/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("got no results, expected at least one content image")
	}

	// The first result must NOT be the og:image placeholder.
	if strings.Contains(results[0].ImgURL, "placeholder") {
		t.Errorf("first result is the placeholder og:image: %s", results[0].ImgURL)
	}

	// Content images must appear before og:image.
	ogIdx := -1
	contentIdx := -1
	for i, r := range results {
		if r.Title == "og:image" || r.Title == "twitter:image" {
			if ogIdx == -1 {
				ogIdx = i
			}
		}
		if strings.HasPrefix(r.Title, "content:") {
			if contentIdx == -1 {
				contentIdx = i
			}
		}
	}
	if contentIdx == -1 {
		t.Fatal("no content:img result found")
	}
	if ogIdx != -1 && contentIdx >= ogIdx {
		t.Errorf("content image at index %d should come before og:image at index %d", contentIdx, ogIdx)
	}
}

// TestContentImageProvider_SlugMatchRanksFirst verifies that a slug-matched image
// is ranked before non-slug content images.
func TestContentImageProvider_SlugMatchRanksFirst(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := strings.ReplaceAll(pageWithBothImages,
			"https://cdn.example.com",
			"http://"+r.Host)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	// Page path contains "igrok" which matches igrok-foto-teatr-2024.jpg.
	p := &ContentImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "",
		SearchOpts{PageURL: srv.URL + "/events/igrok-teatr/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 content results, got %d", len(results))
	}

	// The slug-matched image must be first.
	if !strings.Contains(results[0].ImgURL, "igrok") {
		t.Errorf("slug-matched image not first; first = %s", results[0].ImgURL)
	}
	if results[0].Title != "content:img:slug" {
		t.Errorf("first result title = %q, want %q", results[0].Title, "content:img:slug")
	}
}

// TestContentImageProvider_TinyThumbnailsSkipped verifies that images smaller
// than minContentDimension are excluded.
func TestContentImageProvider_TinyThumbnailsSkipped(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := strings.ReplaceAll(pageWithTinyThumbnails,
			"https://cdn.example.com",
			"http://"+r.Host)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := &ContentImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "",
		SearchOpts{PageURL: srv.URL + "/page/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range results {
		if strings.Contains(r.ImgURL, "100x75") {
			t.Errorf("tiny thumbnail (100x75) should have been skipped: %s", r.ImgURL)
		}
	}

	// The fullsize image should be present.
	var hasFullsize bool
	for _, r := range results {
		if strings.Contains(r.ImgURL, "fullsize") {
			hasFullsize = true
		}
	}
	if !hasFullsize {
		t.Error("fullsize image missing from results")
	}
}

// TestContentImageProvider_JSONLDImage verifies that JSON-LD image fields are
// extracted and ranked before og:image.
func TestContentImageProvider_JSONLDImage(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := strings.ReplaceAll(pageWithJSONLD,
			"https://cdn.example.com",
			"http://"+r.Host)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := &ContentImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "",
		SearchOpts{PageURL: srv.URL + "/events/opera/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var jsonldIdx, ogIdx int = -1, -1
	for i, r := range results {
		if r.Title == "jsonld:image" && jsonldIdx == -1 {
			jsonldIdx = i
		}
		if (r.Title == "og:image" || r.Title == "twitter:image") && ogIdx == -1 {
			ogIdx = i
		}
	}
	if jsonldIdx == -1 {
		t.Fatal("no jsonld:image result found")
	}
	if ogIdx != -1 && jsonldIdx >= ogIdx {
		t.Errorf("jsonld:image at index %d should come before og:image at index %d", jsonldIdx, ogIdx)
	}
}

// TestContentImageProvider_OGOnlyFallback verifies that when there are no content
// images, og:image is still returned (backward-compatible fallback).
func TestContentImageProvider_OGOnlyFallback(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := strings.ReplaceAll(pageWithPlaceholderOnly,
			"https://cdn.example.com",
			"http://"+r.Host)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := &ContentImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "",
		SearchOpts{PageURL: srv.URL + "/page/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected og:image fallback, got no results")
	}
	if results[0].Title != "og:image" {
		t.Errorf("Title = %q, want %q", results[0].Title, "og:image")
	}
}

// TestContentImageProvider_IconsSkipped verifies that icon/avatar/logo filenames
// are filtered out even if they are on the same domain.
func TestContentImageProvider_IconsSkipped(t *testing.T) {
	t.Parallel()

	const htmlWithIcons = `<!DOCTYPE html><html><head></head><body>
  <img src="BASEURL/ui/icon-heart.png" alt="like">
  <img src="BASEURL/ui/avatar-default.jpg" alt="user">
  <img src="BASEURL/events/real-photo.jpg" width="800" height="600" alt="real">
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := strings.ReplaceAll(htmlWithIcons, "BASEURL", "http://"+r.Host)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := &ContentImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "",
		SearchOpts{PageURL: srv.URL + "/page/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, r := range results {
		if strings.Contains(r.ImgURL, "icon") || strings.Contains(r.ImgURL, "avatar") {
			t.Errorf("icon/avatar should have been filtered: %s", r.ImgURL)
		}
	}

	var hasReal bool
	for _, r := range results {
		if strings.Contains(r.ImgURL, "real-photo") {
			hasReal = true
		}
	}
	if !hasReal {
		t.Error("real-photo.jpg missing from results")
	}
}

// TestContentImageProvider_HTTPError returns nil on 4xx/5xx.
func TestContentImageProvider_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := &ContentImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "",
		SearchOpts{PageURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results on HTTP error, want 0", len(results))
	}
}

// TestContentImageProvider_MaxResults caps at contentMaxResults.
func TestContentImageProvider_MaxResults(t *testing.T) {
	t.Parallel()

	var imgs []string
	for i := range contentMaxResults + 5 {
		imgs = append(imgs, fmt.Sprintf(`<img src="BASEURL/events/photo%d.jpg" width="800" height="600">`, i))
	}
	body := `<!DOCTYPE html><html><head></head><body>` + strings.Join(imgs, "\n") + `</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(strings.ReplaceAll(body, "BASEURL", "http://"+r.Host)))
	}))
	defer srv.Close()

	p := &ContentImageProvider{HTTPClient: srv.Client()}
	results, err := p.Search(context.Background(), "",
		SearchOpts{PageURL: srv.URL + "/page/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) > contentMaxResults {
		t.Errorf("got %d results, want at most %d", len(results), contentMaxResults)
	}
}

// TestRegistrableDomain checks eTLD+1 extraction.
func TestRegistrableDomain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rawURL string
		want   string
	}{
		{"https://s13.stc.all.kpcdn.net/img/photo.jpg", "kpcdn.net"},
		{"https://example.com/path", "example.com"},
		{"https://www.fiesta.ru/events/123", "fiesta.ru"},
		{"https://cdn.roofmusicplace.ru/img.jpg", "roofmusicplace.ru"},
		{"", ""},
		{"not-a-url", ""},
	}
	for _, tc := range tests {
		got := registrableDomain(tc.rawURL)
		if got != tc.want {
			t.Errorf("registrableDomain(%q) = %q, want %q", tc.rawURL, got, tc.want)
		}
	}
}

// TestNormalizeImgURL verifies size-suffix stripping.
func TestNormalizeImgURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want string
	}{
		{"https://cdn.example.com/photo-640x480.jpg", "https://cdn.example.com/photo.jpg"},
		{"https://cdn.example.com/photo-1024x768.jpg", "https://cdn.example.com/photo.jpg"},
		{"https://cdn.example.com/photo.jpg", "https://cdn.example.com/photo.jpg"},
		{"https://cdn.example.com/photo-full.jpg", "https://cdn.example.com/photo-full.jpg"},
	}
	for _, tc := range tests {
		got := normalizeImgURL(tc.in)
		if got != tc.want {
			t.Errorf("normalizeImgURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSlugMatch verifies slug token matching logic.
func TestSlugMatch(t *testing.T) {
	t.Parallel()
	tokens := []string{"igrok", "teatr", "opera"}
	tests := []struct {
		url  string
		want bool
	}{
		{"https://cdn.example.com/igrok_foto-natashi-razinoj.jpg", true},
		{"https://cdn.example.com/spektakl-teatr-2024.jpg", true},
		{"https://cdn.example.com/another-photo.jpg", false},
		{"https://cdn.example.com/", false},
	}
	for _, tc := range tests {
		got := isSlugMatch(tc.url, tokens)
		if got != tc.want {
			t.Errorf("isSlugMatch(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

// TestExtractJSONLDImage checks the JSON-LD image extractor.
func TestExtractJSONLDImage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"string value", `{"image":"https://example.com/photo.jpg"}`, "https://example.com/photo.jpg"},
		{"url object", `{"image":{"url":"https://example.com/photo.jpg"}}`, "https://example.com/photo.jpg"},
		{"array first", `{"image":["https://example.com/first.jpg","https://example.com/second.jpg"]}`, "https://example.com/first.jpg"},
		{"no image key", `{"name":"Event"}`, ""},
		{"invalid json", `{invalid}`, ""},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractJSONLDImage(tc.input)
			if got != tc.want {
				t.Errorf("extractJSONLDImage = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestParseClassificationResult_Placeholder verifies that PLACEHOLDER is parsed
// and recognised as a reject class by ParseClassificationResult.
func TestParseClassificationResult_Placeholder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		resp      string
		wantClass string
	}{
		{"PLACEHOLDER 0.95", ClassPlaceholder},
		{"placeholder 0.80", ClassPlaceholder},
		{"Placeholder", ClassPlaceholder},
		{"PLACEHOLDER", ClassPlaceholder},
	}
	for _, tc := range tests {
		t.Run(tc.resp, func(t *testing.T) {
			t.Parallel()
			got := ParseClassificationResult(tc.resp)
			if got.Class != tc.wantClass {
				t.Errorf("ParseClassificationResult(%q).Class = %q, want %q",
					tc.resp, got.Class, tc.wantClass)
			}
		})
	}
}

// TestPlaceholderClassIsRejectedByPipeline verifies that ClassPlaceholder is not
// equal to ClassPhoto, so the existing validation pipeline rejects it.
func TestPlaceholderClassIsRejectedByPipeline(t *testing.T) {
	t.Parallel()

	// The pipeline accepts only ClassPhoto (or empty for graceful-degrade).
	// ClassPlaceholder must not equal ClassPhoto.
	if ClassPlaceholder == ClassPhoto {
		t.Error("ClassPlaceholder must not equal ClassPhoto — pipeline would accept placeholders")
	}
	if ClassPlaceholder == "" {
		t.Error("ClassPlaceholder must not be empty — pipeline would accept on graceful-degrade")
	}

	// Simulate pipeline decision logic.
	result := ParseClassificationResult("PLACEHOLDER 0.92")
	accepted := result.Class == ClassPhoto || result.Class == ""
	if accepted {
		t.Errorf("PLACEHOLDER result would be accepted by pipeline (Class=%q)", result.Class)
	}
}
