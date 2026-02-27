package imagefy

import "strings"

// LogoBannerPatterns are URL substrings indicating non-photo images.
var LogoBannerPatterns = []string{
	"favicon", "logo", "icon", "banner", "sprite",
	"badge", "button", "widget", "avatar",
}

// IsLogoOrBanner checks if a lowercased URL contains logo/banner patterns.
func IsLogoOrBanner(lower string) bool {
	for _, p := range LogoBannerPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
