package imagefy

import (
	"image"

	"github.com/corona10/goimagehash"
)

// placeholderThreshold is the maximum Hamming distance (inclusive) between an
// image's dHash and a known-placeholder dHash for the image to be rejected as
// a hotlink-protection / "image unavailable" placeholder.
//
// This is intentionally TIGHTER than dedupThreshold (10, "same photo" identity):
// placeholders are served near-byte-identical across fetches — same CDN asset,
// same encoding — so a real match is expected to be exact or near-exact. A loose
// threshold risks false-rejecting a legitimate photo that happens to share coarse
// tonal structure (a plain sky, a mostly-flat banner-like product shot) with a
// blocklisted graphic. FP-guard tests in placeholder_test.go assert zero false
// positives across a real-photo corpus at this threshold — tighten further if any
// future FP-guard case fails; only loosen alongside a new regression case proving
// a real placeholder variant needs the extra slack.
const placeholderThreshold = 6

// placeholderHash pairs a known-placeholder dHash with an auditable source label
// used in reject logs.
type placeholderHash struct {
	hash   uint64
	source string
}

// defaultPlaceholderHashes are dHash (goimagehash.DHash, 64-bit) values computed
// offline from known hotlink-protection / "image unavailable" placeholder images.
// We embed only the hash — never the image bytes — per license/repo-size hygiene.
//
// To add an entry: download the placeholder image, decode it, run
// goimagehash.DifferenceHash(img), and record hash.GetHash() here with a source
// comment. Operators can also inject additional hashes at runtime without a
// go-imagefy release via Config.PlaceholderHashes (see imagefy.go).
var defaultPlaceholderHashes = []placeholderHash{
	{
		hash:   0x000f1b0707071703,
		source: "Wikimedia Commons: File:No_Image_Available.jpg (generic image-unavailable placeholder, 547x547 JPEG)",
	},
	{
		hash:   0xf0f0e8aeb6c4f0f0,
		source: "Wikimedia Commons: File:No_image_available.svg (rendered as 960px PNG; generic gray no-image box)",
	},
}

// placeholderMatcher rejects images that perceptually match a known
// hotlink-protection / "image unavailable" placeholder. Safe for concurrent use
// (read-only after construction).
type placeholderMatcher struct {
	entries []placeholderHash
	hashes  []*goimagehash.ImageHash
}

// newPlaceholderMatcher builds a matcher from the embedded default blocklist plus
// any operator-supplied extra hashes (Config.PlaceholderHashes). extra hashes are
// labeled "config" in reject logs since they carry no source description.
func newPlaceholderMatcher(extra []uint64) *placeholderMatcher {
	entries := make([]placeholderHash, 0, len(defaultPlaceholderHashes)+len(extra))
	entries = append(entries, defaultPlaceholderHashes...)
	for _, h := range extra {
		entries = append(entries, placeholderHash{hash: h, source: "config"})
	}

	hashes := make([]*goimagehash.ImageHash, len(entries))
	for i, e := range entries {
		hashes[i] = goimagehash.NewImageHash(e.hash, goimagehash.DHash)
	}

	return &placeholderMatcher{entries: entries, hashes: hashes}
}

// matches returns true and the matched entry's source label if img is within
// placeholderThreshold of any known placeholder hash.
//
// If hashing fails for any reason, matches returns (false, "") — graceful
// degradation: never false-reject on error, accept and let downstream checks
// (license assessment, reverse-stock, vision classification) decide.
func (m *placeholderMatcher) matches(img image.Image) (bool, string) {
	hash, err := goimagehash.DifferenceHash(img)
	if err != nil {
		return false, ""
	}

	for i, h := range m.hashes {
		dist, err := hash.Distance(h)
		if err == nil && dist <= placeholderThreshold {
			return true, m.entries[i].source
		}
	}
	return false, ""
}
