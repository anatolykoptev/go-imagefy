# go-imagefy Architecture

> Last updated: 2026-03-10

## Overview

go-imagefy is a Go library (not a service) that acts as the **single gateway** for all image acquisition in editorial pipelines. Every image — whether from web search, OG tags, WordPress media library, or user-supplied URL — passes through the same filter pipeline before reaching the consumer.

**Consumer:** go-wp (WordPress MCP server on `:8894`)

## System Context

```
go-wp (consumer)
  │
  ├─ FindImages(query, pageURL, external)     ← unified entry point
  │   └─ go-imagefy (library, this repo)
  │       ├─ Search providers (parallel)
  │       │   ├─ OxBrowserProvider → ox-browser :8901 (Bing+DDG+Yandex)
  │       │   ├─ OpenverseProvider → openverse.org API (842M+ CC images)
  │       │   ├─ PexelsProvider → pexels.com API (stock, free license)
  │       │   └─ OGImageProvider → fetches og:image from source page
  │       │
  │       ├─ External candidates (WP media library via SSH/WP-CLI)
  │       │
  │       └─ 7-stage filter pipeline
  │           └─ validated []ImageCandidate
  │
  ├─ ValidateImageURL(url)                    ← single-image probe
  ├─ IsRealPhoto(url)                         ← LLM vision check
  └─ SearchImages(query, max)                 ← legacy entry point
```

## Entry Points

| Method | Purpose | Used by |
|--------|---------|---------|
| `FindImages(ctx, FindOpts)` | **Primary.** Unified search + external + OG + filter pipeline | go-wp `resolve`, `prepare` |
| `SearchImages(ctx, query, max)` | Legacy search-only entry point | — |
| `SearchImagesWithOpts(ctx, query, max, opts)` | Search with pagination/timeout | — |
| `ValidateImageURL(ctx, url)` | HTTP probe: status, content-type, width ≥ 880px | go-wp candidate validation |
| `ValidateCandidates(ctx, candidates, max)` | Filter pipeline for external candidates | — |
| `IsRealPhoto(ctx, url)` | LLM classification: is it a real photo? | go-wp candidate check |
| `ClassifyImageFull(ctx, url)` | Full LLM classification with confidence | — |

## Search Providers

All providers implement `SearchProvider` interface and run **in parallel** via `gatherCandidates()`.

| Provider | Backend | License | Source |
|----------|---------|---------|--------|
| `OxBrowserProvider` | ox-browser Rust service (`:8901`) | Mixed | Bing + DDG + Yandex with CF bypass |
| `OpenverseProvider` | openverse.org REST API | CC / Public Domain | 842M+ images, all `LicenseSafe` |
| `PexelsProvider` | pexels.com REST API | Pexels License (free) | High-quality stock, needs `PEXELS_API_KEY` |
| `OGImageProvider` | Direct HTTP fetch | Unknown | Extracts `og:image` from `SearchOpts.PageURL` |
| `SearXNGProvider` | Self-hosted SearXNG | Mixed | Legacy, replaced by OxBrowser |
| `DDGImageProvider` | DuckDuckGo direct | Mixed | Available as fallback, not used in go-wp |
| `FallbackProvider` | Orchestrator | — | Tries providers in order until one succeeds |

**go-wp provider chain** (configured in `imageadapter/adapter.go`):
OxBrowser → Openverse → Pexels → OGImage

## Filter Pipeline (7 stages)

Every candidate passes through `validateOne()` in `search.go`:

```
1. URL patterns        → IsLogoOrBanner() rejects logos, banners, sprites, favicons
2. HTTP probe          → ValidateImageURL(): status 200, image/* content-type, width ≥ 880px
                         Uses cfg.StealthClient or cfg.HTTPClient (proxy-aware)
3. Domain license      → ExtraBlockedDomains pre-check (avoids download for known-blocked)
4. Download            → Single download for all subsequent checks (dedup + metadata + LLM)
5. Perceptual dedup    → dHash via goimagehash, Hamming distance < 10 threshold
6. Metadata + license  → ExtractImageMetadata (IPTC/EXIF/XMP) → AssessLicense()
                         Stock agency detection (Getty, Shutterstock, etc.)
                         CC license detection from XMP rights fields
                         Blocked always takes precedence over Safe
7. LLM Vision          → ClassifyImageFull() for LicenseUnknown candidates only
                         6-class: PHOTO | STOCK | REJECT | SCREENSHOT | ILLUSTRATION | MAP
                         LicenseSafe candidates skip LLM (cost-tier routing)
```

## File Map

```
Layer 0 — Pure logic (no I/O)
├── license.go          25+ blocked domains, 11 safe domains, CheckLicense/CheckLicenseWith
├── metadata.go         ExtractImageMetadata (IPTC/EXIF/XMP via bep/imagemeta)
├── assessment.go       AssessLicense — composite domain + metadata + CC verdict
├── cclicense.go        ExtractCCLicense, IsCCLicenseURL — HTML CC scanning
├── patterns.go         IsLogoOrBanner — URL pattern detection
├── query.go            BuildImageQuery — Russian stop word filtering
├── helpers.go          ExtractOGImageURL, EncodeDataURL, EncodeBase64
├── prefilter.go        PreClassify — cost-tier routing (safe sources skip LLM)
└── imagefy.go          Config, interfaces (Cache, Classifier), SearchOpts, defaults()

Layer 1 — HTTP (no external services beyond target URLs)
├── download.go         Download with stealth client fallback
├── validate.go         ValidateImageURL + validationClient (proxy-aware)
└── dedup.go            Perceptual hash dedup (dHash + Hamming distance)

Layer 2 — Orchestration (uses interfaces, runs providers)
├── search.go           SearchImages, gatherCandidates, validateCandidates, validateOne
├── find.go             FindImages — unified entry point (search + OG + external)
├── classify.go         ClassifyImage, ClassifyImageFull, IsRealPhoto
├── provider.go         SearchProvider interface, SearXNGProvider
├── openverse.go        OpenverseProvider (Openverse API client)
├── pexels.go           PexelsProvider (Pexels API client)
├── provider_ox.go      OxBrowserProvider (ox-browser REST client)
├── provider_og.go      OGImageProvider (og:image extraction as provider)
├── provider_ddg.go     DDGImageProvider (DuckDuckGo direct, fallback)
└── orchestrator.go     FallbackProvider (try providers in sequence)
```

## Dependency Injection

```go
type Config struct {
    // Required for full pipeline
    Cache         Cache            // Redis, sync.Map, etc.
    Classifier    Classifier       // Multimodal LLM (Gemini, GPT-4V, etc.)

    // HTTP clients (proxy-aware)
    StealthClient *http.Client     // TLS-fingerprinted, preferred for validation/download
    HTTPClient    *http.Client     // Standard client with proxy, fallback

    // Search configuration
    Providers     []SearchProvider // Pluggable search backends
    MinImageWidth int              // Default: 880px

    // Domain customization
    ExtraBlockedDomains []string   // Additional stock domains to block
    ExtraSafeDomains    []string   // Additional free-use domains

    // Observability
    OnImageSearch    func()                    // Search counter
    OnClassification func(ClassificationEvent) // Audit log
    OnPanic          func(tag string, r any)   // Panic recovery
}
```

**go-wp wiring** (`internal/imageadapter/`): bridges go-engine Cache/LLM to imagefy interfaces.

## Key Design Decisions

1. **Library, not service** — go-imagefy is a Go module imported by consumers. No RPC, no containers, no ports.

2. **Single download** — each candidate is downloaded once; the bytes are reused for dedup hash, metadata extraction, and LLM classification (base64 data URI).

3. **Proxy-first** — all outbound HTTP (validation, download, provider calls) goes through injected `StealthClient`/`HTTPClient` with Webshare proxy pool. Direct requests = bug.

4. **Cost-tier routing** — images from known-safe sources (Openverse, Unsplash, Pixabay) skip LLM classification entirely, reducing API costs.

5. **Graceful degradation** — if Classifier is nil or returns error, images are accepted by default. If a provider fails, others continue.

6. **Zero CGO** — all dependencies are pure Go. No libvips, no TensorFlow.

## Constants

| Constant | Value | Location |
|----------|-------|----------|
| `searchTimeout` | 30s | search.go |
| `defaultTimeout` | 10s | download.go |
| `DefaultMinImageWidth` | 880px | imagefy.go |
| `validationSemaphore` | 3 | search.go |
| `decodeLimit` | 256KB | validate.go |
| `maxRedirects` | 3 | validate.go |

## Test Coverage

364 tests across 21 test files. Key test areas:
- Provider mocking via `httptest.Server` and `roundTripFunc`
- License/metadata extraction with real IPTC/EXIF fixtures
- Filter pipeline integration tests
- Perceptual dedup with generated JPEG images
- Classification parsing and audit log callbacks
