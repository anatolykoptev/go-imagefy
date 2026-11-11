# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versions are `v0.x`; entries are
summarised from the tags.

## [Unreleased]

### Changed
- Licence changed from MIT to Apache-2.0 from this version on.
- Docs-only pull requests skip the heavy CI jobs.
- Repository documents brought up to the fleet standard.

## [0.13.0] - 2026-07-07

### Added
- `ScreenImage` and `ScreenImageURL`: the single-image reject gate, reusable outside the pipeline.

## [0.12.0] - 2026-07-07

### Added
- Deterministic flat/non-photographic image reject gate.

## [0.11.0] - 2026-07-07

### Added
- Deterministic placeholder-image reject gate at ingest (perceptual-hash blocklist).

## [0.10.0] - 2026-06-01

### Changed
- Bump go-engine to v1.27.0 and go-stealth to v1.15.0.

## [0.9.0] - 2026-06-01

### Changed
- Import the image search engines from go-engine instead of go-stealth.

## [0.8.0] - 2026-05-29

### Added
- `ContentImageProvider` picks the real content image from a page and rejects placeholders.

## [0.7.0] - 2026-03-31

### Changed
- Validation uses direct HTTP; the TLS-fingerprinted proxy client is only a download fallback.

## [0.6.0] - 2026-03-31

### Changed
- Remove `OxBrowserProvider`; `NativeImageProvider` replaces it.

## [0.5.0] - 2026-03-17

### Added
- Reverse stock-image check integrated into the validation pipeline.

## [0.4.0] - 2026-03-09

### Changed
- `GOWORK=off` in the build tooling.

## [0.3.0] - 2026-03-06

### Added
- License intelligence: image metadata extraction (IPTC, EXIF, XMP), composite `AssessLicense`,
  Creative Commons detection in metadata and HTML, perceptual-hash dedup, configurable domain lists.

## [0.2.0] - 2026-03-04

### Added
- `PexelsProvider`.

## [0.1.0] - 2026-02-27

### Added
- First release: SearXNG and Openverse providers, license check by domain, URL validation, LLM
  classification with confidence, caching and CI.

[Unreleased]: https://github.com/anatolykoptev/go-imagefy/compare/v0.13.0...HEAD
