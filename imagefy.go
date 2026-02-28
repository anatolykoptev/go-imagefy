package imagefy

import (
	"context"
	"net/http"
	"time"
)

// DefaultMinImageWidth is the minimum pixel width for accepted images.
const DefaultMinImageWidth = 880

// ImageInput represents an image for multimodal LLM classification.
type ImageInput struct {
	URL      string // data: URI or HTTP URL
	MIMEType string // e.g. "image/jpeg"
}

// Cache abstracts key-value caching (Redis, sync.Map, etc.)
type Cache interface {
	Key(prefix, value string) string
	Get(ctx context.Context, key string, dest any) bool
	Set(ctx context.Context, key string, value any)
}

// Classifier abstracts multimodal LLM calls for image classification.
type Classifier interface {
	Classify(ctx context.Context, prompt string, images []ImageInput) (string, error)
}

// Config holds all dependencies injected by the consumer.
type Config struct {
	Cache         Cache        // required for ClassifyImage (nil = no caching)
	Classifier    Classifier   // required for ClassifyImage (nil = skip classification)
	StealthClient *http.Client // optional: TLS-fingerprinted client for downloads
	HTTPClient    *http.Client // optional: default http client (nil = http.DefaultClient)
	SearxngURL    string       // required for SearchImages when Providers is empty
	MinImageWidth int          // default: DefaultMinImageWidth (880)
	UserAgent     string       // default: "Mozilla/5.0 (compatible; go-imagefy/1.0)"

	// Providers is an optional list of search backends. When non-empty, these are
	// used instead of auto-creating a SearXNGProvider from SearxngURL.
	// When multiple providers are supplied, results are merged and sorted by license.
	Providers []SearchProvider

	// VisionPrompt overrides the default classification prompt (DefaultVisionPrompt).
	// Set this to customize the LLM instruction for ClassifyImageFull / ClassifyImage.
	VisionPrompt string

	// ExtraBlockedDomains are additional stock/copyrighted domains to block.
	ExtraBlockedDomains []string

	// ExtraSafeDomains are additional free/CC domains to treat as safe.
	ExtraSafeDomains []string

	// Optional callbacks for metrics/logging.
	OnImageSearch    func()
	OnPanic          func(tag string, r any)
	OnClassification func(ClassificationEvent) // optional: audit log for every classification decision
}

// SearchOpts configures image search behavior.
// Zero values mean "use defaults": PageNumber 0 or 1 = page 1, empty Engines = all engines, zero Timeout = 15s.
type SearchOpts struct {
	PageNumber int           // SearXNG page number (default: 1)
	Engines    []string      // SearXNG engines to use (default: all)
	Timeout    time.Duration // search timeout (default: 15s)
}

// defaults fills zero-value fields with sensible defaults.
// Called by methods in Layer 1 (download.go) and Layer 2 (search.go).
func (c *Config) defaults() { //nolint:unused // called by Layer 1/2 methods added in next tasks
	if c.MinImageWidth <= 0 {
		c.MinImageWidth = DefaultMinImageWidth
	}
	if c.UserAgent == "" {
		c.UserAgent = "Mozilla/5.0 (compatible; go-imagefy/1.0)"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
}
