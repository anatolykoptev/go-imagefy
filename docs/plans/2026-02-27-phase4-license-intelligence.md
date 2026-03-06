# Phase 4: Extended License Intelligence — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move beyond domain-based heuristics to metadata-based license detection — read IPTC/EXIF/XMP from downloaded images, scan HTML for Creative Commons links, combine all signals into a composite license assessment. Blocks stock photos by copyright metadata without LLM, promotes CC-licensed images to safe without LLM.

**Architecture:** Three new L0 modules (`metadata.go`, `cclicense.go`, `assessment.go`) that operate on pure data — no I/O, no HTTP. One new dependency (`bep/imagemeta` v0.17+) for IPTC/EXIF/XMP extraction from `[]byte` via `bytes.NewReader`. Pipeline integration in `search.go` wires the assessment into `validateOne` between dedup and LLM classification. Configurable domain lists via `Config` fields let consumers extend the built-in blocked/safe lists.

**Tech Stack:** Go 1.25, `github.com/bep/imagemeta` (pure Go, EXIF+IPTC+XMP, `io.ReadSeeker` input, actively maintained by Hugo author)

**Key design decisions:**
- `bep/imagemeta` over `evanoberholster/imagemeta` (no IPTC support) and `trimmer-io/go-xmp` (unmaintained 4+ years)
- CC HTML scanning is a standalone L0 helper (like `ExtractOGImageURL`), not auto-integrated into pipeline (avoids extra HTTP requests)
- `AssessLicense` returns `LicenseAssessment` with signals list for transparency — consumers can inspect why a decision was made
- PLUS vocabulary and IPTC 2025.1 AI fields deferred — low adoption and unclear library support

---

## Task 1: Configurable Domain Lists

**Goal:** Let consumers add custom blocked/safe domains without forking the library.

**Files:**
- Modify: `imagefy.go` (add Config fields)
- Modify: `license.go` (add `CheckLicenseWith` function, refactor internals)
- Modify: `license_test.go` (add tests for extended domains)

**Parallel:** Independent — can run alongside Tasks 2 and 3.

### Step 1: Write failing tests for configurable domains

Add to `license_test.go`:

```go
func TestCheckLicenseWith_ExtraBlocked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		imageURL     string
		sourceURL    string
		extraBlocked []string
		want         ImageLicense
	}{
		{
			name:         "custom blocked domain",
			imageURL:     "https://cdn.mystock.com/photo.jpg",
			extraBlocked: []string{"mystock.com"},
			want:         LicenseBlocked,
		},
		{
			name:         "custom blocked in source URL",
			imageURL:     "https://cdn.example.com/photo.jpg",
			sourceURL:    "https://mystock.com/gallery",
			extraBlocked: []string{"mystock.com"},
			want:         LicenseBlocked,
		},
		{
			name:         "built-in still works with extras",
			imageURL:     "https://shutterstock.com/photo.jpg",
			extraBlocked: []string{"mystock.com"},
			want:         LicenseBlocked,
		},
		{
			name:         "no match returns unknown",
			imageURL:     "https://example.com/photo.jpg",
			extraBlocked: []string{"mystock.com"},
			want:         LicenseUnknown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CheckLicenseWith(tc.imageURL, tc.sourceURL, tc.extraBlocked, nil)
			if got != tc.want {
				t.Errorf("CheckLicenseWith() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckLicenseWith_ExtraSafe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		imageURL  string
		sourceURL string
		extraSafe []string
		want      ImageLicense
	}{
		{
			name:      "custom safe domain",
			imageURL:  "https://myarchive.org/photo.jpg",
			extraSafe: []string{"myarchive.org"},
			want:      LicenseSafe,
		},
		{
			name:      "built-in safe still works",
			imageURL:  "https://unsplash.com/photo.jpg",
			extraSafe: []string{"myarchive.org"},
			want:      LicenseSafe,
		},
		{
			name:      "blocked takes precedence over custom safe",
			imageURL:  "https://shutterstock.com/photo.jpg",
			extraSafe: []string{"shutterstock.com"},
			want:      LicenseBlocked,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := CheckLicenseWith(tc.imageURL, tc.sourceURL, nil, tc.extraSafe)
			if got != tc.want {
				t.Errorf("CheckLicenseWith() = %v, want %v", got, tc.want)
			}
		})
	}
}
```

### Step 2: Run tests to verify they fail

Run: `cd /home/krolik/src/go-imagefy && go test -run TestCheckLicenseWith -v`
Expected: FAIL — `CheckLicenseWith` undefined

### Step 3: Implement configurable domain lists

**In `imagefy.go`**, add fields to Config:

```go
// In Config struct, after OnClassification field:

// ExtraBlockedDomains are additional stock/copyrighted domains to block.
// These are checked alongside the built-in BlockedDomains list.
ExtraBlockedDomains []string

// ExtraSafeDomains are additional free/CC domains to treat as safe.
// These are checked alongside the built-in SafeDomains list.
ExtraSafeDomains []string
```

**In `license.go`**, add `CheckLicenseWith` and refactor internals:

```go
// CheckLicenseWith is like CheckLicense but also checks extra blocked and safe domains.
// Extra domains are appended to the built-in lists for this call only.
func CheckLicenseWith(imageURL, sourceURL string, extraBlocked, extraSafe []string) ImageLicense {
	for _, u := range []string{imageURL, sourceURL} {
		if isBlockedWith(u, extraBlocked) {
			return LicenseBlocked
		}
	}
	for _, u := range []string{imageURL, sourceURL} {
		if isSafeWith(u, extraSafe) {
			return LicenseSafe
		}
	}
	return LicenseUnknown
}

func isBlockedWith(rawURL string, extra []string) bool {
	if rawURL == "" {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Host)
	if host != "" {
		for _, d := range BlockedDomains {
			if strings.Contains(host, d) {
				return true
			}
		}
		for _, d := range extra {
			if strings.Contains(host, d) {
				return true
			}
		}
	}
	path := strings.ToLower(parsed.Path)
	for _, p := range BlockedURLPatterns {
		if strings.Contains(path, p) {
			return true
		}
	}
	return false
}

func isSafeWith(rawURL string, extra []string) bool {
	host := extractHost(rawURL)
	if host == "" {
		return false
	}
	for _, d := range SafeDomains {
		if strings.Contains(host, d) {
			return true
		}
	}
	for _, d := range extra {
		if strings.Contains(host, d) {
			return true
		}
	}
	return false
}
```

Then refactor existing `isBlocked` and `isSafe` to delegate:

```go
func isBlocked(rawURL string) bool {
	return isBlockedWith(rawURL, nil)
}

func isSafe(rawURL string) bool {
	return isSafeWith(rawURL, nil)
}
```

### Step 4: Run tests to verify they pass

Run: `cd /home/krolik/src/go-imagefy && go test -run TestCheckLicense -v -race`
Expected: ALL PASS (both old and new tests)

### Step 5: Run linter

Run: `cd /home/krolik/src/go-imagefy && make lint`
Expected: PASS

### Step 6: Commit

```bash
cd /home/krolik/src/go-imagefy
git add license.go license_test.go imagefy.go
git commit -m "feat: configurable blocked/safe domain lists

Add CheckLicenseWith() for extended domain checking.
Add ExtraBlockedDomains/ExtraSafeDomains to Config.
Refactor isBlocked/isSafe to delegate to *With variants.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 2: Image Metadata Extraction & Stock Detection

**Goal:** Extract IPTC/EXIF/XMP rights metadata from image bytes. Detect stock agencies and CC licenses from metadata fields.

**Files:**
- Create: `metadata.go`
- Create: `metadata_test.go`
- Modify: `go.mod` (add `bep/imagemeta` dependency)

**Parallel:** Independent — can run alongside Tasks 1 and 3.

### Step 1: Add bep/imagemeta dependency

Run: `cd /home/krolik/src/go-imagefy && go get github.com/bep/imagemeta@latest`

Verify: `grep bep/imagemeta go.mod` should show the dependency.

### Step 2: Write failing tests for ImageMetadata and stock/CC detection

Create `metadata_test.go`:

```go
package imagefy

import (
	"testing"
)

func TestIsStockByMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		meta *ImageMetadata
		want bool
	}{
		{
			name: "nil metadata",
			meta: nil,
			want: false,
		},
		{
			name: "empty metadata",
			meta: &ImageMetadata{},
			want: false,
		},
		{
			name: "shutterstock in IPTC copyright",
			meta: &ImageMetadata{IPTCCopyright: "© Shutterstock, Inc."},
			want: true,
		},
		{
			name: "getty in IPTC credit",
			meta: &ImageMetadata{IPTCCredit: "Getty Images"},
			want: true,
		},
		{
			name: "istock in EXIF copyright",
			meta: &ImageMetadata{EXIFCopyright: "iStockphoto LP"},
			want: true,
		},
		{
			name: "alamy in IPTC source",
			meta: &ImageMetadata{IPTCSource: "Alamy Stock Photo"},
			want: true,
		},
		{
			name: "depositphotos in byline",
			meta: &ImageMetadata{IPTCByline: "Depositphotos contributor"},
			want: true,
		},
		{
			name: "adobe stock in DC rights",
			meta: &ImageMetadata{DCRights: "Licensed via Adobe Stock"},
			want: true,
		},
		{
			name: "regular photographer copyright",
			meta: &ImageMetadata{IPTCCopyright: "© 2026 John Doe Photography"},
			want: false,
		},
		{
			name: "case insensitive detection",
			meta: &ImageMetadata{IPTCCopyright: "SHUTTERSTOCK"},
			want: true,
		},
		{
			name: "dreamstime in copyright",
			meta: &ImageMetadata{EXIFCopyright: "Dreamstime.com"},
			want: true,
		},
		{
			name: "123rf in credit",
			meta: &ImageMetadata{IPTCCredit: "123RF.com"},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsStockByMetadata(tc.meta)
			if got != tc.want {
				t.Errorf("IsStockByMetadata() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsCCByMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		meta *ImageMetadata
		want bool
	}{
		{
			name: "nil metadata",
			meta: nil,
			want: false,
		},
		{
			name: "empty metadata",
			meta: &ImageMetadata{},
			want: false,
		},
		{
			name: "CC BY 4.0 in XMP license",
			meta: &ImageMetadata{XMPLicense: "https://creativecommons.org/licenses/by/4.0/"},
			want: true,
		},
		{
			name: "CC0 public domain in web statement",
			meta: &ImageMetadata{XMPWebStatement: "https://creativecommons.org/publicdomain/zero/1.0/"},
			want: true,
		},
		{
			name: "CC BY-SA in usage terms",
			meta: &ImageMetadata{XMPUsageTerms: "This work is licensed under CC BY-SA 4.0 https://creativecommons.org/licenses/by-sa/4.0/"},
			want: true,
		},
		{
			name: "non-CC license URL",
			meta: &ImageMetadata{XMPLicense: "https://example.com/my-license"},
			want: false,
		},
		{
			name: "CC in DC rights",
			meta: &ImageMetadata{DCRights: "Creative Commons Attribution 4.0 International https://creativecommons.org/licenses/by/4.0/"},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsCCByMetadata(tc.meta)
			if got != tc.want {
				t.Errorf("IsCCByMetadata() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractImageMetadata_NilAndEmpty(t *testing.T) {
	t.Parallel()
	// nil data
	if got := ExtractImageMetadata(nil); got != nil {
		t.Errorf("ExtractImageMetadata(nil) = %v, want nil", got)
	}
	// empty data
	if got := ExtractImageMetadata([]byte{}); got != nil {
		t.Errorf("ExtractImageMetadata(empty) = %v, want nil", got)
	}
	// garbage data (not a valid image)
	if got := ExtractImageMetadata([]byte("not an image")); got != nil {
		t.Errorf("ExtractImageMetadata(garbage) = %v, want nil", got)
	}
}
```

### Step 3: Run tests to verify they fail

Run: `cd /home/krolik/src/go-imagefy && go test -run "TestIsStockByMetadata|TestIsCCByMetadata|TestExtractImageMetadata" -v`
Expected: FAIL — types and functions undefined

### Step 4: Implement ImageMetadata, stock detection, and CC detection

Create `metadata.go`:

```go
package imagefy

import (
	"bytes"
	"strings"

	"github.com/bep/imagemeta"
)

// ImageMetadata holds rights-related metadata extracted from an image file.
// Fields are populated from IPTC, EXIF, and XMP sources.
type ImageMetadata struct {
	// IPTC fields
	IPTCCopyright string // IPTC CopyrightNotice (2:116)
	IPTCCredit    string // IPTC Credit (2:110) — agency or source
	IPTCByline    string // IPTC Byline (2:80) — author
	IPTCSource    string // IPTC Source (2:115)

	// EXIF fields
	EXIFCopyright string // EXIF Copyright (0x8298)
	EXIFArtist    string // EXIF Artist (0x013b)

	// XMP Rights fields
	XMPWebStatement string // xmpRights:WebStatement — URL to license info
	XMPUsageTerms   string // xmpRights:UsageTerms
	XMPLicense      string // xmpRights:License — license URL
	XMPMarked       bool   // xmpRights:Marked — true if copyrighted

	// Dublin Core
	DCRights  string // dc:rights
	DCCreator string // dc:creator
}

// stockAgencyPatterns are substrings found in copyright/credit metadata of stock photos.
// Checked case-insensitively against all rights-related fields.
var stockAgencyPatterns = []string{
	"shutterstock",
	"gettyimages",
	"getty images",
	"istockphoto",
	"istock",
	"adobe stock",
	"adobestock",
	"depositphotos",
	"dreamstime",
	"123rf",
	"alamy",
	"bigstockphoto",
	"stocksy",
	"pond5",
	"masterfile",
	"superstock",
	"agefotostock",
	"age fotostock",
	"colourbox",
	"yayimages",
	"vectorstock",
	"freepik",
	"canstockphoto",
}

// IsStockByMetadata reports whether the image metadata contains stock agency fingerprints.
// Returns false for nil metadata (graceful degradation).
func IsStockByMetadata(meta *ImageMetadata) bool {
	if meta == nil {
		return false
	}
	fields := []string{
		meta.IPTCCopyright,
		meta.IPTCCredit,
		meta.IPTCByline,
		meta.IPTCSource,
		meta.EXIFCopyright,
		meta.EXIFArtist,
		meta.DCRights,
		meta.DCCreator,
	}
	for _, f := range fields {
		if f == "" {
			continue
		}
		lower := strings.ToLower(f)
		for _, pattern := range stockAgencyPatterns {
			if strings.Contains(lower, pattern) {
				return true
			}
		}
	}
	return false
}

const ccLicenseDomain = "creativecommons.org/licenses/"
const ccPublicDomain = "creativecommons.org/publicdomain/"

// IsCCByMetadata reports whether the image metadata contains a Creative Commons license.
// Returns false for nil metadata (graceful degradation).
func IsCCByMetadata(meta *ImageMetadata) bool {
	if meta == nil {
		return false
	}
	fields := []string{
		meta.XMPLicense,
		meta.XMPWebStatement,
		meta.XMPUsageTerms,
		meta.DCRights,
	}
	for _, f := range fields {
		if f == "" {
			continue
		}
		lower := strings.ToLower(f)
		if strings.Contains(lower, ccLicenseDomain) || strings.Contains(lower, ccPublicDomain) {
			return true
		}
	}
	return false
}

// iptcTagNames maps IPTC tag names (as returned by bep/imagemeta) to ImageMetadata field setters.
var iptcTagNames = map[string]func(*ImageMetadata, string){
	"CopyrightNotice": func(m *ImageMetadata, v string) { m.IPTCCopyright = v },
	"Credit":          func(m *ImageMetadata, v string) { m.IPTCCredit = v },
	"Byline":          func(m *ImageMetadata, v string) { m.IPTCByline = v },
	"Source":          func(m *ImageMetadata, v string) { m.IPTCSource = v },
}

// exifTagNames maps EXIF tag names to ImageMetadata field setters.
var exifTagNames = map[string]func(*ImageMetadata, string){
	"Copyright": func(m *ImageMetadata, v string) { m.EXIFCopyright = v },
	"Artist":    func(m *ImageMetadata, v string) { m.EXIFArtist = v },
}

// xmpTagHandlers maps XMP tag names to ImageMetadata handlers.
// XMP values may be string or []string (from altList elements).
var xmpTagHandlers = map[string]func(*ImageMetadata, any){
	"WebStatement": func(m *ImageMetadata, v any) { m.XMPWebStatement = xmpString(v) },
	"UsageTerms":   func(m *ImageMetadata, v any) { m.XMPUsageTerms = xmpString(v) },
	"License":      func(m *ImageMetadata, v any) { m.XMPLicense = xmpString(v) },
	"Marked":       func(m *ImageMetadata, v any) { m.XMPMarked = xmpBool(v) },
	"Rights":       func(m *ImageMetadata, v any) { m.DCRights = xmpString(v) },
	"Creator":      func(m *ImageMetadata, v any) { m.DCCreator = xmpString(v) },
}

// xmpString extracts a string from an XMP value, which may be string or []string.
func xmpString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []string:
		if len(val) > 0 {
			return val[0]
		}
	}
	return ""
}

// xmpBool extracts a bool from an XMP value.
func xmpBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.EqualFold(val, "true")
	}
	return false
}

// allWantedTags is the union of all tag names we need from any source.
var allWantedTags map[string]bool

func init() {
	allWantedTags = make(map[string]bool)
	for k := range iptcTagNames {
		allWantedTags[k] = true
	}
	for k := range exifTagNames {
		allWantedTags[k] = true
	}
	for k := range xmpTagHandlers {
		allWantedTags[k] = true
	}
}

// ExtractImageMetadata reads IPTC, EXIF, and XMP rights metadata from raw image data.
// Returns nil if data is empty, the format is unsupported, or no metadata is found.
// Never returns an error — graceful degradation for non-critical metadata extraction.
func ExtractImageMetadata(data []byte) *ImageMetadata {
	if len(data) == 0 {
		return nil
	}

	meta := &ImageMetadata{}
	found := false

	_, err := imagemeta.Decode(imagemeta.Options{
		R:       bytes.NewReader(data),
		Sources: imagemeta.EXIF | imagemeta.IPTC | imagemeta.XMP,
		ShouldHandleTag: func(ti imagemeta.TagInfo) bool {
			return allWantedTags[ti.Tag]
		},
		HandleTag: func(ti imagemeta.TagInfo) error {
			switch ti.Source {
			case imagemeta.IPTC:
				if setter, ok := iptcTagNames[ti.Tag]; ok {
					if s, ok := ti.Value.(string); ok && s != "" {
						setter(meta, s)
						found = true
					}
				}
			case imagemeta.EXIF:
				if setter, ok := exifTagNames[ti.Tag]; ok {
					if s, ok := ti.Value.(string); ok && s != "" {
						setter(meta, s)
						found = true
					}
				}
			case imagemeta.XMP:
				if handler, ok := xmpTagHandlers[ti.Tag]; ok {
					handler(meta, ti.Value)
					found = true
				}
			}
			return nil
		},
	})
	if err != nil {
		return nil
	}

	if !found {
		return nil
	}

	return meta
}
```

### Step 5: Run tests to verify they pass

Run: `cd /home/krolik/src/go-imagefy && go test -run "TestIsStockByMetadata|TestIsCCByMetadata|TestExtractImageMetadata" -v -race`
Expected: ALL PASS

### Step 6: Run linter

Run: `cd /home/krolik/src/go-imagefy && make lint`
Expected: PASS

### Step 7: Commit

```bash
cd /home/krolik/src/go-imagefy
git add metadata.go metadata_test.go go.mod go.sum
git commit -m "feat: image metadata extraction with stock/CC detection

Add ImageMetadata struct for IPTC/EXIF/XMP rights fields.
Add ExtractImageMetadata() using bep/imagemeta (pure Go).
Add IsStockByMetadata() to detect stock agency fingerprints.
Add IsCCByMetadata() to detect Creative Commons licenses.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 3: Creative Commons HTML Scanning

**Goal:** Detect CC licenses in HTML source pages via `rel="license"` links and CC URLs.

**Files:**
- Create: `cclicense.go`
- Create: `cclicense_test.go`

**Parallel:** Independent — can run alongside Tasks 1 and 2.

### Step 1: Write failing tests

Create `cclicense_test.go`:

```go
package imagefy

import "testing"

func TestExtractCCLicense(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		html     string
		wantURL  string
	}{
		{
			name:    "empty HTML",
			html:    "",
			wantURL: "",
		},
		{
			name:    "no license link",
			html:    `<html><head><title>Test</title></head><body>Hello</body></html>`,
			wantURL: "",
		},
		{
			name:    "rel=license with CC BY 4.0",
			html:    `<a rel="license" href="https://creativecommons.org/licenses/by/4.0/">CC BY 4.0</a>`,
			wantURL: "https://creativecommons.org/licenses/by/4.0/",
		},
		{
			name:    "rel=license with CC BY-SA",
			html:    `<link rel="license" href="https://creativecommons.org/licenses/by-sa/4.0/" />`,
			wantURL: "https://creativecommons.org/licenses/by-sa/4.0/",
		},
		{
			name:    "CC0 public domain",
			html:    `<a rel="license" href="https://creativecommons.org/publicdomain/zero/1.0/">CC0</a>`,
			wantURL: "https://creativecommons.org/publicdomain/zero/1.0/",
		},
		{
			name:    "non-CC license link ignored",
			html:    `<a rel="license" href="https://example.com/license">Custom License</a>`,
			wantURL: "",
		},
		{
			name:    "CC URL in href without rel=license",
			html:    `<a href="https://creativecommons.org/licenses/by/4.0/">License</a>`,
			wantURL: "https://creativecommons.org/licenses/by/4.0/",
		},
		{
			name:    "CC URL with http scheme",
			html:    `<a href="http://creativecommons.org/licenses/by/3.0/">CC BY 3.0</a>`,
			wantURL: "http://creativecommons.org/licenses/by/3.0/",
		},
		{
			name:    "mixed quotes and whitespace",
			html:    `<a  rel="license"  href='https://creativecommons.org/licenses/by/4.0/' >CC</a>`,
			wantURL: "https://creativecommons.org/licenses/by/4.0/",
		},
		{
			name:    "CC URL in meta tag",
			html:    `<meta name="dcterms.license" content="https://creativecommons.org/licenses/by/4.0/" />`,
			wantURL: "https://creativecommons.org/licenses/by/4.0/",
		},
		{
			name:    "real-world Wikipedia page",
			html:    `<link rel="license" href="//creativecommons.org/licenses/by-sa/4.0/">`,
			wantURL: "//creativecommons.org/licenses/by-sa/4.0/",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractCCLicense(tc.html)
			if got != tc.wantURL {
				t.Errorf("ExtractCCLicense() = %q, want %q", got, tc.wantURL)
			}
		})
	}
}

func TestIsCCLicenseURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"CC BY 4.0", "https://creativecommons.org/licenses/by/4.0/", true},
		{"CC BY-SA 3.0", "https://creativecommons.org/licenses/by-sa/3.0/", true},
		{"CC0", "https://creativecommons.org/publicdomain/zero/1.0/", true},
		{"public domain mark", "https://creativecommons.org/publicdomain/mark/1.0/", true},
		{"not CC", "https://example.com/license", false},
		{"empty", "", false},
		{"CC homepage not a license", "https://creativecommons.org/", false},
		{"http scheme", "http://creativecommons.org/licenses/by/4.0/", true},
		{"protocol-relative", "//creativecommons.org/licenses/by/4.0/", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsCCLicenseURL(tc.url); got != tc.want {
				t.Errorf("IsCCLicenseURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}
```

### Step 2: Run tests to verify they fail

Run: `cd /home/krolik/src/go-imagefy && go test -run "TestExtractCCLicense|TestIsCCLicenseURL" -v`
Expected: FAIL — functions undefined

### Step 3: Implement CC license scanning

Create `cclicense.go`:

```go
package imagefy

import (
	"regexp"
	"strings"
)

// ccLicenseRe matches rel="license" links and href attributes pointing to CC URLs.
// Captures the href value from:
//   - <a rel="license" href="...">
//   - <link rel="license" href="..." />
//   - <a href="...creativecommons.org/licenses/...">
//   - <meta name="dcterms.license" content="...creativecommons.org/...">
var ccLicenseRe = regexp.MustCompile(
	`(?i)` +
		// Pattern 1: rel="license" ... href="URL"
		`rel=["']license["'][^>]*href=["']([^"']+)["']` +
		`|` +
		// Pattern 2: href="URL" ... rel="license"
		`href=["']([^"']+)["'][^>]*rel=["']license["']` +
		`|` +
		// Pattern 3: href pointing to creativecommons.org/licenses/ or /publicdomain/
		`href=["']((?:https?:)?//creativecommons\.org/(?:licenses|publicdomain)/[^"']+)["']` +
		`|` +
		// Pattern 4: meta content with CC URL
		`content=["']((?:https?:)?//creativecommons\.org/(?:licenses|publicdomain)/[^"']+)["']`,
)

// ExtractCCLicense scans HTML for Creative Commons license references.
// Returns the first CC license URL found, or empty string if none.
// Checks both rel="license" links and bare CC URLs in href/content attributes.
func ExtractCCLicense(html string) string {
	if html == "" {
		return ""
	}

	matches := ccLicenseRe.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		// Find the first non-empty capture group.
		for _, group := range m[1:] {
			if group == "" {
				continue
			}
			if IsCCLicenseURL(group) {
				return group
			}
		}
	}
	return ""
}

// IsCCLicenseURL reports whether the URL points to a Creative Commons license.
// Matches https/http/protocol-relative URLs containing
// creativecommons.org/licenses/ or creativecommons.org/publicdomain/.
func IsCCLicenseURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	lower := strings.ToLower(rawURL)
	return strings.Contains(lower, "creativecommons.org/licenses/") ||
		strings.Contains(lower, "creativecommons.org/publicdomain/")
}
```

### Step 4: Run tests to verify they pass

Run: `cd /home/krolik/src/go-imagefy && go test -run "TestExtractCCLicense|TestIsCCLicenseURL" -v -race`
Expected: ALL PASS

### Step 5: Run linter

Run: `cd /home/krolik/src/go-imagefy && make lint`
Expected: PASS

### Step 6: Commit

```bash
cd /home/krolik/src/go-imagefy
git add cclicense.go cclicense_test.go
git commit -m "feat: Creative Commons license scanning in HTML

Add ExtractCCLicense() to detect CC licenses in HTML pages.
Add IsCCLicenseURL() helper for CC URL validation.
Supports rel=\"license\" links, bare CC hrefs, and meta content.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 4: License Assessment

**Goal:** Combine all license signals (domain, metadata, CC) into a single assessment with transparency.

**Files:**
- Create: `assessment.go`
- Create: `assessment_test.go`

**Depends on:** Tasks 1, 2, 3 (uses types from all three).

### Step 1: Write failing tests

Create `assessment_test.go`:

```go
package imagefy

import "testing"

func TestAssessLicense_DomainOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cand ImageCandidate
		cfg  *Config
		want ImageLicense
	}{
		{
			name: "blocked domain from search",
			cand: ImageCandidate{
				ImgURL:  "https://shutterstock.com/photo.jpg",
				License: LicenseBlocked,
			},
			cfg:  &Config{},
			want: LicenseBlocked,
		},
		{
			name: "safe domain from search",
			cand: ImageCandidate{
				ImgURL:  "https://unsplash.com/photo.jpg",
				License: LicenseSafe,
			},
			cfg:  &Config{},
			want: LicenseSafe,
		},
		{
			name: "unknown domain",
			cand: ImageCandidate{
				ImgURL:  "https://example.com/photo.jpg",
				License: LicenseUnknown,
			},
			cfg:  &Config{},
			want: LicenseUnknown,
		},
		{
			name: "extra blocked domain upgrades unknown to blocked",
			cand: ImageCandidate{
				ImgURL:  "https://cdn.mystock.com/photo.jpg",
				Source:   "https://mystock.com/gallery",
				License: LicenseUnknown,
			},
			cfg:  &Config{ExtraBlockedDomains: []string{"mystock.com"}},
			want: LicenseBlocked,
		},
		{
			name: "extra safe domain upgrades unknown to safe",
			cand: ImageCandidate{
				ImgURL:  "https://myarchive.org/photo.jpg",
				License: LicenseUnknown,
			},
			cfg:  &Config{ExtraSafeDomains: []string{"myarchive.org"}},
			want: LicenseSafe,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.cfg.AssessLicense(tc.cand, nil)
			if got.License != tc.want {
				t.Errorf("AssessLicense().License = %v, want %v (signals: %v)", got.License, tc.want, got.Signals)
			}
		})
	}
}

func TestAssessLicense_MetadataStock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cand ImageCandidate
		meta *ImageMetadata
		want ImageLicense
	}{
		{
			name: "shutterstock in IPTC blocks unknown",
			cand: ImageCandidate{
				ImgURL:  "https://cdn.example.com/photo.jpg",
				License: LicenseUnknown,
			},
			meta: &ImageMetadata{IPTCCopyright: "© Shutterstock, Inc."},
			want: LicenseBlocked,
		},
		{
			name: "metadata blocked overrides search safe",
			cand: ImageCandidate{
				ImgURL:  "https://cdn.example.com/photo.jpg",
				License: LicenseSafe,
			},
			meta: &ImageMetadata{IPTCCredit: "Getty Images"},
			want: LicenseBlocked,
		},
		{
			name: "nil metadata does not block",
			cand: ImageCandidate{
				ImgURL:  "https://example.com/photo.jpg",
				License: LicenseUnknown,
			},
			meta: nil,
			want: LicenseUnknown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{}
			got := cfg.AssessLicense(tc.cand, tc.meta)
			if got.License != tc.want {
				t.Errorf("AssessLicense().License = %v, want %v (signals: %v)", got.License, tc.want, got.Signals)
			}
		})
	}
}

func TestAssessLicense_MetadataCC(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cand ImageCandidate
		meta *ImageMetadata
		want ImageLicense
	}{
		{
			name: "CC license in XMP promotes to safe",
			cand: ImageCandidate{
				ImgURL:  "https://example.com/photo.jpg",
				License: LicenseUnknown,
			},
			meta: &ImageMetadata{XMPLicense: "https://creativecommons.org/licenses/by/4.0/"},
			want: LicenseSafe,
		},
		{
			name: "CC0 in web statement promotes to safe",
			cand: ImageCandidate{
				ImgURL:  "https://example.com/photo.jpg",
				License: LicenseUnknown,
			},
			meta: &ImageMetadata{XMPWebStatement: "https://creativecommons.org/publicdomain/zero/1.0/"},
			want: LicenseSafe,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{}
			got := cfg.AssessLicense(tc.cand, tc.meta)
			if got.License != tc.want {
				t.Errorf("AssessLicense().License = %v, want %v (signals: %v)", got.License, tc.want, got.Signals)
			}
		})
	}
}

func TestAssessLicense_BlockedTakesPrecedence(t *testing.T) {
	t.Parallel()
	// Stock metadata should override CC metadata.
	cand := ImageCandidate{
		ImgURL:  "https://example.com/photo.jpg",
		License: LicenseUnknown,
	}
	meta := &ImageMetadata{
		IPTCCopyright: "© Shutterstock, Inc.",
		XMPLicense:    "https://creativecommons.org/licenses/by/4.0/",
	}
	cfg := &Config{}
	got := cfg.AssessLicense(cand, meta)
	if got.License != LicenseBlocked {
		t.Errorf("AssessLicense().License = %v, want Blocked when both stock and CC signals present", got.License)
	}
}

func TestAssessLicense_SignalsPopulated(t *testing.T) {
	t.Parallel()
	cand := ImageCandidate{
		ImgURL:  "https://cdn.example.com/photo.jpg",
		Source:   "https://shutterstock.com/gallery",
		License: LicenseBlocked,
	}
	meta := &ImageMetadata{IPTCCopyright: "Shutterstock"}
	cfg := &Config{}
	got := cfg.AssessLicense(cand, meta)

	if len(got.Signals) < 2 {
		t.Errorf("expected at least 2 signals, got %d: %v", len(got.Signals), got.Signals)
	}
}
```

### Step 2: Run tests to verify they fail

Run: `cd /home/krolik/src/go-imagefy && go test -run TestAssessLicense -v`
Expected: FAIL — `AssessLicense` undefined

### Step 3: Implement LicenseAssessment

Create `assessment.go`:

```go
package imagefy

// LicenseSignal represents a single evidence point about an image's license status.
type LicenseSignal struct {
	Source  string       // signal source: "domain", "extra_domain", "metadata_stock", "metadata_cc", "url_pattern"
	Detail  string       // human-readable detail
	License ImageLicense // what this signal indicates
}

// LicenseAssessment combines multiple signals into a final license verdict.
type LicenseAssessment struct {
	License ImageLicense   // final verdict: Blocked > Safe > Unknown
	Signals []LicenseSignal // contributing evidence (never nil, may be empty)
}

// AssessLicense evaluates an image candidate's license status by combining:
//   - Domain-based heuristic (built-in + Config.ExtraBlockedDomains/ExtraSafeDomains)
//   - Image metadata (IPTC/EXIF/XMP stock agency detection and CC license detection)
//
// The meta parameter may be nil if metadata extraction failed or was skipped.
// Blocked signals always take precedence over Safe signals.
func (cfg *Config) AssessLicense(cand ImageCandidate, meta *ImageMetadata) LicenseAssessment {
	var signals []LicenseSignal

	// Signal 1: search-time domain classification (already computed by provider).
	if cand.License == LicenseBlocked {
		signals = append(signals, LicenseSignal{
			Source:  "domain",
			Detail:  "blocked by search-time domain check",
			License: LicenseBlocked,
		})
	} else if cand.License == LicenseSafe {
		signals = append(signals, LicenseSignal{
			Source:  "domain",
			Detail:  "safe by search-time domain check",
			License: LicenseSafe,
		})
	}

	// Signal 2: re-check with extra domains (may upgrade Unknown → Blocked/Safe).
	if len(cfg.ExtraBlockedDomains) > 0 || len(cfg.ExtraSafeDomains) > 0 {
		extended := CheckLicenseWith(cand.ImgURL, cand.Source, cfg.ExtraBlockedDomains, cfg.ExtraSafeDomains)
		if extended == LicenseBlocked && cand.License != LicenseBlocked {
			signals = append(signals, LicenseSignal{
				Source:  "extra_domain",
				Detail:  "blocked by extra domain list",
				License: LicenseBlocked,
			})
		} else if extended == LicenseSafe && cand.License != LicenseSafe {
			signals = append(signals, LicenseSignal{
				Source:  "extra_domain",
				Detail:  "safe by extra domain list",
				License: LicenseSafe,
			})
		}
	}

	// Signal 3: metadata-based stock agency detection.
	if IsStockByMetadata(meta) {
		signals = append(signals, LicenseSignal{
			Source:  "metadata_stock",
			Detail:  metadataStockDetail(meta),
			License: LicenseBlocked,
		})
	}

	// Signal 4: metadata-based CC license detection.
	if IsCCByMetadata(meta) {
		signals = append(signals, LicenseSignal{
			Source:  "metadata_cc",
			Detail:  metadataCCDetail(meta),
			License: LicenseSafe,
		})
	}

	// Resolve: Blocked > Safe > Unknown.
	license := LicenseUnknown
	for _, s := range signals {
		if s.License == LicenseBlocked {
			license = LicenseBlocked
			break
		}
		if s.License == LicenseSafe {
			license = LicenseSafe
		}
	}

	return LicenseAssessment{
		License: license,
		Signals: signals,
	}
}

// metadataStockDetail returns a human-readable detail for stock agency detection.
func metadataStockDetail(meta *ImageMetadata) string {
	if meta == nil {
		return ""
	}
	// Return the first non-empty rights field for context.
	for _, f := range []string{meta.IPTCCopyright, meta.IPTCCredit, meta.EXIFCopyright, meta.IPTCSource} {
		if f != "" {
			return "stock agency in metadata: " + f
		}
	}
	return "stock agency detected in metadata"
}

// metadataCCDetail returns a human-readable detail for CC license detection.
func metadataCCDetail(meta *ImageMetadata) string {
	if meta == nil {
		return ""
	}
	for _, f := range []string{meta.XMPLicense, meta.XMPWebStatement, meta.DCRights} {
		if f != "" {
			return "CC license in metadata: " + f
		}
	}
	return "CC license detected in metadata"
}
```

### Step 4: Run tests to verify they pass

Run: `cd /home/krolik/src/go-imagefy && go test -run TestAssessLicense -v -race`
Expected: ALL PASS

### Step 5: Run linter

Run: `cd /home/krolik/src/go-imagefy && make lint`
Expected: PASS

### Step 6: Commit

```bash
cd /home/krolik/src/go-imagefy
git add assessment.go assessment_test.go
git commit -m "feat: composite license assessment combining all signals

Add LicenseSignal/LicenseAssessment types for transparent decisions.
Add Config.AssessLicense() combining domain, metadata, and CC signals.
Blocked signals always take precedence over Safe.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 5: Pipeline Integration

**Goal:** Wire metadata extraction and license assessment into the search validation pipeline. Download image once, use for dedup + metadata.

**Files:**
- Modify: `dedup.go` (refactor `downloadAndDecode` → `downloadForValidation`)
- Modify: `search.go` (update `validateOne` to use assessment)
- Modify: `search_test.go` (add integration tests)
- Modify: `prefilter.go` (remove or deprecate — replaced by assessment)
- Modify: `prefilter_test.go` (update tests)

**Depends on:** Task 4 (uses `AssessLicense`).

### Step 1: Write failing integration test

Add to `search_test.go`:

```go
func TestSearchImages_MetadataStockBlocked(t *testing.T) {
	t.Parallel()

	// Create a classifier that always accepts (PHOTO).
	// The metadata-based assessment should block the image before LLM is called.
	classifier := &mockClassifier{response: "PHOTO 0.99"}

	imgData := makeJPEG(1024, 768)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search":
			results := buildSearxngJSON([]searxngResult{
				{ImgSrc: srv.URL + "/stock-photo.jpg", URL: "https://example.com/page", Title: "Test"},
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(results)
		default:
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(imgData)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{
		SearxngURL: srv.URL,
		Classifier: classifier,
		Cache:      nil,
	}
	results := cfg.SearchImages(context.Background(), "test", 5)

	// The image should still appear (no metadata in test JPEG to block it).
	// This test verifies the pipeline doesn't crash with metadata extraction.
	if len(results) == 0 {
		t.Fatal("expected results from search pipeline with metadata extraction")
	}
}

func TestSearchImages_ExtraBlockedDomain(t *testing.T) {
	t.Parallel()

	imgData := makeJPEG(1024, 768)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search":
			results := buildSearxngJSON([]searxngResult{
				{ImgSrc: srv.URL + "/photo.jpg", URL: "https://mystock.example.com/page", Title: "Test"},
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(results)
		default:
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(imgData)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &Config{
		SearxngURL:          srv.URL,
		Classifier:          &mockClassifier{response: "PHOTO 0.99"},
		ExtraBlockedDomains: []string{"mystock.example.com"},
	}
	results := cfg.SearchImages(context.Background(), "test", 5)

	// Image should be blocked by extra domain list during assessment.
	if len(results) != 0 {
		t.Errorf("expected 0 results (blocked by extra domain), got %d", len(results))
	}
}
```

### Step 2: Run tests to verify they fail

Run: `cd /home/krolik/src/go-imagefy && go test -run "TestSearchImages_Metadata|TestSearchImages_ExtraBlocked" -v`
Expected: FAIL (tests may fail due to missing `buildSearxngJSON` helper or logic changes)

### Step 3: Refactor downloadAndDecode to return raw bytes

In `dedup.go`, add a new method alongside existing one:

```go
// downloadForValidation fetches the image and returns both raw bytes and decoded image.
// Raw bytes are used for metadata extraction; decoded image is used for perceptual dedup.
// Returns (nil, nil) on any recoverable failure for graceful degradation.
func (cfg *Config) downloadForValidation(ctx context.Context, url string) ([]byte, image.Image) {
	result, err := cfg.Download(ctx, url, DownloadOpts{})
	if err != nil || result == nil {
		return nil, nil
	}

	img, _, err := image.Decode(bytes.NewReader(result.Data))
	if err != nil {
		// Raw bytes available for metadata even if image decode fails.
		return result.Data, nil
	}

	return result.Data, img
}
```

### Step 4: Update validateOne to use assessment

In `search.go`, replace the current `validateOne` method:

```go
func (cfg *Config) validateOne(ctx context.Context, cand ImageCandidate, maxResults int, mu *sync.Mutex, validated *[]ImageCandidate, dedup *dedupFilter) {
	defer func() {
		if r := recover(); r != nil {
			if cfg.OnPanic != nil {
				cfg.OnPanic("imageValidation", r)
			}
		}
	}()

	if !cfg.ValidateImageURL(ctx, cand.ImgURL) {
		return
	}

	// Download once for both dedup and metadata extraction.
	data, img := cfg.downloadForValidation(ctx, cand.ImgURL)

	// Dedup check using perceptual hash.
	if img != nil {
		if dedup.isDuplicate(img) {
			slog.Debug("imagefy: dedup rejected", "url", cand.ImgURL)
			return
		}
	}

	// Extract metadata and assess license.
	meta := ExtractImageMetadata(data)
	assessment := cfg.AssessLicense(cand, meta)

	if assessment.License == LicenseBlocked {
		slog.Debug("imagefy: blocked by license assessment", "url", cand.ImgURL, "signals", assessment.Signals)
		if cfg.OnClassification != nil {
			cfg.OnClassification(ClassificationEvent{
				URL:    cand.ImgURL,
				Class:  ClassStock,
				Source: "license_assessment",
			})
		}
		return
	}

	if assessment.License == LicenseSafe {
		slog.Debug("imagefy: safe by license assessment", "url", cand.ImgURL, "signals", assessment.Signals)
		if cfg.OnClassification != nil {
			cfg.OnClassification(ClassificationEvent{
				URL:        cand.ImgURL,
				Class:      ClassPhoto,
				Confidence: 1.0,
				Source:     "license_assessment",
			})
		}
		mu.Lock()
		if len(*validated) < maxResults {
			*validated = append(*validated, cand)
		}
		mu.Unlock()
		return
	}

	// Unknown license — fall through to LLM classification.
	if !cfg.IsRealPhoto(ctx, cand.ImgURL) {
		slog.Debug("imagefy: vision rejected", "url", cand.ImgURL)
		return
	}
	mu.Lock()
	if len(*validated) < maxResults {
		*validated = append(*validated, cand)
	}
	mu.Unlock()
}
```

### Step 5: Update PreClassify (keep for backward compat but mark as superseded)

In `prefilter.go`, add a comment noting it's superseded by AssessLicense in the pipeline:

```go
// PreClassify applies cheap heuristics to classify an image candidate without
// calling the LLM. Returns the predicted class and skip=true if the heuristic
// is conclusive. Returns ("", false) if the LLM should be consulted.
//
// Note: In the search pipeline, this function is superseded by Config.AssessLicense
// which combines domain, metadata, and CC signals. PreClassify remains available
// as a standalone helper for consumers who don't use the full search pipeline.
//
// Current heuristics:
//   - LicenseSafe sources (Openverse, Unsplash, Pixabay) → auto-accept as PHOTO.
//     These are curated CC/public-domain collections with negligible false-positive risk.
func PreClassify(cand ImageCandidate) (class string, skip bool) {
	if cand.License == LicenseSafe {
		return ClassPhoto, true
	}
	return "", false
}
```

### Step 6: Run all tests

Run: `cd /home/krolik/src/go-imagefy && go test ./... -v -race -count=1`
Expected: ALL PASS

**Note:** Some existing tests may need adjustment if they relied on the old `validateOne` flow calling `PreClassify` then `IsRealPhoto`. The new flow calls `AssessLicense` then `IsRealPhoto`. The `OnClassification` callback source changes from `"prefilter"` to `"license_assessment"` for safe images. Check `search_test.go` for any tests that assert on `Source: "prefilter"` and update them.

### Step 7: Run linter

Run: `cd /home/krolik/src/go-imagefy && make lint`
Expected: PASS

### Step 8: Commit

```bash
cd /home/krolik/src/go-imagefy
git add dedup.go search.go search_test.go prefilter.go prefilter_test.go
git commit -m "feat: wire metadata extraction and license assessment into pipeline

Refactor validateOne to download once for dedup + metadata.
Replace PreClassify with AssessLicense in search pipeline.
Block stock images detected by IPTC/EXIF metadata without LLM.
Accept CC-licensed images detected by XMP metadata without LLM.
Support ExtraBlockedDomains/ExtraSafeDomains in pipeline.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 6: Documentation & Roadmap Update

**Goal:** Update README and ROADMAP to reflect Phase 4 completion.

**Files:**
- Modify: `README.md`
- Modify: `docs/ROADMAP.md`

**Depends on:** Task 5.

### Step 1: Update ROADMAP.md

Mark Phase 4 items as complete:

```markdown
## Phase 4 — Extended License Intelligence (done)

**Goal:** metadata-based license detection, moving beyond domain heuristics.

Key library: [`bep/imagemeta`](https://github.com/bep/imagemeta) (pure Go, EXIF+IPTC+XMP).

- [x] **IPTC/EXIF metadata reading** — `ExtractImageMetadata()` extracts `Copyright`, `Credit`, `Byline`, `Source` (IPTC), `Copyright`, `Artist` (EXIF), and XMP rights fields from image data via `bep/imagemeta`. Integrated into search pipeline — downloaded image bytes parsed once for both dedup and metadata.
- [x] **Creative Commons scanning in HTML** — `ExtractCCLicense()` scans HTML for `rel="license"` links and CC URLs (`creativecommons.org/licenses/`, `/publicdomain/`). Standalone L0 helper, like `ExtractOGImageURL`.
- [x] **XMP Rights parsing** — `XMPWebStatement`, `XMPUsageTerms`, `XMPLicense`, `XMPMarked` fields extracted from XMP metadata. CC license URLs in XMP promote images to `LicenseSafe`.
- [x] **Configurable domain lists** — `Config.ExtraBlockedDomains` and `Config.ExtraSafeDomains` allow consumers to extend the built-in 25+/11 domain lists. `CheckLicenseWith()` function for ad-hoc use.
- [x] **License assessment** — `Config.AssessLicense()` combines domain, metadata stock, and metadata CC signals into `LicenseAssessment` with signal transparency. Replaces `PreClassify` in the search pipeline. Blocked always takes precedence.
- [ ] **IPTC 2025.1 AI fields** — deferred (requires field support in bep/imagemeta).
- [ ] **License confidence scoring** — deferred (signals list provides transparency; numeric scoring adds complexity without clear consumer demand).
- [ ] **PLUS vocabulary support** — deferred (low adoption outside major agencies).
```

### Step 2: Update README.md

Add a "License Intelligence" section to the README feature list:

```markdown
### License Intelligence (Phase 4)

- **Image metadata extraction** — reads IPTC, EXIF, and XMP rights fields from image data
  - IPTC: CopyrightNotice, Credit, Byline, Source
  - EXIF: Copyright, Artist
  - XMP: WebStatement, UsageTerms, License, Marked, dc:rights, dc:creator
- **Stock agency detection** — blocks images with stock watermark fingerprints in metadata (Shutterstock, Getty, iStock, Adobe Stock, etc.) without LLM call
- **CC license detection** — promotes images with Creative Commons licenses in XMP/DC metadata to `LicenseSafe` without LLM call
- **HTML CC scanning** — `ExtractCCLicense()` finds CC license links in HTML pages
- **Configurable domains** — `ExtraBlockedDomains`/`ExtraSafeDomains` in Config
- **Transparent assessment** — `AssessLicense()` returns signals explaining the license decision
```

Add API reference entries for new public functions:

```markdown
#### License Assessment

| Function | Layer | Description |
|----------|-------|-------------|
| `ExtractImageMetadata(data []byte) *ImageMetadata` | L0 | Extract IPTC/EXIF/XMP rights metadata |
| `IsStockByMetadata(meta *ImageMetadata) bool` | L0 | Detect stock agency fingerprints |
| `IsCCByMetadata(meta *ImageMetadata) bool` | L0 | Detect Creative Commons license |
| `ExtractCCLicense(html string) string` | L0 | Scan HTML for CC license URLs |
| `IsCCLicenseURL(url string) bool` | L0 | Check if URL is a CC license |
| `CheckLicenseWith(imageURL, sourceURL string, extraBlocked, extraSafe []string) ImageLicense` | L0 | Extended domain check |
| `cfg.AssessLicense(cand ImageCandidate, meta *ImageMetadata) LicenseAssessment` | L0 | Composite license verdict |
```

### Step 3: Commit

```bash
cd /home/krolik/src/go-imagefy
git add README.md docs/ROADMAP.md
git commit -m "docs: update README and ROADMAP for Phase 4 completion

Document metadata extraction, stock/CC detection, configurable domains,
and license assessment features.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Execution Summary

| Task | Files | New Dep | Parallel? |
|------|-------|---------|-----------|
| 1. Configurable domains | license.go, imagefy.go | — | Yes (with 2, 3) |
| 2. Metadata extraction | metadata.go | bep/imagemeta | Yes (with 1, 3) |
| 3. CC HTML scanning | cclicense.go | — | Yes (with 1, 2) |
| 4. License assessment | assessment.go | — | After 1, 2, 3 |
| 5. Pipeline integration | search.go, dedup.go | — | After 4 |
| 6. Documentation | README.md, ROADMAP.md | — | After 5 |

**Total new files:** 6 (3 source + 3 test)
**Modified files:** 7 (imagefy.go, license.go, license_test.go, dedup.go, search.go, search_test.go, prefilter.go)
**New dependency:** `github.com/bep/imagemeta` (pure Go, zero CGO)
**Estimated test count:** ~30 new tests
