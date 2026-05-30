package imagefy

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	contentFetchTimeout = 10 * time.Second
	contentBodyLimit    = 2 * 1024 * 1024 // 2MB
	contentMaxResults   = 8
)

// contentSkipPatterns are filename substrings indicating non-content images.
// Checked case-insensitively against the img src URL.
var contentSkipPatterns = []string{
	"logo", "icon", "avatar", "sprite", "favicon",
	"placeholder", "stub", "banner",
}

// contentSizeRe matches trailing size suffix like -320x240 or -200x150 in a
// filename before the extension. Used to detect (and strip) thumbnail variants.
var contentSizeRe = regexp.MustCompile(`-(\d+)x(\d+)$`)

// contentImgRe extracts src and optional width/height from <img> tags.
// Captures groups: 1=src from src="...", 2=src from src='...', 3=width, 4=height.
var contentImgRe = regexp.MustCompile(
	`(?i)<img\b[^>]*?\bsrc=["']([^"'>]+)["'][^>]*>|<img\b[^>]*?\bsrc='([^'>]+)'[^>]*>`,
)

// contentImgAttrRe extracts individual attributes from an <img> tag string.
var (
	contentSrcRe    = regexp.MustCompile(`(?i)\bsrc=["']([^"']+)["']`)
	contentWidthRe  = regexp.MustCompile(`(?i)\bwidth=["']?(\d+)["']?`)
	contentHeightRe = regexp.MustCompile(`(?i)\bheight=["']?(\d+)["']?`)
	contentImgTagRe = regexp.MustCompile(`(?i)<img\b[^>]*>`)
)

// twitterImageRe matches twitter:image meta tags.
var twitterImageRe = regexp.MustCompile(
	`(?i)<meta\s+[^>]*(?:name|property)=["']twitter:image["'][^>]*content=["']([^"']+)["']|` +
		`<meta\s+[^>]*content=["']([^"']+)["'][^>]*(?:name|property)=["']twitter:image["']`,
)

// jsonLDImageRe extracts "image" fields from JSON-LD blocks.
var jsonLDScriptRe = regexp.MustCompile(
	`(?is)<script\b[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`,
)

// ContentImageProvider fetches a page and extracts the best content image.
// Candidates are returned in priority order:
//  1. Content <img> tags whose src is on the same registrable domain (slug-matched first).
//  2. JSON-LD image fields.
//  3. og:image and twitter:image (kept as lowest-priority fallback).
//
// The PageURL is passed via SearchOpts.PageURL; the query parameter is used
// only for slug-matching (shared tokens between query/page path and image filename).
type ContentImageProvider struct {
	HTTPClient *http.Client
}

// Name returns the provider name.
func (p *ContentImageProvider) Name() string { return "content" }

// Search fetches opts.PageURL, extracts image candidates, and returns them sorted
// best-first (content images > og/twitter fallback). Returns empty on any fetch failure.
func (p *ContentImageProvider) Search(ctx context.Context, query string, opts SearchOpts) ([]ImageCandidate, error) {
	if opts.PageURL == "" {
		return nil, nil
	}

	pageBody, err := p.fetchPage(ctx, opts.PageURL)
	if err != nil || pageBody == "" {
		return nil, nil
	}

	pageHost := registrableDomain(opts.PageURL)
	slugTokens := slugTokensFrom(opts.PageURL, query)

	var priority []ImageCandidate // slug-matched content images
	var normal []ImageCandidate   // other content images on same domain
	var jsonld []ImageCandidate   // JSON-LD images
	var ogFallback []ImageCandidate

	seen := map[string]struct{}{}

	addIfNew := func(imgURL string, title string, bucket *[]ImageCandidate) {
		clean := html.UnescapeString(strings.TrimSpace(imgURL))
		if clean == "" || !strings.HasPrefix(clean, "http") {
			return
		}
		norm := normalizeImgURL(clean)
		if _, dup := seen[norm]; dup {
			return
		}
		seen[norm] = struct{}{}
		if IsLogoOrBanner(strings.ToLower(clean)) {
			return
		}
		license := CheckLicense(clean, opts.PageURL)
		if license == LicenseBlocked {
			return
		}
		*bucket = append(*bucket, ImageCandidate{
			ImgURL:  clean,
			Source:  opts.PageURL,
			Title:   title,
			License: license,
		})
	}

	// 1. Extract og:image and twitter:image (kept as fallback).
	if ogURL := ExtractOGImageURL(pageBody); ogURL != "" {
		addIfNew(ogURL, "og:image", &ogFallback)
	}
	if m := twitterImageRe.FindStringSubmatch(pageBody); m != nil {
		tw := m[1]
		if tw == "" {
			tw = m[2]
		}
		addIfNew(tw, "twitter:image", &ogFallback)
	}

	// 2. Extract JSON-LD image fields.
	for _, match := range jsonLDScriptRe.FindAllStringSubmatch(pageBody, -1) {
		if u := extractJSONLDImage(match[1]); u != "" {
			addIfNew(u, "jsonld:image", &jsonld)
		}
	}

	// 3. Extract <img> tags — content images on same domain.
	for _, imgTag := range contentImgTagRe.FindAllString(pageBody, -1) {
		src := extractAttr(contentSrcRe, imgTag)
		if src == "" {
			continue
		}
		src = html.UnescapeString(strings.TrimSpace(src))
		if !strings.HasPrefix(src, "http") {
			continue
		}

		// Same registrable domain only.
		if registrableDomain(src) != pageHost || pageHost == "" {
			continue
		}

		// Skip obvious non-content by filename.
		lower := strings.ToLower(src)
		if isContentSkip(lower) {
			continue
		}

		// Skip tiny thumbnails by size suffix or explicit attrs.
		if isTinyThumbnail(imgTag, src) {
			continue
		}

		// Prefer full-size by stripping trailing -WxH suffix.
		src = normalizeImgURL(src)

		if isSlugMatch(src, slugTokens) {
			addIfNew(src, "content:img:slug", &priority)
		} else {
			addIfNew(src, "content:img", &normal)
		}
	}

	// Merge: priority slug > normal content > jsonld > og fallback.
	var out []ImageCandidate
	out = append(out, priority...)
	out = append(out, normal...)
	out = append(out, jsonld...)
	out = append(out, ogFallback...)

	if len(out) > contentMaxResults {
		out = out[:contentMaxResults]
	}
	return out, nil
}

// fetchPage performs a GET request for pageURL and returns the response body.
func (p *ContentImageProvider) fetchPage(ctx context.Context, pageURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, contentFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; go-imagefy/1.0)")

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req) //nolint:gosec // G107: URL is caller-supplied
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return "", nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, contentBodyLimit))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// registrableDomain returns the eTLD+1 from a URL, e.g. "kpcdn.net" for
// "https://s13.stc.all.kpcdn.net/...". Falls back to Host on parse error.
// Returns empty string on invalid URL.
func registrableDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	// Return last two labels (eTLD+1 approximation — good enough for CDN matching).
	return strings.Join(parts[len(parts)-2:], ".")
}

// slugTokensFrom extracts short slug tokens from the page URL path and query string.
// Tokens must be ≥4 characters to avoid noise.
func slugTokensFrom(pageURL, query string) []string {
	var tokens []string
	seen := map[string]struct{}{}

	add := func(s string) {
		s = strings.ToLower(s)
		if len(s) >= 4 {
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				tokens = append(tokens, s)
			}
		}
	}

	if u, err := url.Parse(pageURL); err == nil {
		seg := path.Base(u.Path)
		for _, part := range regexp.MustCompile(`[^a-z0-9]+`).Split(strings.ToLower(seg), -1) {
			add(part)
		}
	}

	for _, word := range regexp.MustCompile(`[^a-z0-9]+`).Split(strings.ToLower(query), -1) {
		add(word)
	}

	return tokens
}

// isSlugMatch reports whether any slug token appears in the image URL filename.
func isSlugMatch(imgURL string, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}
	lower := strings.ToLower(path.Base(imgURL))
	for _, t := range tokens {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}

// isContentSkip reports whether the image filename matches a known non-content pattern.
func isContentSkip(lower string) bool {
	for _, p := range contentSkipPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// isTinyThumbnail reports true if the image URL contains a size suffix with max
// dimension < 200, or explicit width/height HTML attrs both < 200.
const minContentDimension = 200

func isTinyThumbnail(imgTag, src string) bool {
	// Check URL size suffix e.g. photo-320x240.jpg
	base := strings.TrimSuffix(path.Base(src), path.Ext(src))
	if m := contentSizeRe.FindStringSubmatch(base); m != nil {
		w := parseIntFast(m[1])
		h := parseIntFast(m[2])
		if w > 0 && h > 0 && w < minContentDimension && h < minContentDimension {
			return true
		}
	}

	// Check explicit HTML attrs.
	wStr := extractAttr(contentWidthRe, imgTag)
	hStr := extractAttr(contentHeightRe, imgTag)
	if wStr != "" && hStr != "" {
		w := parseIntFast(wStr)
		h := parseIntFast(hStr)
		if w > 0 && h > 0 && w < minContentDimension && h < minContentDimension {
			return true
		}
	}
	return false
}

// normalizeImgURL strips a trailing -WxH size suffix from the filename stem,
// returning the presumed full-size URL.
// Example: https://cdn.example.com/photo-640x480.jpg → https://cdn.example.com/photo.jpg
func normalizeImgURL(imgURL string) string {
	ext := path.Ext(imgURL)
	stem := strings.TrimSuffix(imgURL, ext)
	if contentSizeRe.MatchString(path.Base(stem)) {
		return contentSizeRe.ReplaceAllString(stem, "") + ext
	}
	return imgURL
}

// extractAttr returns the first capture group from re applied to s.
func extractAttr(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if m == nil || m[1] == "" {
		return ""
	}
	return m[1]
}

// parseIntFast parses a non-negative integer without importing strconv.
func parseIntFast(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// extractJSONLDImage parses a raw JSON-LD block and returns the "image" value.
// Handles both string and {"url":"..."} shapes. Returns empty on parse failure.
func extractJSONLDImage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return ""
	}

	imageRaw, ok := doc["image"]
	if !ok {
		return ""
	}

	// Try plain string.
	var s string
	if err := json.Unmarshal(imageRaw, &s); err == nil && s != "" {
		return s
	}

	// Try {"url":"..."}
	var obj struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(imageRaw, &obj); err == nil && obj.URL != "" {
		return obj.URL
	}

	// Try array — take first element.
	var arr []json.RawMessage
	if err := json.Unmarshal(imageRaw, &arr); err == nil && len(arr) > 0 {
		var first string
		if err2 := json.Unmarshal(arr[0], &first); err2 == nil {
			return first
		}
		var firstObj struct {
			URL string `json:"url"`
		}
		if err2 := json.Unmarshal(arr[0], &firstObj); err2 == nil {
			return firstObj.URL
		}
	}

	return ""
}
