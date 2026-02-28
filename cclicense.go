package imagefy

import (
	"html"
	"regexp"
	"strings"
)

// ccLicensePathSegments are URL path prefixes that identify a Creative Commons
// license or public-domain dedication (as opposed to the CC homepage).
var ccLicensePathSegments = []string{
	"creativecommons.org/licenses/",
	"creativecommons.org/publicdomain/",
}

// IsCCLicenseURL reports whether rawURL points to a Creative Commons license.
// It matches URLs containing "creativecommons.org/licenses/" or
// "creativecommons.org/publicdomain/". Case-insensitive. Works with https,
// http, and protocol-relative ("//...") URLs.
// Returns false for empty string and the CC homepage without a license path.
func IsCCLicenseURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	lower := strings.ToLower(rawURL)
	for _, seg := range ccLicensePathSegments {
		if strings.Contains(lower, seg) {
			return true
		}
	}
	return false
}

// Compiled regexes for extracting CC license URLs from HTML.
// Order matters: patterns with rel="license" are tried first.
var (
	// Pattern 1: rel="license" ... href="URL"
	ccRelHrefRe = regexp.MustCompile(
		`(?i)rel=["']license["'][^>]*href=["']([^"']+)["']`,
	)
	// Pattern 2: href="URL" ... rel="license" (reversed attribute order)
	ccHrefRelRe = regexp.MustCompile(
		`(?i)href=["']([^"']+)["'][^>]*rel=["']license["']`,
	)
	// Pattern 3: href="URL" where URL contains a CC license path
	ccBareHrefRe = regexp.MustCompile(
		`(?i)href=["']((?:https?:)?//creativecommons\.org/(?:licenses|publicdomain)/[^"']+)["']`,
	)
	// Pattern 4: content="URL" in meta tags with CC URLs
	ccMetaContentRe = regexp.MustCompile(
		`(?i)content=["']((?:https?:)?//creativecommons\.org/(?:licenses|publicdomain)/[^"']+)["']`,
	)
)

// ExtractCCLicense scans HTML for Creative Commons license references.
// Returns the first CC license URL found, or empty string if none.
func ExtractCCLicense(pageHTML string) string {
	// Try rel="license" patterns first (most authoritative).
	if url := matchCCFromRel(ccRelHrefRe, pageHTML); url != "" {
		return url
	}
	if url := matchCCFromRel(ccHrefRelRe, pageHTML); url != "" {
		return url
	}
	// Bare CC href (no rel="license" required).
	if m := ccBareHrefRe.FindStringSubmatch(pageHTML); m != nil {
		return html.UnescapeString(m[1])
	}
	// Meta tag content attribute.
	if m := ccMetaContentRe.FindStringSubmatch(pageHTML); m != nil {
		return html.UnescapeString(m[1])
	}
	return ""
}

// matchCCFromRel extracts a URL from a rel="license" regex match and returns
// it only if it is a valid CC license URL.
func matchCCFromRel(re *regexp.Regexp, pageHTML string) string {
	m := re.FindStringSubmatch(pageHTML)
	if m == nil {
		return ""
	}
	url := html.UnescapeString(m[1])
	if IsCCLicenseURL(url) {
		return url
	}
	return ""
}
