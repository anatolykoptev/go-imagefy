package imagefy

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"sync"

	"github.com/corona10/goimagehash"
	_ "golang.org/x/image/webp"
)

// dedupThreshold is the maximum Hamming distance between two dHash values
// below which images are considered perceptually identical.
const dedupThreshold = 10

// dedupFilter is a per-search-call deduplication filter based on perceptual hashing.
// It is safe for concurrent use.
type dedupFilter struct {
	mu     sync.Mutex
	hashes []*goimagehash.ImageHash
}

// isDuplicate returns true if img is perceptually identical to a previously seen
// image. If hashing fails for any reason, the image is accepted (graceful degradation).
// When the image is accepted as unique, its hash is stored for future comparisons.
func (d *dedupFilter) isDuplicate(img image.Image) bool {
	hash, err := goimagehash.DifferenceHash(img)
	if err != nil {
		// Graceful degradation: unable to hash → accept the image.
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, h := range d.hashes {
		dist, err := hash.Distance(h)
		if err == nil && dist < dedupThreshold {
			return true
		}
	}

	d.hashes = append(d.hashes, hash)
	return false
}

// downloadForValidation fetches the image and returns both raw bytes and decoded image.
// Raw bytes are used for metadata extraction; decoded image is used for perceptual dedup.
// Returns (nil, nil) on any recoverable failure for graceful degradation.
func (cfg *Config) downloadForValidation(ctx context.Context, url string) ([]byte, image.Image) {
	result, err := cfg.Download(ctx, url, DownloadOpts{})
	if err != nil || result == nil {
		return nil, nil
	}

	img, _, err := image.Decode(bytes.NewReader(result.Data))
	if err != nil {
		// Raw bytes available for metadata even if image decode fails.
		return result.Data, nil
	}

	return result.Data, img
}
