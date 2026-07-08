package imagefy

import (
	"context"
	"fmt"
	"image"
	"log/slog"

	"github.com/corona10/goimagehash"
)

// Classification reason labels emitted by screenDecodedImage — pulled out as
// constants (rather than repeated string literals) since the same reason
// string is asserted by production code (emitClassification's Source field),
// this package's own tests, and now ScreenImage's return value.
const (
	reasonPhashBlocklist = "phash_blocklist"
	reasonFlatImage      = "flat_image"
)

// ScreenImage runs the placeholder-blocklist gate (perceptual-hash match
// against known hotlink-protection / "image unavailable" graphics, see
// placeholder.go) and the flat/non-photographic detector (flatimage.go)
// against a single, already-decoded image. It is the single-image
// counterpart of the gates validateOne applies inside the search candidate
// pipeline (see rejectedByHash, which shares this exact logic via
// screenDecodedImage — one source of truth, no duplication).
//
// It deliberately does NOT run:
//   - perceptual dedup — dedupFilter is per-search-call state (identity vs.
//     other candidates in the SAME search), meaningless for one
//     caller-supplied image.
//   - license/reachability checks (ValidateImageURL, AssessLicense,
//     ReverseCheck, LLM vision classification) — a caller validating one
//     already-resolved URL runs those separately if it wants them.
//
// Graceful degradation: a nil img (e.g. a failed decode upstream) never
// rejects — reject=false, class="", reason="" — same posture as every other
// gate in this package.
func (cfg *Config) ScreenImage(img image.Image, sourceURL string) (reject bool, class, reason string) {
	placeholders := newPlaceholderMatcher(cfg.PlaceholderHashes)
	return cfg.screenDecodedImage(img, nil, sourceURL, placeholders)
}

// ScreenImageURL downloads and decodes url via the same download path the
// search pipeline uses (downloadForValidation — HTTPClient with StealthClient
// fallback), then runs ScreenImage against the decoded image. This is the
// one-call entry a consumer resolving a single caller-supplied image URL
// (rather than running the internal search-candidate pipeline) should use.
//
// Graceful degradation: a download or decode failure fails OPEN
// (reject=false, class="", reason="") with a descriptive error returned for
// the caller to log — matching the rest of this package's posture of never
// hard-failing on a network hiccup.
func (cfg *Config) ScreenImageURL(ctx context.Context, url string) (reject bool, class, reason string, err error) {
	data, _, img := cfg.downloadForValidation(ctx, url)
	if data == nil {
		return false, "", "", fmt.Errorf("imagefy: ScreenImageURL: download failed for %s", url)
	}
	if img == nil {
		return false, "", "", fmt.Errorf("imagefy: ScreenImageURL: decode failed for %s", url)
	}

	reject, class, reason = cfg.ScreenImage(img, url)
	return reject, class, reason, nil
}

// screenDecodedImage runs the placeholder-blocklist check followed by the
// flat/non-photographic detector against img — the single shared
// implementation behind both the public ScreenImage and validateOne's
// rejectedByHash. Passing a precomputed hash lets a caller that already
// needs img's dHash for another purpose (validateOne's perceptual dedup)
// avoid hashing the same decoded image twice; pass nil to have it hashed
// here (the path ScreenImage takes, since it has no other use for the hash).
//
// Graceful degradation: a nil img never rejects. A hash failure (nil hash,
// or DifferenceHash erroring when computed here) just skips the
// placeholder-blocklist half and falls through to the flat detector — never
// false-reject on a hashing error.
func (cfg *Config) screenDecodedImage(img image.Image, hash *goimagehash.ImageHash, url string, placeholders *placeholderMatcher) (reject bool, class, reason string) {
	if img == nil {
		return false, "", ""
	}

	if hash == nil {
		if h, err := goimagehash.DifferenceHash(img); err == nil {
			hash = h
		}
	}
	if hash != nil {
		if matched, source := placeholders.matchesHash(hash); matched {
			slog.Debug("imagefy: placeholder rejected", "url", url, "source", source)
			return true, ClassPlaceholder, reasonPhashBlocklist
		}
	}

	if cfg.DisableFlatImageDetection {
		return false, "", ""
	}
	if rejected, flatReason := isNonPhotographic(img, cfg.flatThresholds()); rejected {
		slog.Debug("imagefy: flat/non-photographic rejected", "url", url, "reason", flatReason)
		return true, ClassPlaceholder, reasonFlatImage
	}

	return false, "", ""
}
