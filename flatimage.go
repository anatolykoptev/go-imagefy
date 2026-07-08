package imagefy

import (
	"fmt"
	"image"
	"math"
)

// Flat/non-photographic image reject gate.
//
// Insight: this catches UNKNOWN placeholders generically, where the phash
// blocklist (placeholder.go) only catches KNOWN, seeded ones. It looks at the
// pixel PALETTE, not the content: a real photograph can never be
// near-monochrome, and a rich, modern AI-generated poster always spans many
// colors — so a near-flat / solid-color / tiny-palette image is almost
// certainly a placeholder or blank, not a legitimate photo or poster. No OCR,
// no vision-LLM: pure Go over the already-decoded image.
const (
	// DefaultFlatImageDominantFraction is the minimum share of sampled pixels
	// that must fall in a single coarse color bucket for the dominant-fraction
	// signal to fire. No real photograph has a single color at this share.
	DefaultFlatImageDominantFraction = 0.85

	// DefaultFlatImageMaxUniqueBuckets is the maximum number of distinct
	// coarse color buckets (out of flatBucketCount) allowed for the
	// unique-buckets signal to fire. A rich AI-generated poster spans far
	// more buckets than this.
	DefaultFlatImageMaxUniqueBuckets = 32

	// DefaultFlatImageMaxEntropyBits is the maximum Shannon entropy (bits) of
	// the sampled color-bucket histogram allowed for the entropy signal to
	// fire.
	DefaultFlatImageMaxEntropyBits = 1.5

	// flatSampleCap bounds the number of pixels sampled per image, keeping
	// the detector's cost roughly constant regardless of image resolution.
	flatSampleCap = 4096

	// flatBitsPerChannel quantizes each color channel down to this many
	// most-significant bits before bucketing (3 bits/channel = 8 levels per
	// channel = 512 total buckets).
	flatBitsPerChannel = 3
	flatBucketCount    = 1 << (3 * flatBitsPerChannel) // 512
)

// flatThresholds bundles the three-signal AND-verdict thresholds for
// isNonPhotographic. Built from Config's FlatImage* fields (see imagefy.go)
// via Config.flatThresholds, after cfg.defaults() has applied safe defaults.
type flatThresholds struct {
	dominantFraction float64
	maxUniqueBuckets int
	maxEntropyBits   float64
}

// flatThresholds builds the isNonPhotographic threshold bundle from this
// Config's FlatImage* fields. Callers must run after cfg.defaults() has
// applied safe defaults — validateOne's pipeline always calls
// ValidateImageURL first, which does.
func (cfg *Config) flatThresholds() flatThresholds {
	return flatThresholds{
		dominantFraction: cfg.FlatImageDominantFraction,
		maxUniqueBuckets: cfg.FlatImageMaxUniqueBuckets,
		maxEntropyBits:   cfg.FlatImageMaxEntropyBits,
	}
}

// isNonPhotographic decides whether img is a flat / near-solid-color / blank
// placeholder by its pixel palette alone, computed over a bounded sample of
// its pixels:
//
//   - dominantFraction — largest coarse-color-bucket count / total samples.
//   - uniqueBuckets    — count of non-empty coarse color buckets.
//   - entropy          — Shannon entropy (bits) of the bucket histogram.
//
// Verdict is a CONSERVATIVE AND across all three signals — never an OR — by
// design: a real photograph fails the dominant-fraction test (no photo has
// one color at 85%+ of its pixels); a rich AI-generated poster fails the
// unique-buckets/entropy test (many colors, high entropy). Only a genuine
// flat/near-solid/blank image satisfies all three simultaneously. Preserve
// this AND on any threshold retune — favor false-negatives (a missed blank is
// cheap) over false-positives (a rejected legitimate poster is not).
//
// Returns (true, reason) on reject with a human-readable metrics summary for
// logging; (false, "") otherwise. A nil or zero-area img is graceful
// degradation — accept, same contract as rejectedByHash.
func isNonPhotographic(img image.Image, t flatThresholds) (bool, string) {
	if img == nil {
		return false, ""
	}

	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= 0 || height <= 0 {
		return false, ""
	}

	var hist [flatBucketCount]int
	total := 0

	stepX, stepY := flatSampleStep(width, height)
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, _ := img.At(x, y).RGBA()
			hist[flatBucket(r, g, b)]++
			total++
		}
	}
	if total == 0 {
		return false, ""
	}

	dominant, unique, entropy := flatHistStats(hist[:], total)
	dominantFraction := float64(dominant) / float64(total)

	if dominantFraction < t.dominantFraction || unique > t.maxUniqueBuckets || entropy > t.maxEntropyBits {
		return false, ""
	}

	reason := fmt.Sprintf("dominant_fraction=%.3f unique_buckets=%d entropy_bits=%.3f samples=%d",
		dominantFraction, unique, entropy, total)
	return true, reason
}

// flatHistStats reduces a color-bucket histogram to the three verdict
// signals: the largest single-bucket count, the number of non-empty buckets,
// and the histogram's Shannon entropy in bits.
func flatHistStats(hist []int, total int) (dominant, unique int, entropyBits float64) {
	for _, count := range hist {
		if count == 0 {
			continue
		}
		unique++
		if count > dominant {
			dominant = count
		}
		p := float64(count) / float64(total)
		entropyBits -= p * math.Log2(p)
	}
	return dominant, unique, entropyBits
}

// flatSampleStep picks an (x, y) pixel stride so the number of samples taken
// across the image stays near flatSampleCap regardless of resolution — O(1)
// detector cost instead of O(width*height).
func flatSampleStep(width, height int) (stepX, stepY int) {
	total := width * height
	if total <= flatSampleCap {
		return 1, 1
	}
	step := int(math.Sqrt(float64(total) / float64(flatSampleCap)))
	if step < 1 {
		step = 1
	}
	return step, step
}

// flatBucket quantizes an RGBA() triple (16-bit, alpha-premultiplied per the
// image.Color contract) down to a flatBitsPerChannel-per-channel coarse
// bucket index in [0, flatBucketCount).
func flatBucket(r, g, b uint32) int {
	const shift = 16 - flatBitsPerChannel
	ri := int(r >> shift)
	gi := int(g >> shift)
	bi := int(b >> shift)
	return (ri << (2 * flatBitsPerChannel)) | (gi << flatBitsPerChannel) | bi
}
