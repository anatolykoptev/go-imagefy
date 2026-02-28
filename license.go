package imagefy

import (
	"net/url"
	"strings"
)

// ImageLicense classifies an image source by copyright safety.
type ImageLicense int

const (
	LicenseSafe    ImageLicense = iota // known free source (unsplash, pixabay, etc.)
	LicenseUnknown                     // no info — usable with caution
	LicenseBlocked                     // stock site — reject entirely
)

func (l ImageLicense) String() string {
	switch l {
	case LicenseSafe:
		return "safe"
	case LicenseBlocked:
		return "blocked"
	default:
		return "unknown"
	}
}

// BlockedDomains are stock photo sites that enforce copyright and send invoices.
var BlockedDomains = []string{
	"shutterstock",
	"gettyimages",
	"istockphoto",
	"adobestock",
	"depositphotos",
	"dreamstime",
	"123rf",
	"alamy",
	"bigstockphoto",
	"stocksy",
	"eyeem",
	"pond5",
	"thinkstockphotos", // Getty subsidiary
	"canstockphoto",
	"masterfile",
	"superstock",
	"agefotostock",
	"colourbox",
	"photodune",   // Envato marketplace
	"yayimages",
	"vectorstock",
	"loriimages",  // Russian stock (Лори)
	"fotobank",    // Russian stock
	"freepik",     // active DMCA enforcement
	"canva.",      // freemium stock elements (trailing dot avoids matching "canvas")
	"clipartof",
	"featurepics",
	"rfclipart",
}

// BlockedURLPatterns are URL path segments that indicate stock photo pages.
var BlockedURLPatterns = []string{
	"/stock-photo",
	"/stock-image",
	"/editorial-image",
	"/premium-photo",
}

// SafeDomains are free / CC / attribution-friendly image sources.
var SafeDomains = []string{
	"unsplash",
	"pexels",
	"pixabay",
	"wikimedia",
	"commons.wikimedia",
	"flickr",
	"rawpixel",
	"stocksnap",
	"burst.shopify",
	"kaboompics",
	"picjumbo",
}

// CheckLicense classifies an image by checking its URL and source page URL
// against known blocked (stock) and safe (free/CC) domain lists.
// Both URLs are checked — an image hosted on a CDN may still originate from a stock site.
// Also checks URL path patterns that indicate stock photo pages.
func CheckLicense(imageURL, sourceURL string) ImageLicense {
	return CheckLicenseWith(imageURL, sourceURL, nil, nil)
}

// CheckLicenseWith is like CheckLicense but also checks caller-supplied extra
// blocked and safe domain lists. The extra slices use the same substring-match
// semantics as the built-in BlockedDomains / SafeDomains.
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

// isBlockedWith reports whether the URL matches a blocked domain, URL pattern,
// or any of the extra blocked domains.
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

// isSafeWith reports whether the URL matches a known safe/free domain
// or any of the extra safe domains.
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

func extractHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Host)
}
