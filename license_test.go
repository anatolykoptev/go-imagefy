package imagefy

import (
	"testing"
)

func TestCheckLicense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		imageURL  string
		sourceURL string
		want      ImageLicense
	}{
		{
			name:      "blocked domain in imageURL",
			imageURL:  "https://www.shutterstock.com/image.jpg",
			sourceURL: "",
			want:      LicenseBlocked,
		},
		{
			name:      "blocked domain in sourceURL",
			imageURL:  "",
			sourceURL: "https://www.gettyimages.com/photo/123",
			want:      LicenseBlocked,
		},
		{
			name:      "safe domain in imageURL",
			imageURL:  "https://images.unsplash.com/photo.jpg",
			sourceURL: "",
			want:      LicenseSafe,
		},
		{
			name:      "safe domain in sourceURL",
			imageURL:  "",
			sourceURL: "https://pixabay.com/photos/123",
			want:      LicenseSafe,
		},
		{
			name:      "unknown domain",
			imageURL:  "https://example.com/image.jpg",
			sourceURL: "",
			want:      LicenseUnknown,
		},
		{
			name:      "both empty",
			imageURL:  "",
			sourceURL: "",
			want:      LicenseUnknown,
		},
		{
			name:      "malformed imageURL",
			imageURL:  "://bad",
			sourceURL: "",
			want:      LicenseUnknown,
		},
		{
			name:      "blocked takes precedence over safe — imageURL safe sourceURL blocked",
			imageURL:  "https://images.unsplash.com/photo.jpg",
			sourceURL: "https://www.shutterstock.com/image.jpg",
			want:      LicenseBlocked,
		},
		{
			name:      "istockphoto blocked",
			imageURL:  "https://media.istockphoto.com/id/123/photo.jpg",
			sourceURL: "",
			want:      LicenseBlocked,
		},
		{
			name:      "wikimedia safe",
			imageURL:  "https://upload.wikimedia.org/wikipedia/commons/a/ab/photo.jpg",
			sourceURL: "",
			want:      LicenseSafe,
		},
		{
			name:      "pexels safe",
			imageURL:  "https://images.pexels.com/photos/123/photo.jpeg",
			sourceURL: "",
			want:      LicenseSafe,
		},
		{
			name:      "alamy blocked via sourceURL with clean imageURL",
			imageURL:  "https://cdn.example.com/cached/photo.jpg",
			sourceURL: "https://www.alamy.com/stock-photo/city.html",
			want:      LicenseBlocked,
		},
		// New blocked domains.
		{
			name:     "thinkstockphotos blocked",
			imageURL: "https://www.thinkstockphotos.com/image/123.jpg",
			want:     LicenseBlocked,
		},
		{
			name:     "canstockphoto blocked",
			imageURL: "https://www.canstockphoto.com/photo.jpg",
			want:     LicenseBlocked,
		},
		{
			name:     "loriimages blocked (Russian stock)",
			imageURL: "https://loriimages.com/photo/12345",
			want:     LicenseBlocked,
		},
		{
			name:     "fotobank blocked (Russian stock)",
			imageURL: "https://www.fotobank.ru/image.jpg",
			want:     LicenseBlocked,
		},
		{
			name:     "vectorstock blocked",
			imageURL: "https://www.vectorstock.com/royalty-free-vector/123",
			want:     LicenseBlocked,
		},
		{
			name:     "photodune blocked",
			imageURL: "https://photodune.net/item/photo/123",
			want:     LicenseBlocked,
		},
		// URL path pattern checks.
		{
			name:     "stock-photo path pattern blocked",
			imageURL: "https://cdn.example.com/stock-photo/12345.jpg",
			want:     LicenseBlocked,
		},
		{
			name:     "stock-image path pattern blocked",
			imageURL: "https://images.example.com/stock-image-premium.jpg",
			want:     LicenseBlocked,
		},
		{
			name:     "editorial-image path pattern blocked",
			imageURL: "https://cdn.example.com/editorial-image/event.jpg",
			want:     LicenseBlocked,
		},
		{
			name:     "premium-photo path pattern blocked",
			imageURL: "https://media.example.com/premium-photo/city.jpg",
			want:     LicenseBlocked,
		},
		{
			name:      "stock-photo path in sourceURL blocked",
			imageURL:  "https://cdn.example.com/cached.jpg",
			sourceURL: "https://example.com/gallery/stock-photo/123",
			want:      LicenseBlocked,
		},
		{
			name:     "normal path not blocked",
			imageURL: "https://example.com/photos/city.jpg",
			want:     LicenseUnknown,
		},
		{
			name:     "freepik blocked",
			imageURL: "https://img.freepik.com/free-photo/city.jpg",
			want:     LicenseBlocked,
		},
		{
			name:     "canva blocked",
			imageURL: "https://www.canva.com/photos/MADGv/image.jpg",
			want:     LicenseBlocked,
		},
		{
			name:     "clipartof blocked",
			imageURL: "https://www.clipartof.com/illustration/123",
			want:     LicenseBlocked,
		},
		// False positive prevention: "canva." must NOT match "canvas.*".
		{
			name:     "canvas.io NOT blocked (canva false positive prevention)",
			imageURL: "https://www.canvas.io/image.jpg",
			want:     LicenseUnknown,
		},
		{
			name:     "mycanvas.app NOT blocked",
			imageURL: "https://mycanvas.app/photo.jpg",
			want:     LicenseUnknown,
		},
		// But canva subdomains ARE blocked.
		{
			name:     "canva subdomain blocked",
			imageURL: "https://img.canva.com/photo.jpg",
			want:     LicenseBlocked,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CheckLicense(tc.imageURL, tc.sourceURL)
			if got != tc.want {
				t.Errorf("CheckLicense(%q, %q) = %v (%d), want %v (%d)",
					tc.imageURL, tc.sourceURL, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestImageLicenseString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		license ImageLicense
		want    string
	}{
		{LicenseSafe, "safe"},
		{LicenseUnknown, "unknown"},
		{LicenseBlocked, "blocked"},
		// Unrecognised value falls through to default "unknown".
		{ImageLicense(99), "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.license.String(); got != tc.want {
				t.Errorf("ImageLicense(%d).String() = %q, want %q", tc.license, got, tc.want)
			}
		})
	}
}
