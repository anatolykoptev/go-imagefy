# go-imagefy

A Go library for editorial image search, validation, and classification. Finds free-to-use photographs via pluggable search providers (SearXNG, Openverse), filters out stock photo sites, validates dimensions, deduplicates with perceptual hashing, and classifies images using a multimodal LLM to reject banners, watermarked stock previews, screenshots, illustrations, maps, and other non-photographic content.

## Features

- **Multi-provider image search** — pluggable `SearchProvider` interface with built-in SearXNG and Openverse backends. Merge results from multiple sources with license-aware sorting.
- **Perceptual hash dedup** — `corona10/goimagehash` dHash eliminates visually identical images before expensive LLM classification.
- **6-class LLM classification** — PHOTO, STOCK, REJECT, SCREENSHOT, ILLUSTRATION, MAP with confidence scores (0.0–1.0).
- **Cost-tier routing** — `PreClassify` auto-accepts images from safe sources (Openverse, Unsplash, Pixabay) without calling the LLM.
- **Custom classification prompts** — override `DefaultVisionPrompt` via `Config.VisionPrompt` for NSFW detection, e-commerce filtering, or any domain-specific use case.
- **Classification audit log** — `OnClassification` callback with URL, class, confidence, and source (LLM vs prefilter) for debugging and metrics.
- **License checking** — blocks 25+ stock photo domains (Shutterstock, Getty, Alamy, etc.), prioritizes free sources (Unsplash, Pexels, Pixabay, Wikimedia). Configurable via `ExtraBlockedDomains` / `ExtraSafeDomains`.
- **Image metadata extraction** — IPTC, EXIF, and XMP rights fields via `bep/imagemeta`. Detects stock agencies and Creative Commons licenses from embedded metadata.
- **License assessment** — composite `AssessLicense()` combines domain heuristics, metadata stock signals, and CC detection with transparent signal reporting.
- **HTML CC scanning** — `ExtractCCLicense()` finds `rel="license"` links and CC URLs in HTML pages.
- **URL validation** — checks HTTP status, content type, minimum width, logo/banner URL patterns.
- **Image download** with stealth client fallback for anti-bot protection.
- **Search query builder** — extracts meaningful words from titles, strips Russian stop words.
- **OG image extraction** from HTML pages.
- **Dependency injection** — bring your own cache, classifier, and HTTP clients.

## Installation

```bash
go get github.com/anatolykoptev/go-imagefy@latest
```

Requires Go 1.24+.

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    "github.com/anatolykoptev/go-imagefy"
)

func main() {
    cfg := &imagefy.Config{
        SearxngURL:    "http://localhost:8888",
        MinImageWidth: 880,
    }

    results := cfg.SearchImages(context.Background(), "Hermitage Museum Saint Petersburg", 5)
    for _, img := range results {
        fmt.Printf("%s [%s] %s\n", img.License, img.Title, img.ImgURL)
    }
}
```

### Multi-provider search with Openverse

```go
cfg := &imagefy.Config{
    Providers: []imagefy.SearchProvider{
        &imagefy.SearXNGProvider{URL: "http://localhost:8888"},
        &imagefy.OpenverseProvider{}, // 842M+ CC/public-domain images
    },
    MinImageWidth: 880,
}

// Results are merged and sorted: LicenseSafe first, then LicenseUnknown.
// Openverse results skip LLM classification entirely (cost savings).
results := cfg.SearchImages(ctx, "Kazan Cathedral", 5)
```

### With classification, caching, and audit log

```go
cfg := &imagefy.Config{
    SearxngURL:    "http://localhost:8888",
    Cache:         myRedisCache,    // implements imagefy.Cache
    Classifier:    myVisionLLM,     // implements imagefy.Classifier
    StealthClient: stealthHTTP,     // TLS-fingerprinted *http.Client (optional)
    MinImageWidth: 880,

    OnClassification: func(e imagefy.ClassificationEvent) {
        log.Printf("classified %s as %s (conf=%.2f, source=%s)",
            e.URL, e.Class, e.Confidence, e.Source)
    },
}

// Full pipeline: search → filter → validate → dedup → metadata → license assess → classify
results := cfg.SearchImages(ctx, "Nevsky Prospekt", 3)

// Or classify a single image with confidence
result := cfg.ClassifyImageFull(ctx, "https://example.com/photo.jpg")
// result.Class: "PHOTO", "STOCK", "REJECT", "SCREENSHOT", "ILLUSTRATION", "MAP", or ""
// result.Confidence: 0.0–1.0
```

### Custom prompt (NSFW detection example)

```go
cfg := &imagefy.Config{
    Classifier: myVisionLLM,
    VisionPrompt: `Classify this image. Answer with one word and confidence:
- SAFE — appropriate content
- NSFW — nudity or sexual content
- VIOLENCE — graphic violence
Answer format: CLASS 0.95`,
}

result := cfg.ClassifyImageFull(ctx, imageURL)
// result.Class: "SAFE", "NSFW", "VIOLENCE"
// result.Confidence: 0.92
```

### Pagination and engine selection

```go
results := cfg.SearchImagesWithOpts(ctx, "city park", 10, imagefy.SearchOpts{
    PageNumber: 2,                          // SearXNG page 2
    Engines:    []string{"google", "bing"}, // specific engines only
    Timeout:    30 * time.Second,           // custom timeout
})
```

## Architecture

```
Layer 0 (pure logic, no I/O)
├── license.go      — CheckLicense, CheckLicenseWith, blocked/safe domain lists
├── metadata.go     — ExtractImageMetadata, IsStockByMetadata, IsCCByMetadata
├── cc.go           — ExtractCCLicense, IsCCLicenseURL
├── assess.go       — AssessLicense, LicenseAssessment with signals
├── patterns.go     — IsLogoOrBanner, URL pattern detection
├── query.go        — BuildImageQuery, stop word filtering
├── helpers.go      — ExtractOGImageURL, EncodeDataURL, EncodeBase64
└── prefilter.go    — PreClassify cost-tier routing

Layer 1 (HTTP, no external services beyond target URLs)
├── download.go     — Download with stealth fallback
├── validate.go     — ValidateImageURL (HTTP probe + dimension check)
└── dedup.go        — Perceptual hash dedup (dHash + Hamming distance)

Layer 2 (orchestration, uses interfaces)
├── classify.go     — ClassifyImage / ClassifyImageFull / IsRealPhoto
├── search.go       — SearchImages / SearchImagesWithOpts (pipeline)
├── provider.go     — SearchProvider interface, SearXNGProvider
└── openverse.go    — OpenverseProvider (Openverse API client)
```

## API Reference

### Config

```go
type Config struct {
    Cache         Cache            // optional: caching for classification results
    Classifier    Classifier       // optional: multimodal LLM for image classification
    StealthClient *http.Client     // optional: TLS-fingerprinted client for downloads
    HTTPClient    *http.Client     // optional: default HTTP client (nil = http.DefaultClient)
    SearxngURL    string           // required for SearchImages when Providers is empty
    MinImageWidth int              // default: 880px
    UserAgent     string           // default: "Mozilla/5.0 (compatible; go-imagefy/1.0)"
    Providers     []SearchProvider // optional: search backends (default: auto-create from SearxngURL)
    VisionPrompt  string           // optional: custom classification prompt (default: DefaultVisionPrompt)

    ExtraBlockedDomains []string   // optional: additional stock domains to block
    ExtraSafeDomains    []string   // optional: additional free-use domains

    OnImageSearch    func()                      // optional: metrics callback
    OnPanic          func(tag string, r any)     // optional: panic recovery callback
    OnClassification func(ClassificationEvent)   // optional: audit log for every classification
}
```

### Interfaces

```go
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

// SearchProvider abstracts an image search backend.
type SearchProvider interface {
    Search(ctx context.Context, query string, opts SearchOpts) ([]ImageCandidate, error)
    Name() string
}
```

### Key Types

```go
// ClassificationResult holds a classification decision with confidence.
type ClassificationResult struct {
    Class      string  // PHOTO, STOCK, REJECT, SCREENSHOT, ILLUSTRATION, MAP, or ""
    Confidence float64 // 0.0–1.0; 0 if not provided
}

// ClassificationEvent is emitted by the audit log callback.
type ClassificationEvent struct {
    URL        string  // image URL
    Class      string  // classification result
    Confidence float64 // 0.0–1.0
    Source     string  // "llm" or "prefilter"
}

// SearchOpts configures image search behavior.
type SearchOpts struct {
    PageNumber int           // SearXNG page number (default: 1)
    Engines    []string      // SearXNG engines (default: all)
    Timeout    time.Duration // search timeout (default: 15s)
}
```

### Methods on Config

| Method | Description |
|--------|-------------|
| `SearchImages(ctx, query, maxResults)` | Search, filter, validate, dedup, assess license, classify — returns `[]ImageCandidate` |
| `SearchImagesWithOpts(ctx, query, maxResults, opts)` | Same with pagination, engine selection, custom timeout |
| `ClassifyImageFull(ctx, imageURL)` | Classify image via LLM — returns `ClassificationResult` with class + confidence |
| `ClassifyImage(ctx, imageURL)` | Classify image — returns class string (`"PHOTO"`, `"STOCK"`, etc.) |
| `IsRealPhoto(ctx, imageURL)` | Returns `true` if class is `"PHOTO"` or `""` (graceful degradation) |
| `AssessLicense(cand, meta)` | Composite license verdict combining domain, metadata, and CC signals — returns `LicenseAssessment` |
| `ValidateImageURL(ctx, rawURL)` | Check HTTP status, content type, and minimum width |
| `Download(ctx, url, opts)` | Download image bytes with stealth fallback |

### Standalone Functions

| Function | Description |
|----------|-------------|
| `PreClassify(candidate)` | Cost-tier routing: returns `(class, skip)` for heuristic pre-filter |
| `ParseClassificationResult(resp)` | Parse `"CLASS 0.95"` LLM response into `ClassificationResult` |
| `ParseVisionResponse(resp)` | *(Deprecated)* Legacy 3-class parser — use `ParseClassificationResult` |
| `CheckLicense(imageURL, sourceURL)` | Classify license: `LicenseSafe`, `LicenseUnknown`, or `LicenseBlocked` |
| `CheckLicenseWith(imageURL, sourceURL, extraBlocked, extraSafe)` | Extended domain check with custom domain lists |
| `ExtractImageMetadata(data)` | Extract IPTC/EXIF/XMP rights metadata from image bytes |
| `IsStockByMetadata(meta)` | Detect stock agency fingerprints in image metadata |
| `IsCCByMetadata(meta)` | Detect Creative Commons license in image metadata |
| `ExtractCCLicense(html)` | Scan HTML for CC license URLs (`rel="license"`, CC links) |
| `IsCCLicenseURL(url)` | Check if a URL is a Creative Commons license |
| `IsLogoOrBanner(lowerURL)` | Detect logo/banner URL patterns |
| `BuildImageQuery(title, city)` | Build search query from title (strips stop words, appends city) |
| `ExtractOGImageURL(html)` | Extract `og:image` URL from HTML |
| `EncodeDataURL(data, mime)` | Create `data:` URI from bytes |

## Search Providers

| Provider | Source | License | API Key |
|----------|--------|---------|---------|
| `SearXNGProvider` | Self-hosted SearXNG meta-search | Mixed (depends on engine) | No |
| `OpenverseProvider` | WordPress Openverse (842M+ images) | CC / Public Domain only | No |

Custom providers implement the `SearchProvider` interface.

## License Lists

**Blocked** (25+ domains): Shutterstock, Getty Images, iStock, Adobe Stock, Depositphotos, Dreamstime, 123RF, Alamy, BigStock, Stocksy, EyeEm, Pond5, Freepik, Canva, and more.

**Safe** (11 domains): Unsplash, Pexels, Pixabay, Wikimedia Commons, Flickr, RawPixel, StockSnap, Burst (Shopify), Kaboompics, PicJumbo.

## Classification

The built-in `DefaultVisionPrompt` instructs the LLM to classify images into 6 categories:

- **PHOTO** — real photograph (small photographer watermarks are OK)
- **STOCK** — photograph with stock agency watermarks (Shutterstock, Getty, etc.)
- **REJECT** — banner, ad, promotional graphic, large text overlay, collage, meme
- **SCREENSHOT** — screenshot of a website, app, or software interface
- **ILLUSTRATION** — drawing, painting, digital art, cartoon, vector graphic
- **MAP** — map, satellite view, floor plan, diagram

Each response includes a confidence score (0.0–1.0). Consumers can override the prompt via `Config.VisionPrompt` for any classification scheme.

Classification uses graceful degradation: if the classifier is nil, unavailable, or returns an error, images are accepted by default.

## License Intelligence

Beyond domain-based heuristics, go-imagefy extracts embedded image metadata and HTML signals for license detection:

- **Image metadata extraction** — `ExtractImageMetadata()` reads IPTC (`Copyright`, `Credit`, `Byline`, `Source`), EXIF (`Copyright`, `Artist`), and XMP rights fields (`WebStatement`, `UsageTerms`, `License`, `Marked`) from image bytes via [`bep/imagemeta`](https://github.com/bep/imagemeta). Parsed once per image alongside perceptual hashing.
- **Stock agency detection** — `IsStockByMetadata()` scans metadata fields for stock agency fingerprints (Shutterstock, Getty, Alamy, Adobe Stock, etc.). Catches CDN-hosted stock images that pass domain checks.
- **Creative Commons from metadata** — `IsCCByMetadata()` detects CC license URLs in XMP rights fields. Images with CC metadata are promoted to `LicenseSafe`.
- **HTML CC scanning** — `ExtractCCLicense()` finds `rel="license"` links and `creativecommons.org/licenses/` URLs in HTML pages. Zero-cost signal available during OG image extraction.
- **Configurable domain lists** — `Config.ExtraBlockedDomains` and `Config.ExtraSafeDomains` let consumers extend the built-in 25+/11 domain lists without forking. `CheckLicenseWith()` provides ad-hoc domain checking.
- **Transparent assessment** — `Config.AssessLicense()` combines domain, metadata stock, and metadata CC signals into a `LicenseAssessment` with a list of human-readable signals explaining the decision. Blocked always takes precedence over safe.

### Metadata-aware configuration

```go
cfg := &imagefy.Config{
    Providers: []imagefy.SearchProvider{
        &imagefy.SearXNGProvider{URL: "http://localhost:8888"},
    },
    Classifier:          myLLM,
    ExtraBlockedDomains: []string{"mystock.internal.com"},
    ExtraSafeDomains:    []string{"ourarchive.org"},
}
// Pipeline now: search -> validate -> dedup -> metadata extract -> license assess -> LLM classify
results := cfg.SearchImages(ctx, "Hermitage Museum", 5)
```

## Development

```bash
make test     # go test -race -count=1 ./...
make lint     # golangci-lint run ./...
make build    # go build ./...
```

CI runs on Go 1.24 and 1.25 with golangci-lint v2.

## License

MIT
