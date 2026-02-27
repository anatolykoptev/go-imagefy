package imagefy

import (
	"encoding/base64"
	"fmt"
	"html"
	"regexp"
)

var ogImageRe = regexp.MustCompile(
	`(?i)<meta\s+[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["']|` +
		`<meta\s+[^>]*content=["']([^"']+)["'][^>]*property=["']og:image["']`,
)

// ExtractOGImageURL pulls the og:image URL from raw HTML.
// Returns empty string if not found.
func ExtractOGImageURL(pageHTML string) string {
	m := ogImageRe.FindStringSubmatch(pageHTML)
	if m == nil {
		return ""
	}
	img := m[1]
	if img == "" {
		img = m[2]
	}
	if img == "" {
		return ""
	}
	return html.UnescapeString(img)
}

// EncodeBase64 encodes bytes to base64 string.
func EncodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// EncodeDataURL creates a data: URI from bytes and MIME type.
func EncodeDataURL(data []byte, mimeType string) string {
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
}
