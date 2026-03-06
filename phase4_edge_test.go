package imagefy

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 1. ExtractImageMetadata with real JPEG
// ---------------------------------------------------------------------------

// TestPhase4Edge_ExtractMetadata_MinimalJPEG creates a minimal JPEG from Go's
// image/jpeg encoder and checks whether bep/imagemeta can parse it at all.
// A plain Go-encoded JPEG contains no EXIF/IPTC/XMP, so ExtractImageMetadata
// should return nil (no metadata found) — not panic or return a non-nil empty struct.
func TestPhase4Edge_ExtractMetadata_MinimalJPEG(t *testing.T) {
	t.Parallel()

	// Create a tiny 2x2 red JPEG.
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("failed to encode test JPEG: %v", err)
	}

	meta := ExtractImageMetadata(buf.Bytes())
	if meta != nil {
		t.Errorf("ExtractImageMetadata(minimal JPEG without EXIF) = %+v, want nil", meta)
	}
}

// TestPhase4Edge_ExtractMetadata_TruncatedJPEG feeds a truncated JPEG
// (valid SOI marker but truncated body) to check graceful degradation.
func TestPhase4Edge_ExtractMetadata_TruncatedJPEG(t *testing.T) {
	t.Parallel()

	// JPEG SOI marker (0xFF 0xD8) + partial APP0 marker.
	truncated := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}

	meta := ExtractImageMetadata(truncated)
	// Should not panic, should return nil.
	if meta != nil {
		t.Errorf("ExtractImageMetadata(truncated JPEG) = %+v, want nil", meta)
	}
}

// ---------------------------------------------------------------------------
// 2. IsCCByMetadata edge cases
// ---------------------------------------------------------------------------

// TestPhase4Edge_IsCCByMetadata_CreativecommonsNonLicenseText tests that a
// free-text field mentioning "creativecommons" without a valid license path
// does NOT trigger a false positive.
// BUG HYPOTHESIS: The ccLicensePathSegments include "creativecommons.org/licenses/"
// — the code checks if the field *contains* this substring. A text like
// "See creativecommons.org for info" does NOT contain "/licenses/" so should be false.
// But "Visit creativecommons.org/licenses/ for details" in usage terms WOULD match.
func TestPhase4Edge_IsCCByMetadata_CreativecommonsNonLicenseText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta *ImageMetadata
		want bool
	}{
		{
			name: "mention of creativecommons.org without license path",
			meta: &ImageMetadata{
				XMPUsageTerms: "See creativecommons.org for info",
			},
			want: false,
		},
		{
			name: "mention of creativecommons homepage URL",
			meta: &ImageMetadata{
				XMPUsageTerms: "Visit https://creativecommons.org/ for details",
			},
			want: false,
		},
		{
			name: "text containing creativecommons.org/licenses/ in prose triggers match",
			meta: &ImageMetadata{
				XMPUsageTerms: "See creativecommons.org/licenses/ for options",
			},
			// This WILL match because the code does substring check for
			// "creativecommons.org/licenses/" inside the field.
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsCCByMetadata(tc.meta)
			if got != tc.want {
				t.Errorf("IsCCByMetadata(%+v) = %v, want %v", tc.meta, got, tc.want)
			}
		})
	}
}

// TestPhase4Edge_IsCCByMetadata_WhitespaceOnly verifies that fields with only
// whitespace are not treated as CC licenses.
func TestPhase4Edge_IsCCByMetadata_WhitespaceOnly(t *testing.T) {
	t.Parallel()

	meta := &ImageMetadata{
		XMPLicense:      "   ",
		XMPWebStatement: "\t\n",
		XMPUsageTerms:   "  \r\n  ",
		DCRights:        " ",
	}
	got := IsCCByMetadata(meta)
	if got {
		t.Errorf("IsCCByMetadata(whitespace-only fields) = true, want false")
	}
}

// TestPhase4Edge_IsCCByMetadata_VeryLongString verifies that very long strings
// do not cause performance issues or panics.
func TestPhase4Edge_IsCCByMetadata_VeryLongString(t *testing.T) {
	t.Parallel()

	longStr := strings.Repeat("a", 1_000_000) // 1 MB of 'a'
	meta := &ImageMetadata{
		XMPUsageTerms: longStr,
	}
	got := IsCCByMetadata(meta)
	if got {
		t.Errorf("IsCCByMetadata(1MB string) = true, want false")
	}
}

// ---------------------------------------------------------------------------
// 3. IsStockByMetadata edge cases
// ---------------------------------------------------------------------------

// TestPhase4Edge_IsStockByMetadata_FalsePositives tests whether "istock" keyword
// causes false positives with words like "livestock" or "bristockton".
// BUG: "istock" is a substring of "livestock" → false positive!
func TestPhase4Edge_IsStockByMetadata_FalsePositives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta *ImageMetadata
		want bool
	}{
		{
			name: "livestock should not match istock",
			meta: &ImageMetadata{
				EXIFCopyright: "Copyright 2024 Livestock Photography",
			},
			// BUG: "istock" is a substring of "livestock" so this WILL match.
			// Expected correct behavior: false (not stock).
			// Actual behavior: true (false positive).
			want: false,
		},
		{
			name: "bristockton should not match istock",
			meta: &ImageMetadata{
				IPTCByline: "John from Bristockton",
			},
			// BUG: "istock" is found in "bristockton".
			want: false,
		},
		{
			name: "superstock in context of superstore",
			meta: &ImageMetadata{
				IPTCCopyright: "Copyright 2024 Superstore Inc.",
			},
			// "superstock" is NOT in "superstore" — no false positive here.
			want: false,
		},
		{
			name: "canstockphoto as word boundary",
			meta: &ImageMetadata{
				EXIFCopyright: "I canstockphoto by mistake",
			},
			// This matches because "canstockphoto" is literally in the string.
			want: true,
		},
		{
			name: "freepik in unexpected context",
			meta: &ImageMetadata{
				IPTCCredit: "Photo for a freepik review article",
			},
			// This will match because "freepik" is in the string.
			want: true,
		},
		{
			name: "vectorstock in casual mention",
			meta: &ImageMetadata{
				DCCreator: "I uploaded to vectorstock once",
			},
			// Matches because "vectorstock" is in the string.
			want: true,
		},
		{
			name: "alamy in balaMystar username",
			meta: &ImageMetadata{
				EXIFArtist: "balaMystar",
			},
			// BUG: "alamy" appears in "balamystar" when lowercased → false positive.
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsStockByMetadata(tc.meta)
			if got != tc.want {
				t.Errorf("IsStockByMetadata(%q) = %v, want %v",
					tc.name, got, tc.want)
			}
		})
	}
}

// TestPhase4Edge_IsStockByMetadata_WhitespaceFields verifies that whitespace-
// only fields are not treated as containing stock keywords (they should be
// empty-checked).
// NOTE: The code checks `if f == ""` then continue — whitespace-only fields
// are NOT empty strings, so they go through the keyword matching.
func TestPhase4Edge_IsStockByMetadata_WhitespaceFields(t *testing.T) {
	t.Parallel()

	meta := &ImageMetadata{
		EXIFCopyright: "   ",
		IPTCCopyright: "\t",
	}
	got := IsStockByMetadata(meta)
	if got {
		t.Errorf("IsStockByMetadata(whitespace-only fields) = true, want false")
	}
}

// ---------------------------------------------------------------------------
// 4. AssessLicense edge cases
// ---------------------------------------------------------------------------

// TestPhase4Edge_AssessLicense_SameDomainInBothLists tests what happens when
// the same domain is in both ExtraBlockedDomains and ExtraSafeDomains.
// Expected: Blocked should take precedence because CheckLicenseWith checks
// blocked BEFORE safe.
func TestPhase4Edge_AssessLicense_SameDomainInBothLists(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExtraBlockedDomains: []string{"ambiguous.com"},
		ExtraSafeDomains:    []string{"ambiguous.com"},
	}
	cand := ImageCandidate{
		ImgURL:  "https://www.ambiguous.com/photo.jpg",
		Source:  "https://www.ambiguous.com/gallery",
		License: LicenseUnknown,
	}

	got := cfg.AssessLicense(cand, nil)
	if got.License != LicenseBlocked {
		t.Errorf("AssessLicense(same domain in both lists).License = %v, want %v (blocked should win)",
			got.License, LicenseBlocked)
	}
}

// TestPhase4Edge_AssessLicense_EmptyImgURL tests that an empty ImgURL does not
// cause panics or unexpected classification.
func TestPhase4Edge_AssessLicense_EmptyImgURL(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExtraBlockedDomains: []string{"stock.com"},
	}
	cand := ImageCandidate{
		ImgURL:  "",
		Source:  "",
		License: LicenseUnknown,
	}

	got := cfg.AssessLicense(cand, nil)
	if got.License != LicenseUnknown {
		t.Errorf("AssessLicense(empty ImgURL).License = %v, want %v",
			got.License, LicenseUnknown)
	}
}

// TestPhase4Edge_AssessLicense_StockAndCCSimultaneous tests that when metadata
// has both stock and CC signals, Blocked wins.
func TestPhase4Edge_AssessLicense_StockAndCCSimultaneous(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	cand := ImageCandidate{
		ImgURL:  "https://example.com/photo.jpg",
		Source:  "https://example.com/page",
		License: LicenseUnknown,
	}
	meta := &ImageMetadata{
		IPTCCopyright: "Copyright Shutterstock Inc.",
		XMPLicense:    "https://creativecommons.org/licenses/by/4.0/",
	}

	got := cfg.AssessLicense(cand, meta)
	if got.License != LicenseBlocked {
		t.Errorf("AssessLicense(stock + CC in metadata).License = %v, want %v",
			got.License, LicenseBlocked)
	}
	// Should have both signals.
	if len(got.Signals) < 2 {
		t.Errorf("Expected at least 2 signals, got %d: %+v", len(got.Signals), got.Signals)
	}
}

// TestPhase4Edge_AssessLicense_ExtendedDomainNoSignalWhenSame tests that the
// extended domain check does NOT add a signal when it matches the same
// classification as the search-time check.
func TestPhase4Edge_AssessLicense_ExtendedDomainNoSignalWhenSame(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExtraBlockedDomains: []string{"shutterstock"},
	}
	cand := ImageCandidate{
		ImgURL:  "https://www.shutterstock.com/image.jpg",
		Source:  "https://www.shutterstock.com/photo/123",
		License: LicenseBlocked, // already blocked at search time
	}

	got := cfg.AssessLicense(cand, nil)
	// The extended domain check matches LicenseBlocked, same as cand.License,
	// so it should NOT add an extra_domain signal.
	for _, sig := range got.Signals {
		if sig.Source == "extra_domain" {
			t.Errorf("Extended domain signal should not be added when classification is the same, got: %+v", sig)
		}
	}
}

// ---------------------------------------------------------------------------
// 5. Pipeline (validateOne) edge cases
// ---------------------------------------------------------------------------

// TestPhase4Edge_ExtractMetadata_Nil verifies ExtractImageMetadata(nil) returns nil.
func TestPhase4Edge_ExtractMetadata_Nil(t *testing.T) {
	t.Parallel()

	meta := ExtractImageMetadata(nil)
	if meta != nil {
		t.Errorf("ExtractImageMetadata(nil) = %+v, want nil", meta)
	}
}

// TestPhase4Edge_DownloadForValidation_DecodeFails tests that when image.Decode
// fails, the raw bytes are still returned for metadata extraction.
func TestPhase4Edge_DownloadForValidation_DecodeFails(t *testing.T) {
	t.Parallel()

	// Create raw bytes that are NOT a valid image format but look like image data.
	// downloadForValidation should still return raw bytes even when image.Decode fails.
	// We test this indirectly by verifying the logic in dedup.go:
	// line 62: return result.Data, nil  (when decode fails)
	//
	// We create a minimal valid JPEG, corrupt it slightly, and check that
	// bytes are still returned. But since we can't call downloadForValidation
	// without an HTTP server, we test the underlying logic:
	// image.Decode fails → (data, nil) is returned.

	// Simulate what downloadForValidation does: Decode, and on error return data, nil.
	corruptData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00}
	_, _, err := image.Decode(bytes.NewReader(corruptData))
	if err == nil {
		t.Skip("corrupt data decoded successfully (unexpected), skipping")
	}

	// Verify that metadata extraction on corrupt image data doesn't crash.
	meta := ExtractImageMetadata(corruptData)
	// Should return nil (no valid metadata), but must not panic.
	_ = meta
}

// ---------------------------------------------------------------------------
// 6. CheckLicenseWith edge cases
// ---------------------------------------------------------------------------

// TestPhase4Edge_CheckLicenseWith_EmptyStringsInExtraSlices tests that empty
// strings in the extra blocked/safe slices don't cause everything to match.
// BUG HYPOTHESIS: strings.Contains(host, "") returns true for any host!
func TestPhase4Edge_CheckLicenseWith_EmptyStringsInExtraBlocked(t *testing.T) {
	t.Parallel()

	// An empty string in extraBlocked should not block every URL.
	got := CheckLicenseWith(
		"https://www.example.com/photo.jpg",
		"",
		[]string{""},  // empty string in blocked list
		nil,
	)
	if got != LicenseUnknown {
		t.Errorf("CheckLicenseWith with empty string in extraBlocked = %v, want %v (empty string should not match everything)",
			got, LicenseUnknown)
	}
}

// TestPhase4Edge_CheckLicenseWith_EmptyStringsInExtraSafe tests that empty
// strings in the extra safe slices don't cause everything to match.
func TestPhase4Edge_CheckLicenseWith_EmptyStringsInExtraSafe(t *testing.T) {
	t.Parallel()

	got := CheckLicenseWith(
		"https://www.example.com/photo.jpg",
		"",
		nil,
		[]string{""}, // empty string in safe list
	)
	if got != LicenseUnknown {
		t.Errorf("CheckLicenseWith with empty string in extraSafe = %v, want %v (empty string should not match everything)",
			got, LicenseUnknown)
	}
}

// TestPhase4Edge_CheckLicenseWith_SubstringOfBuiltinDomain tests what happens
// when an extra domain is a substring of a built-in domain.
func TestPhase4Edge_CheckLicenseWith_SubstringOfBuiltinDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		imageURL     string
		extraBlocked []string
		extraSafe    []string
		want         ImageLicense
	}{
		{
			name:         "extra 'stock' matches within shutterstock.com (already blocked)",
			imageURL:     "https://www.shutterstock.com/image.jpg",
			extraBlocked: []string{"stock"},
			want:         LicenseBlocked,
		},
		{
			name:         "extra 'stock' also matches unrelated hostname containing stock",
			imageURL:     "https://www.woodstock-photos.com/image.jpg",
			extraBlocked: []string{"stock"},
			want:         LicenseBlocked,
		},
		{
			name:         "extra 'splash' matches inside unsplash.com",
			imageURL:     "https://images.unsplash.com/photo.jpg",
			extraSafe:    []string{"splash"},
			want:         LicenseSafe, // already safe via built-in
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CheckLicenseWith(tc.imageURL, "", tc.extraBlocked, tc.extraSafe)
			if got != tc.want {
				t.Errorf("CheckLicenseWith(%q, ...) = %v, want %v",
					tc.imageURL, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 7. ExtractCCLicense edge cases
// ---------------------------------------------------------------------------

// TestPhase4Edge_ExtractCCLicense_HTMLEntities tests that HTML entities like
// &amp; in href attributes are properly decoded.
func TestPhase4Edge_ExtractCCLicense_HTMLEntities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "href with &amp; entity in CC URL",
			html: `<a rel="license" href="https://creativecommons.org/licenses/by/4.0/?ref=cczero&amp;lang=en">CC BY 4.0</a>`,
			want: "https://creativecommons.org/licenses/by/4.0/?ref=cczero&lang=en",
		},
		{
			name: "bare href with &amp; entity",
			html: `<a href="https://creativecommons.org/licenses/by-sa/4.0/?foo=1&amp;bar=2">License</a>`,
			want: "https://creativecommons.org/licenses/by-sa/4.0/?foo=1&bar=2",
		},
		{
			name: "meta content with &#x26; entity",
			html: `<meta name="license" content="https://creativecommons.org/licenses/by/4.0/?a=1&#x26;b=2">`,
			want: "https://creativecommons.org/licenses/by/4.0/?a=1&b=2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractCCLicense(tc.html)
			if got != tc.want {
				t.Errorf("ExtractCCLicense(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPhase4Edge_ExtractCCLicense_MultipleCCLinks verifies that when multiple
// CC license links exist, the first one is returned.
func TestPhase4Edge_ExtractCCLicense_MultipleCCLinks(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<a rel="license" href="https://creativecommons.org/licenses/by/4.0/">First</a>
		<a rel="license" href="https://creativecommons.org/licenses/by-sa/4.0/">Second</a>
	</body></html>`

	got := ExtractCCLicense(html)
	want := "https://creativecommons.org/licenses/by/4.0/"
	if got != want {
		t.Errorf("ExtractCCLicense(multiple CC links) = %q, want %q (first link)", got, want)
	}
}

// TestPhase4Edge_ExtractCCLicense_NestedTagsWithCC tests that CC URLs inside
// nested HTML structures are still found.
func TestPhase4Edge_ExtractCCLicense_NestedTagsWithCC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "CC URL inside nested div/span",
			html: `<div class="footer"><span class="license"><a href="https://creativecommons.org/licenses/by/4.0/">CC BY 4.0</a></span></div>`,
			want: "https://creativecommons.org/licenses/by/4.0/",
		},
		{
			name: "CC URL inside deeply nested structure",
			html: `<html><body><div><div><div><a rel="license" href="https://creativecommons.org/licenses/by-nc/4.0/">License</a></div></div></div></body></html>`,
			want: "https://creativecommons.org/licenses/by-nc/4.0/",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractCCLicense(tc.html)
			if got != tc.want {
				t.Errorf("ExtractCCLicense(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestPhase4Edge_ExtractCCLicense_RelLicenseNonCC tests that rel="license"
// links pointing to non-CC URLs are ignored, even if followed by actual CC links.
func TestPhase4Edge_ExtractCCLicense_RelLicenseNonCC_ThenBarCC(t *testing.T) {
	t.Parallel()

	html := `<html><body>
		<a rel="license" href="https://example.com/my-license">Custom License</a>
		<a href="https://creativecommons.org/licenses/by/4.0/">CC BY 4.0</a>
	</body></html>`

	got := ExtractCCLicense(html)
	// The rel="license" link is non-CC so it's skipped.
	// The bare CC href should be found.
	want := "https://creativecommons.org/licenses/by/4.0/"
	if got != want {
		t.Errorf("ExtractCCLicense(non-CC rel then bare CC) = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Additional edge cases discovered during analysis
// ---------------------------------------------------------------------------

// TestPhase4Edge_CheckLicenseWith_NilExtraSlices verifies nil extra slices
// work the same as empty slices.
func TestPhase4Edge_CheckLicenseWith_NilExtraSlices(t *testing.T) {
	t.Parallel()

	got := CheckLicenseWith("https://example.com/photo.jpg", "", nil, nil)
	if got != LicenseUnknown {
		t.Errorf("CheckLicenseWith(nil, nil) = %v, want %v", got, LicenseUnknown)
	}
}

// TestPhase4Edge_IsCCByMetadata_CCHomepageInField tests that having just
// "creativecommons.org" without a license path does not match.
func TestPhase4Edge_IsCCByMetadata_CCHomepageInField(t *testing.T) {
	t.Parallel()

	meta := &ImageMetadata{
		XMPLicense: "https://creativecommons.org/",
	}
	got := IsCCByMetadata(meta)
	if got {
		t.Errorf("IsCCByMetadata(CC homepage only) = true, want false")
	}
}

// TestPhase4Edge_IsCCByMetadata_CCAboutPage tests that an about page URL
// does not trigger CC detection.
func TestPhase4Edge_IsCCByMetadata_CCAboutPage(t *testing.T) {
	t.Parallel()

	meta := &ImageMetadata{
		XMPLicense: "https://creativecommons.org/about/cclicenses/",
	}
	got := IsCCByMetadata(meta)
	if got {
		t.Errorf("IsCCByMetadata(CC about page) = true, want false")
	}
}

// TestPhase4Edge_AssessLicense_NilConfig tests that a zero-value Config
// can call AssessLicense without panic.
func TestPhase4Edge_AssessLicense_NilConfig(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	cand := ImageCandidate{}
	meta := (*ImageMetadata)(nil)

	// Should not panic.
	got := cfg.AssessLicense(cand, meta)
	if got.License != LicenseUnknown {
		t.Errorf("AssessLicense(zero config, zero cand, nil meta).License = %v, want %v",
			got.License, LicenseUnknown)
	}
}

// TestPhase4Edge_IsStockByMetadata_XMPFieldsNotChecked verifies that XMP
// fields (XMPLicense, XMPWebStatement, XMPUsageTerms) are NOT checked for
// stock keywords — only EXIF/IPTC/DC fields are.
// If someone put "shutterstock" in XMPUsageTerms it should NOT be flagged as stock.
func TestPhase4Edge_IsStockByMetadata_XMPFieldsNotChecked(t *testing.T) {
	t.Parallel()

	meta := &ImageMetadata{
		XMPUsageTerms: "Licensed from Shutterstock",
		XMPLicense:    "https://shutterstock.com/license/123",
	}
	got := IsStockByMetadata(meta)
	// XMP fields are NOT in the stock-check field list, so this should be false.
	if got {
		t.Errorf("IsStockByMetadata(stock keyword in XMP fields only) = true, want false")
	}
}

// TestPhase4Edge_AssessLicense_ExtraDomainConflictWithSearchTime tests that
// when extra domains reclassify a candidate differently from search-time,
// the extra_domain signal is added.
func TestPhase4Edge_AssessLicense_ExtraDomainConflictWithSearchTime(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ExtraSafeDomains: []string{"example.com"},
	}
	cand := ImageCandidate{
		ImgURL:  "https://www.example.com/photo.jpg",
		Source:  "https://www.example.com/page",
		License: LicenseUnknown, // search time said unknown
	}

	got := cfg.AssessLicense(cand, nil)
	// Extended domain check should classify as Safe and add a signal.
	if got.License != LicenseSafe {
		t.Errorf("AssessLicense().License = %v, want %v", got.License, LicenseSafe)
	}

	foundExtraDomain := false
	for _, sig := range got.Signals {
		if sig.Source == "extra_domain" {
			foundExtraDomain = true
			break
		}
	}
	if !foundExtraDomain {
		t.Errorf("Expected extra_domain signal but got none. Signals: %+v", got.Signals)
	}
}

// TestPhase4Edge_DownloadForValidation_NilResult tests that when Download
// returns nil, downloadForValidation returns (nil, nil) without crash.
// We test this by creating a Config with no HTTP clients and an invalid URL.
func TestPhase4Edge_DownloadForValidation_NilResult(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	data, mimeType, img := cfg.downloadForValidation(context.Background(), "http://[::1]:0/nonexistent")
	if data != nil {
		t.Errorf("downloadForValidation(invalid URL) data = %v, want nil", data)
	}
	if mimeType != "" {
		t.Errorf("downloadForValidation(invalid URL) mimeType = %q, want empty", mimeType)
	}
	if img != nil {
		t.Errorf("downloadForValidation(invalid URL) img = %v, want nil", img)
	}
}

// TestPhase4Edge_CheckLicense_CanvaDotFalsePositive verifies that "canva."
// blocks "canva.com" but not "canvas.io" (the trailing dot is intentional).
func TestPhase4Edge_CheckLicense_CanvaDotPrecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		imageURL string
		want     ImageLicense
	}{
		{
			name:     "canva.com is blocked",
			imageURL: "https://www.canva.com/photo.jpg",
			want:     LicenseBlocked,
		},
		{
			name:     "canvas.io is not blocked",
			imageURL: "https://www.canvas.io/photo.jpg",
			want:     LicenseUnknown,
		},
		{
			name:     "canvasdesign.com is not blocked",
			imageURL: "https://www.canvasdesign.com/photo.jpg",
			want:     LicenseUnknown,
		},
		{
			name:     "canva.dev is blocked (canva. matches)",
			imageURL: "https://www.canva.dev/photo.jpg",
			want:     LicenseBlocked,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CheckLicense(tc.imageURL, "")
			if got != tc.want {
				t.Errorf("CheckLicense(%q) = %v, want %v", tc.imageURL, got, tc.want)
			}
		})
	}
}
