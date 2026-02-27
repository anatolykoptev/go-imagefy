package imagefy

import (
	"strings"
	"testing"
)

func TestExtractOGImageURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		html     string
		want     string
	}{
		{
			name: "property-first order",
			html: `<html><head><meta property="og:image" content="https://example.com/photo.jpg"/></head></html>`,
			want: "https://example.com/photo.jpg",
		},
		{
			name: "content-first order",
			html: `<html><head><meta content="https://example.com/other.jpg" property="og:image"/></head></html>`,
			want: "https://example.com/other.jpg",
		},
		{
			name: "HTML entities decoded",
			html: `<html><head><meta property="og:image" content="https://example.com/photo.jpg?a=1&amp;b=2"/></head></html>`,
			want: "https://example.com/photo.jpg?a=1&b=2",
		},
		{
			name: "not found returns empty string",
			html: `<html><head><title>No OG</title></head></html>`,
			want: "",
		},
		{
			name: "empty string returns empty string",
			html: "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractOGImageURL(tc.html)
			if got != tc.want {
				t.Errorf("ExtractOGImageURL(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsLogoOrBanner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lower string
		want  bool
	}{
		{
			name:  "logo in URL",
			lower: "https://example.com/images/logo.png",
			want:  true,
		},
		{
			name:  "favicon in URL",
			lower: "https://example.com/favicon.ico",
			want:  true,
		},
		{
			name:  "regular photo URL",
			lower: "https://example.com/photos/city-view.jpg",
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsLogoOrBanner(tc.lower)
			if got != tc.want {
				t.Errorf("IsLogoOrBanner(%q) = %v, want %v", tc.lower, got, tc.want)
			}
		})
	}
}

func TestEncodeDataURL(t *testing.T) {
	t.Parallel()

	data := []byte("hello world")
	mimeType := "image/jpeg"
	got := EncodeDataURL(data, mimeType)

	if !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Errorf("EncodeDataURL() = %q, want prefix %q", got, "data:image/jpeg;base64,")
	}
	// Verify round-trip via EncodeBase64.
	want := "data:image/jpeg;base64," + EncodeBase64(data)
	if got != want {
		t.Errorf("EncodeDataURL() = %q, want %q", got, want)
	}
}
