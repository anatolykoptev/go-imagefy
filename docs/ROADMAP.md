# Roadmap

> See [COMPARE.md](COMPARE.md) for full competitive landscape analysis.

## Completed

### v0.1.0 — Extraction from go-wp (done)

- Extracted image processing code from `go-wp/internal/images/` into standalone module
- Three-layer architecture: L0 pure logic, L1 HTTP, L2 interfaces
- Core API: `SearchImages`, `ClassifyImage`, `IsRealPhoto`, `ValidateImageURL`, `Download`
- License checking: 25+ blocked stock domains, 11 safe/free domains
- URL pattern filtering: logos, banners, sprites, favicons
- LLM-based vision classification with `VisionPrompt` (PHOTO/STOCK/REJECT)
- Search query builder with Russian stop word filtering
- OG image extraction from HTML
- Stealth client fallback for anti-bot protection
- Dependency injection via `Cache` and `Classifier` interfaces
- 29 unit tests, golangci-lint v2 clean
- CI: GitHub Actions (Go 1.24 + 1.25, golangci-lint v2.10.1)

### v0.1.1 — Stabilization (done)

- Bug fixes and API cleanup
- go-wp adapter (`internal/imageadapter/adapter.go`) bridges `engine.*` to `imagefy.*`

---

## Phase 1 — Enhanced Search (done)

**Goal:** improve search quality, eliminate duplicate results, expand coverage.

- [x] **Perceptual hash dedup** — integrated [`corona10/goimagehash`](https://github.com/corona10/goimagehash) dHash with Hamming distance < 10 threshold. Eliminates redundant LLM calls for visually identical images.
- [x] **SearXNG pagination** — `SearchOpts.PageNumber` parameter, `&pageno=N` in requests.
- [x] **Openverse as search backend** — `OpenverseProvider` queries 842M+ CC/PD images. All results `LicenseSafe`.
- [x] **SearchProvider interface** — pluggable backends via `SearchProvider` interface. Ships with `SearXNGProvider` and `OpenverseProvider`.
- [x] **Configurable search timeout** — `SearchOpts.Timeout` overrides default 15s.
- [ ] **Image relevance scoring** (title match, source authority, resolution) — deferred.
- [x] **SearXNG engine selection** — `SearchOpts.Engines` controls which backends SearXNG queries.

## Phase 2 — Advanced Classification (done)

**Goal:** reduce false positives/negatives, cut LLM costs.

- [x] **Multi-class classification** — added SCREENSHOT, ILLUSTRATION, MAP categories to `DefaultVisionPrompt`. 6-class taxonomy with `ClassificationResult` type.
- [x] **Confidence scores** — LLM responses now parsed as "CLASS 0.95" format via `ParseClassificationResult`. `ClassificationResult.Confidence` field (0.0–1.0).
- [x] **NSFW detection via prompt** — enabled via `Config.VisionPrompt` custom prompt support. Consumers override the prompt to add NSFW/violence categories.
- [ ] **Batch classification** — deferred (requires `Classifier` interface change; current per-image flow is sufficient).
- [x] **Custom prompt support** — `Config.VisionPrompt` field overrides `DefaultVisionPrompt` for domain-specific classification.
- [x] **Classification audit log** — `Config.OnClassification` callback receives `ClassificationEvent` for every decision (both LLM and pre-filter), with URL, class, confidence, and source.
- [x] **Cost-tier routing** — `PreClassify` pre-filter skips LLM for `LicenseSafe` sources (Openverse, Unsplash, Pixabay). Auto-accepts as PHOTO without API call.

## Phase 2.5 — ox-browser Integration + Parallel Search

**Goal:** Replace Go-based scrapers with ox-browser Rust backend for image search,
add parallel provider execution and orchestration.

- [ ] **OxBrowserProvider** — new `SearchProvider` that calls ox-browser `POST /images/search` REST endpoint. Delegates all scraping (Bing, DDG, Yandex, Brave) to Rust with wreq+BoringSSL TLS fingerprint, proxy rotation, CF bypass.
- [ ] **Parallel gatherCandidates** — replace sequential provider loop with `sync.WaitGroup` + goroutine-per-provider. Context-aware early cancel when enough candidates collected.
- [ ] **FallbackProvider** — orchestrator that tries providers in order until one succeeds. For API fallback (Openverse down → Pexels).
- [ ] **Connect PexelsProvider** — already implemented, just needs wiring in consumers via `PEXELS_API_KEY` env var.
- [ ] **Preserve DDGImageProvider** as fallback — when ox-browser is unavailable, fall back to existing Go DDG provider.

**Architecture:**
- ox-browser (Rust) handles all scraping: TLS fingerprint, anti-bot, proxy rotation, CF bypass, HTML/JSON parsing
- go-imagefy handles validation: license check, perceptual dedup, metadata extraction, LLM classification
- Clean separation: Rust scrapes, Go validates

**Consumer changes (go-wp):**
- `imageadapter/adapter.go` — switch primary provider to OxBrowserProvider, add Pexels
- `media/upload.go` — fix `http.DefaultClient` → proxied stealth client

**Depends on:** ox-browser Phase 4.6 (image search crate)

## Phase 3 — Image Processing

**Goal:** basic image manipulation for editorial pipelines.

Recommended library: [`disintegration/imaging`](https://github.com/disintegration/imaging) (5678 stars, pure Go, no CGO). Covers resize, crop, thumbnail. WebP/AVIF encoding requires CGO (`chai2010/webp` or libvips) — defer to consumers.

- [ ] **Resize/crop** to standard editorial dimensions via `disintegration/imaging`
- [ ] **Thumbnail generation** — `imaging.Fill()` with center crop
- [ ] **BlurHash placeholder** — generate compact BlurHash strings for progressive loading. Available in [`evanoberholster/imagemeta`](https://github.com/evanoberholster/imagemeta) (132 stars).
- [ ] **Image quality scoring** — blur detection, exposure analysis
- [ ] **Format conversion** — JPEG/PNG encode (pure Go); WebP/AVIF via optional consumer-injected encoder interface

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

## Phase 5 — Performance & Observability

**Goal:** production-grade reliability and monitoring.

- [ ] **OpenTelemetry tracing** for search/validate/classify pipeline
- [ ] **Prometheus metrics** — search latency, classification distribution, cache hit rate, dedup savings
- [ ] **Circuit breaker** for SearXNG and classifier backends
- [ ] **Connection pooling optimization**
- [ ] **Benchmark suite** for hot paths. Consider `sync.Pool` for decoded `image.Image` objects (pattern from `goimagehash`'s `pixelPool64`).

## Phase 6 — Content Provenance (strategic)

**Goal:** cryptographic image provenance verification.

- [ ] **C2PA Content Credentials** — read C2PA manifests (ISO standard, v2.3) to verify image provenance. Adopted by Adobe, Google, Microsoft, BBC, Meta, Sony, Nikon, Leica. Can verify real photographs, detect AI-generated images, and identify source agencies. **No Go SDK exists** — would require CGO bindings to [`c2pa-c`](https://github.com/contentauth/c2pa-c) or a pure Go parser. First-mover advantage in Go ecosystem.
- [ ] **AI-generated image detection** — combine C2PA credentials + IPTC 2025.1 AI fields + Sightengine-style heuristics. Increasingly important as AI-generated stock proliferates.
- [ ] **Reverse image search** for provenance verification

## Ideas (unscheduled)

- **Watermark detection without LLM** — LAION-AI/watermark-detection model via ONNX runtime, or perceptual hash comparison against known watermark patterns
- **Face detection / privacy blur**
- **Image CDN integration** (Cloudflare Images, imgproxy)
- **CLI tool** for batch image validation
- **MCP server wrapper** for direct integration with AI agents
- **Dedicated watermark model** — train via Clarifai or similar on stock watermark dataset, serve via ONNX. Cheaper and faster than LLM for STOCK detection specifically
- **Direct stock photo API clients** — thin Pexels/Pixabay wrappers (~100 lines each) for guaranteed license-clean results
