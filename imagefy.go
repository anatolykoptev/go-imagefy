package imagefy

import (
	"context"
	"net/http"
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
	SearxngURL    string       // required for SearchImages
	MinImageWidth int          // default: DefaultMinImageWidth (880)
	UserAgent     string       // default: "Mozilla/5.0 (compatible; go-imagefy/1.0)"

	// Optional callbacks for metrics/logging.
	OnImageSearch func()
	OnPanic       func(tag string, r any)
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
