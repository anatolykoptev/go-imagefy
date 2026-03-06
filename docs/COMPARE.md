# Competitive Landscape & Ecosystem Analysis

> Last updated: 2026-02-27

## Overview

go-imagefy occupies a unique niche in the Go ecosystem: an embeddable editorial image pipeline that combines search, license filtering, LLM-based classification, URL validation, and download with stealth fallback. No existing Go library provides this combination. Competition comes primarily from commercial SaaS APIs, Python packages, and fragmented Go libraries that each cover a single aspect.

---

## 1. Go Libraries

### 1.1 Image Search

| Library | Stars | Last Active | What It Does | Relevance |
|---------|-------|-------------|--------------|-----------|
| [morikuni/go-searxng](https://github.com/morikuni/go-searxng) | ~0 | 2026 | Typed Go client for SearXNG API | Polymorphic `SearchResult` interface; typed `ImageResult`; engine selection constants. go-imagefy does the same manually in `search.go` — adopting the typed approach or just adding `&pageno=N` for pagination is the main takeaway. |
| [hbagdi/go-unsplash](https://github.com/hbagdi/go-unsplash) | 77 | 2025-12 | Full Unsplash API v1 client | Clean pagination (Link header), rate limit tracking. Requires API key — conflicts with go-imagefy's zero-config approach. Better as an optional consumer-side integration via a `SearchProvider` interface. |

**No quality Go clients exist for Pexels or Pixabay** — their REST APIs are simple enough for ~100 lines each.

### 1.2 Perceptual Hashing / Deduplication

| Library | Stars | Last Active | Approach | Notes |
|---------|-------|-------------|----------|-------|
| [corona10/goimagehash](https://github.com/corona10/goimagehash) | 830 | 2024-01 | pHash/dHash/aHash + Hamming distance | Most popular; DCT-based; object pooling; extended 256-bit hashes. Pure Go, zero CGO. |
| [evanoberholster/imagemeta](https://github.com/evanoberholster/imagemeta) | 132 | 2026-02 | pHash + BlurHash alongside EXIF | Zero-allocation pHash adapted from goimagehash; also has BlurHash for placeholders. Actively maintained. |
| [azr/phash](https://github.com/azr/phash) | 58 | 2024-01 | Minimal pHash | Simple API but fewer features than goimagehash. |
| [ajdnik/imghash](https://github.com/ajdnik/imghash) | 31 | 2026-02 | aHash/dHash/pHash/mHash | Actively maintained, most recently updated. |
| [Nr90/imgsim](https://github.com/Nr90/imgsim) | 75 | 2018 | aHash + dHash | Marked unstable; abandoned. |

**Winner:** `corona10/goimagehash` (830 stars, battle-tested). Alternative: `evanoberholster/imagemeta` if also using it for EXIF (kills two birds).

### 1.3 EXIF / Metadata Extraction

| Library | Stars | Last Active | Approach | Notes |
|---------|-------|-------------|----------|-------|
| [evanoberholster/imagemeta](https://github.com/evanoberholster/imagemeta) | 132 | 2026-02 | EXIF + XMP for JPEG/HEIC/AVIF/TIFF/Raw | Copyright field, Artist, GPS; built-in pHash + BlurHash. Pure Go. |
| [bep/imagemeta](https://github.com/bep/imagemeta) | ~new | 2026 | EXIF/IPTC/XMP for JPEG/TIFF/PNG/WebP/HEIF/AVIF | Pure Go, read-only. By the Hugo author. |
| [rwcarlsen/goexif](https://github.com/rwcarlsen/goexif) | 670 | 2022-11 | EXIF/TIFF from JPEG | Alpha stage, unmaintained since 2022. |
| [dsoprea/go-exif](https://github.com/dsoprea/go-exif) | 577 | 2024-04 | Full EXIF read/write | Pure Go, standards-driven. |
| [barasher/go-exiftool](https://github.com/barasher/go-exiftool) | 293 | 2025-08 | Wrapper for `exiftool` binary | Full EXIF/IPTC/XMP read/write. Requires external binary. |
| [trimmer-io/go-xmp](https://github.com/trimmer-io/go-xmp) | 68 | 2021-11 | Native XMP SDK | `xmpRights` schema: Owner, UsageTerms, WebStatement, License, CopyrightStatus. |

**Winner:** `evanoberholster/imagemeta` (active, EXIF+XMP+pHash in one package) + `trimmer-io/go-xmp` for deep Creative Commons parsing.

### 1.4 Image Processing

| Library | Stars | Last Active | Approach | CGO? | Notes |
|---------|-------|-------------|----------|------|-------|
| [disintegration/imaging](https://github.com/disintegration/imaging) | 5678 | 2023-09 | Resize/crop/rotate/blur | No | Pure Go. Lanczos, CatmullRom. Encodes JPEG/PNG/GIF/BMP/TIFF. No WebP/AVIF encode. |
| [imgproxy/imgproxy](https://github.com/imgproxy/imgproxy) | 10469 | 2026-02 | Server-mode processor | Yes (libvips) | Extremely fast; AVIF/JXL. Designed as a service, not a library. |
| [cshum/imagor](https://github.com/cshum/imagor) | 3907 | 2026-02 | Server + library | Yes (libvips) | Clean `Processor` interface; streaming pipeline; Docker-first. |
| [h2non/bimg](https://github.com/h2non/bimg) | 2986 | 2025-01 | Programmatic API | Yes (libvips) | Crop/resize/rotate/watermark; JPEG/PNG/WebP/AVIF native. |
| [Pixboost/transformimgs](https://github.com/Pixboost/transformimgs) | 289 | 2025-11 | Image CDN | Yes | AVIF/WebP auto-negotiation via Accept header. |

**Winner for go-imagefy:** `disintegration/imaging` (pure Go, no CGO, 5678 stars). WebP/AVIF encoding deferred to consumers.

### 1.5 NSFW / Content Moderation

| Library | Stars | Last Active | Approach | Notes |
|---------|-------|-------------|----------|-------|
| [ccuetoh/nsfw](https://github.com/ccuetoh/nsfw) | ~small | Recent | TensorFlow-based 5-class NSFW | Requires TensorFlow C library (heavy CGO). Auto-downloads model at runtime (security risk). |

**Assessment:** The Go ecosystem for NSFW detection is very thin. `ccuetoh/nsfw` requires TensorFlow C bindings, which destroys go-imagefy's zero-CGO philosophy. Better approach: extend the existing `Classifier` interface with NSFW-aware prompts, or delegate to external APIs (Sightengine, AWS Rekognition).

### 1.6 Watermark Detection

| Library | Stars | Last Active | Approach | Notes |
|---------|-------|-------------|----------|-------|
| [saifabid/Watermark-Detection](https://github.com/saifabid/Watermark-Detection) | 7 | 2014 | Crop + Tesseract OCR | Abandoned; requires gosseract (CGO). |

**Assessment:** No viable Go libraries exist. The Python ecosystem is better: [LAION-AI/watermark-detection](https://github.com/LAION-AI/watermark-detection) (trained on real stock watermarks) and [boomb0om/watermark-detection](https://github.com/boomb0om/watermark-detection) (pre-trained weights on HuggingFace). go-imagefy's LLM approach is currently the most practical solution for Go.

---

## 2. Commercial / SaaS Tools

### 2.1 Copyright Enforcement & License Compliance

| Service | What It Does | Pricing | Key Insight for go-imagefy |
|---------|-------------|---------|----------------------------|
| [Pixsy](https://pixsy.com) | Reverse image search + legal enforcement. Monitors 150M+ images daily. | Free monitoring (500 images); 50% of recovered fees. | Solves the *opposite* problem (protecting photographers). Domain-matching heuristics are similar to `CheckLicense`. Takeaway: dynamic, regularly-updated domain database vs. hardcoded list. |
| [Copytrack](https://copytrack.com) | Image theft detection + post-licensing. | Free monitoring (500 images); 45% of recovered fees. | "Post-licensing" concept — rather than binary block/allow, a third category: "licensable." |
| [PicDefense](https://picdefense.io) | Website image audit. EXIF analysis, stock DB matching, "PicRisk" score per image. | From $20/100 scans. WordPress plugin. | **PicRisk score** is exactly the graduated risk assessment go-imagefy could adopt instead of binary Safe/Unknown/Blocked. EXIF analysis for stock fingerprints is another layer go-imagefy lacks. |
| [Fair Licensing](https://fairlicensing.com) | Case management for photographers; evidence collection + billing. | Not public. | "Friendly" enforcement model. |

### 2.2 Image Moderation APIs

| Service | What It Does | Pricing | Key Insight for go-imagefy |
|---------|-------------|---------|----------------------------|
| [Google Cloud Vision](https://cloud.google.com/vision) | SafeSearch (adult/spoof/medical/violence/racy) + labels + OCR + logo + landmark detection. | 1K/month free; then $1.50/1K units. | Label detection ("stock photography", "watermark", "illustration") could supplement LLM classification at 1/10th the cost. |
| [AWS Rekognition](https://aws.amazon.com/rekognition) | Content moderation (26+ labels, 3-tier taxonomy) + face + object + text-in-image. | ~$1/1K images. 12-month free tier. | Three-tier moderation taxonomy (Violence > Weapons > Guns) more nuanced than PHOTO/STOCK/REJECT. Cheapest per-image. |
| [Sightengine](https://sightengine.com) | NSFW, violence, hate, **AI-generated image detection**, deepfake detection, text-in-image. | From $29/month. Free tier available. | **AI-generated image detection** is unique. Text-in-image detection could help with watermark identification. |
| [Clarifai](https://clarifai.com) | Pre-built moderation + custom model training + visual search. | $30/month (30K calls). | Custom model training — purpose-built "stock watermark" detector would be faster/cheaper than LLM. Visual search for duplicate detection. |

### 2.3 Free Image Aggregators & APIs

| Service | Library Size | API | Key Insight for go-imagefy |
|---------|-------------|-----|----------------------------|
| [Openverse](https://openverse.org) (WordPress Foundation) | 842M+ images from 54 providers | REST, free, open source | License metadata is first-class. Returns only openly-licensed images — eliminates post-hoc filtering entirely. Go SDK does not exist but API is simple. **Strong alternative to SearXNG for license-safe search.** |
| [Unsplash](https://unsplash.com/documentation) | 3M+ photos | REST, free (50-5000 req/hr) | Unsplash License (free commercial, no attribution). Go SDK: `hbagdi/go-unsplash`. |
| [Pexels](https://pexels.com/api) | 1M+ photos + videos | REST, free (200 req/hr) | No hotlinking requirement. No Go SDK. |
| [Pixabay](https://pixabay.com/api) | 5M+ images + videos | REST, free (100 req/min) | Largest free library. Illustrations + vectors. No Go SDK. |

---

## 3. Open-Source Projects (Non-Go)

### 3.1 Watermark Detection (Python)

| Project | Stars | Approach | Key Insight |
|---------|-------|----------|-------------|
| [LAION-AI/watermark-detection](https://github.com/LAION-AI/watermark-detection) | ~active | Training dataset + classifier for stock watermarks | Purpose-built for the exact STOCK detection go-imagefy does with LLM. Could be served via ONNX runtime in Go. |
| [boomb0om/watermark-detection](https://github.com/boomb0om/watermark-detection) | ~active | PyTorch binary classifier (watermarked vs clean) | Pre-trained weights on HuggingFace. Simple binary classification matches STOCK vs PHOTO. |
| [rohitrango/automatic-watermark-detection](https://github.com/rohitrango/automatic-watermark-detection) | ~active | Edge detection + distance transforms | Locates watermark position (not just presence/absence). |

### 3.2 SearXNG Integration

| Project | Language | Approach |
|---------|----------|----------|
| LangChain SearXNG Wrapper | Python | Pluggable "tool" with category selection. Mirrors go-imagefy's architecture but with configurable engine selection. |
| liteLLM SearXNG | Python | LLM-augmented search with pre-configured Docker. |

---

## 4. Standards & Protocols

### 4.1 IPTC Photo Metadata (v2025.1)

The most widely adopted standard for embedded image metadata. Key fields for go-imagefy:
- `Credit` — identifies stock agency or photographer
- `CopyrightNotice` — legal copyright statement
- `WebStatement` — URL to full rights/license info
- `Licensor` — PLUS-compatible licensor details

**2025.1 additions:** AI Prompt Information, AI System Used, AI System Version — relevant for detecting AI-generated images.

**go-imagefy takeaway:** Reading IPTC from downloaded images adds a high-confidence signal. If JPEG contains `Credit: Getty Images`, that is definitive — no LLM needed. Pure Go via `bep/imagemeta`.

### 4.2 Creative Commons Machine-Readable (ccREL)

Encodes license info in RDFa (HTML) or XMP (files). Uses `rel="license"` microformat.

**go-imagefy takeaway:** When fetching a source page for OG image, also scan for `rel="license"` links and CC URLs (`creativecommons.org/licenses/`). Promote images from CC-licensed pages to `LicenseSafe` even if domain is not in the safe list.

### 4.3 C2PA (Content Authenticity Initiative)

Cryptographically signed "Content Credentials" recording provenance. ISO standard (2025). Adopted by Adobe, Google, Microsoft, BBC, Meta, Sony, Nikon, Leica.

- Current spec: C2PA 2.3 (2025)
- Official SDKs: Rust (reference), Python, C/C++, JavaScript, Android, iOS
- **No official Go SDK**

**go-imagefy takeaway:** C2PA credentials can verify real photographs and detect AI-generated images. No Go library exists yet — implementation would require CGO bindings to `c2pa-c` or a pure Go parser. Strategic long-term bet as adoption accelerates.

### 4.4 PLUS (Picture Licensing Universal System)

Standardized vocabulary for image license terms. Embedded in XMP metadata. Integrated with IPTC.

**go-imagefy takeaway:** PLUS metadata definitively identifies license terms. Limited adoption outside major agencies. Low priority until IPTC/XMP reading is implemented.

---

## 5. Feature Gap Analysis

> Updated after Phase 1 + Phase 2 completion.

| Capability | go-imagefy Today | Best in Ecosystem | Status |
|------------|-----------------|-------------------|--------|
| Stock domain filtering | 25+ hardcoded domains | PicDefense PicRisk scoring, dynamic DBs | Static list; no auto-updates, no risk scoring |
| Image classification | 6-class LLM (PHOTO/STOCK/REJECT/SCREENSHOT/ILLUSTRATION/MAP) + confidence scores | LAION-AI watermark model, Clarifai custom | **Phase 2** ~~3-class~~ → 6-class with confidence. LLM still slower than purpose-built models |
| Cost-tier routing | PreClassify skips LLM for LicenseSafe sources | Google Vision label pre-filter | **Phase 2** — heuristic pre-filter saves LLM costs for safe sources |
| Custom classification | Config.VisionPrompt override for any domain | Clarifai custom models | **Phase 2** — prompt-level customization (NSFW, e-commerce, etc.) |
| Classification audit | OnClassification callback with source tracking | Sightengine audit API | **Phase 2** — full audit trail for debugging |
| License detection | Domain-based heuristic only | IPTC/XMP metadata, CC URL scanning, PLUS | No metadata-based detection |
| Content moderation | Via custom VisionPrompt | Google Vision, AWS Rekognition, Sightengine | **Phase 2** — enabled via custom prompt support |
| AI-generated detection | Not addressed | Sightengine, C2PA credentials | Cannot flag AI-generated images |
| Search sources | SearXNG + Openverse (842M+) via SearchProvider interface | Openverse, Unsplash, Pexels direct APIs | **Phase 1** — multi-provider with pluggable interface |
| Provenance verification | None | C2PA Content Credentials | No cryptographic provenance |
| Metadata reading | None | bep/imagemeta, evanoberholster/imagemeta | Cannot read embedded rights data |
| CC license in HTML | Not checked | ccREL/RDFa scanning | OG extraction ignores license markup |
| Deduplication | dHash + Hamming distance via goimagehash | goimagehash (830 stars) | **Phase 1** — perceptual hash dedup integrated |
| Image processing | None | disintegration/imaging (5678 stars) | No resize/crop/thumbnail |
| Pagination | SearchOpts.PageNumber + Engines | SearXNG `&pageno=N` | **Phase 1** — pagination + engine selection |

---

## 6. Recommended Libraries to Integrate

### Add (pure Go, zero CGO)

| Library | Stars | Purpose | Phase |
|---------|-------|---------|-------|
| `corona10/goimagehash` | 830 | Perceptual hash dedup in search pipeline | Phase 1 |
| `evanoberholster/imagemeta` | 132 | EXIF/XMP + Copyright field + pHash | Phase 4 |
| `disintegration/imaging` | 5678 | Resize, crop, thumbnail generation | Phase 3 |
| `trimmer-io/go-xmp` | 68 | Creative Commons license parsing from XMP | Phase 4 |

### Skip (wrong fit for go-imagefy)

| Library | Reason |
|---------|--------|
| `morikuni/go-searxng` | 0 stars; go-imagefy's manual parsing is equally simple |
| `hbagdi/go-unsplash` | Requires API key; better as optional consumer integration |
| `ccuetoh/nsfw` | Requires TensorFlow C library (breaks zero-CGO) |
| `h2non/bimg` / `cshum/imagor` | Require libvips (CGO) |
| `rwcarlsen/goexif` | Alpha, unmaintained since 2022 |
| `barasher/go-exiftool` | Requires external `exiftool` binary |

---

## 7. Key Takeaways

1. **go-imagefy is unique** — no Go library combines search + license filtering + classification. The closest competition is Python packages and commercial APIs.

2. **Completed high-impact additions** (Phase 1 + Phase 2):
   - Perceptual hash dedup (`goimagehash`) — eliminates redundant LLM calls
   - Openverse as search backend — 842M pre-licensed images with `SearchProvider` interface
   - 6-class classification with confidence scores — SCREENSHOT, ILLUSTRATION, MAP now correctly rejected
   - Cost-tier routing — LicenseSafe images skip LLM entirely (zero API cost)
   - Custom prompts — any classification scheme without forking the library

3. **Next highest-impact additions** (remaining gaps):
   - IPTC/EXIF metadata reading — high-confidence license signal without LLM
   - CC license scanning in HTML — zero-cost license detection during OG extraction
   - Image processing (resize/crop/thumbnail) — editorial pipeline completeness

4. **LLM classification is a competitive advantage**, not a weakness — it is the most accurate stock watermark detector available without specialized ML models, and go-imagefy's `Classifier` interface makes it trivially swappable. Cost-tier routing mitigates the expense.

5. **Zero-CGO philosophy is a differentiator** — most image processing libraries require libvips or TensorFlow. go-imagefy should maintain this.

6. **C2PA is the strategic long-term bet** — as cameras and platforms adopt Content Credentials (ISO standard, backed by Adobe/Google/Microsoft), reading C2PA manifests will become essential for image provenance. No Go SDK exists yet — first-mover advantage opportunity.
