package imagefy

import (
	"testing"
)

func TestAssessLicense_DomainOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cand        ImageCandidate
		meta        *ImageMetadata
		cfg         Config
		wantLicense ImageLicense
	}{
		{
			name: "blocked domain from search",
			cand: ImageCandidate{
				ImgURL:  "https://www.shutterstock.com/image.jpg",
				Source:  "https://www.shutterstock.com/photo/123",
				License: LicenseBlocked,
			},
			meta:        nil,
			cfg:         Config{},
			wantLicense: LicenseBlocked,
		},
		{
			name: "safe domain from search",
			cand: ImageCandidate{
				ImgURL:  "https://images.unsplash.com/photo.jpg",
				Source:  "https://unsplash.com/photos/abc",
				License: LicenseSafe,
			},
			meta:        nil,
			cfg:         Config{},
			wantLicense: LicenseSafe,
		},
		{
			name: "unknown domain",
			cand: ImageCandidate{
				ImgURL:  "https://example.com/image.jpg",
				Source:  "https://example.com/page",
				License: LicenseUnknown,
			},
			meta:        nil,
			cfg:         Config{},
			wantLicense: LicenseUnknown,
		},
		{
			name: "extra blocked domain upgrades unknown to blocked",
			cand: ImageCandidate{
				ImgURL:  "https://www.mycorpstock.com/photo.jpg",
				Source:  "https://www.mycorpstock.com/gallery",
				License: LicenseUnknown,
			},
			meta:        nil,
			cfg:         Config{ExtraBlockedDomains: []string{"mycorpstock"}},
			wantLicense: LicenseBlocked,
		},
		{
			name: "extra safe domain upgrades unknown to safe",
			cand: ImageCandidate{
				ImgURL:  "https://images.myfreephotos.org/sunset.jpg",
				Source:  "https://myfreephotos.org/photo/123",
				License: LicenseUnknown,
			},
			meta:        nil,
			cfg:         Config{ExtraSafeDomains: []string{"myfreephotos"}},
			wantLicense: LicenseSafe,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.cfg.AssessLicense(tc.cand, tc.meta)
			if got.License != tc.wantLicense {
				t.Errorf("AssessLicense().License = %v, want %v", got.License, tc.wantLicense)
			}
			if got.Signals == nil {
				t.Errorf("AssessLicense().Signals should never be nil")
			}
		})
	}
}

func TestAssessLicense_MetadataStock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cand        ImageCandidate
		meta        *ImageMetadata
		cfg         Config
		wantLicense ImageLicense
	}{
		{
			name: "shutterstock in IPTC blocks unknown",
			cand: ImageCandidate{
				ImgURL:  "https://example.com/image.jpg",
				Source:  "https://example.com/page",
				License: LicenseUnknown,
			},
			meta:        &ImageMetadata{IPTCSource: "Shutterstock Inc."},
			cfg:         Config{},
			wantLicense: LicenseBlocked,
		},
		{
			name: "metadata blocked overrides search safe",
			cand: ImageCandidate{
				ImgURL:  "https://images.unsplash.com/photo.jpg",
				Source:  "https://unsplash.com/photos/abc",
				License: LicenseSafe,
			},
			meta:        &ImageMetadata{IPTCCopyright: "Copyright Shutterstock Inc."},
			cfg:         Config{},
			wantLicense: LicenseBlocked,
		},
		{
			name: "nil metadata does not block",
			cand: ImageCandidate{
				ImgURL:  "https://example.com/image.jpg",
				Source:  "https://example.com/page",
				License: LicenseUnknown,
			},
			meta:        nil,
			cfg:         Config{},
			wantLicense: LicenseUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.cfg.AssessLicense(tc.cand, tc.meta)
			if got.License != tc.wantLicense {
				t.Errorf("AssessLicense().License = %v, want %v", got.License, tc.wantLicense)
			}
		})
	}
}

func TestAssessLicense_MetadataCC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cand        ImageCandidate
		meta        *ImageMetadata
		cfg         Config
		wantLicense ImageLicense
	}{
		{
			name: "CC license in XMP promotes to safe",
			cand: ImageCandidate{
				ImgURL:  "https://example.com/image.jpg",
				Source:  "https://example.com/page",
				License: LicenseUnknown,
			},
			meta:        &ImageMetadata{XMPLicense: "https://creativecommons.org/licenses/by/4.0/"},
			cfg:         Config{},
			wantLicense: LicenseSafe,
		},
		{
			name: "CC0 in web statement promotes to safe",
			cand: ImageCandidate{
				ImgURL:  "https://example.com/image.jpg",
				Source:  "https://example.com/page",
				License: LicenseUnknown,
			},
			meta:        &ImageMetadata{XMPWebStatement: "https://creativecommons.org/publicdomain/zero/1.0/"},
			cfg:         Config{},
			wantLicense: LicenseSafe,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.cfg.AssessLicense(tc.cand, tc.meta)
			if got.License != tc.wantLicense {
				t.Errorf("AssessLicense().License = %v, want %v", got.License, tc.wantLicense)
			}
		})
	}
}

func TestAssessLicense_BlockedTakesPrecedence(t *testing.T) {
	t.Parallel()

	// Both stock metadata AND CC metadata present → Blocked wins.
	cand := ImageCandidate{
		ImgURL:  "https://example.com/image.jpg",
		Source:  "https://example.com/page",
		License: LicenseUnknown,
	}
	meta := &ImageMetadata{
		IPTCCopyright: "Copyright Shutterstock Inc.",
		XMPLicense:    "https://creativecommons.org/licenses/by/4.0/",
	}
	cfg := Config{}

	got := cfg.AssessLicense(cand, meta)
	if got.License != LicenseBlocked {
		t.Errorf("AssessLicense().License = %v, want %v (blocked should take precedence)", got.License, LicenseBlocked)
	}
}

func TestAssessLicense_SignalsPopulated(t *testing.T) {
	t.Parallel()

	// When both domain and metadata match, signals should have at least 2 entries.
	cand := ImageCandidate{
		ImgURL:  "https://www.shutterstock.com/image.jpg",
		Source:  "https://www.shutterstock.com/photo/123",
		License: LicenseBlocked,
	}
	meta := &ImageMetadata{
		IPTCSource: "Shutterstock Inc.",
	}
	cfg := Config{}

	got := cfg.AssessLicense(cand, meta)
	if len(got.Signals) < 2 {
		t.Errorf("AssessLicense().Signals has %d entries, want at least 2", len(got.Signals))
	}

	// Verify signal sources are populated.
	for i, sig := range got.Signals {
		if sig.Source == "" {
			t.Errorf("Signal[%d].Source is empty", i)
		}
		if sig.Detail == "" {
			t.Errorf("Signal[%d].Detail is empty", i)
		}
	}
}
