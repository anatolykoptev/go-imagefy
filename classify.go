package imagefy

import (
	"context"
	"log/slog"
	"strings"
)

// VisionPrompt is the default system prompt for LLM-based image classification.
// Consumers may pass a customized version to Classifier.Classify if needed.
const VisionPrompt = `You are an editorial image filter for a city guide website.
We only accept real photographs without stock watermarks.

Classify this image. Answer with exactly one word:
- PHOTO — a real photograph. Small photographer watermark in a corner is OK.
- STOCK — a real photograph with a visible stock photo watermark (Shutterstock,
  Getty Images, iStock, Adobe Stock, Depositphotos, Dreamstime, 123RF, Alamy,
  or any semi-transparent tiled/diagonal watermark pattern typical of stock previews).
- REJECT — banner, advertisement, social media cover, promotional graphic with
  large text overlay, infographic, chart, screenshot, collage, illustration,
  drawing, meme, map, UI element, or image where text/graphics dominate.

Key distinctions:
- Small corner watermark of a photographer → PHOTO
- Repeating diagonal "shutterstock" or stock agency watermark → STOCK
- Promotional banner with overlaid text/branding → REJECT

Answer:`

const visionMaxBytes = 200 * 1024 // 200KB vision preview

// ClassifyImage uses a multimodal LLM to classify the image at imageURL.
// Returns "PHOTO", "STOCK", "REJECT", or "" on error (graceful degradation).
func (cfg *Config) ClassifyImage(ctx context.Context, imageURL string) string {
	cfg.defaults()

	if cfg.Classifier == nil {
		return "" // no classifier → accept
	}

	// Check cache.
	if cfg.Cache != nil {
		cacheKey := cfg.Cache.Key("vision_cls", imageURL)
		var cached string
		if cfg.Cache.Get(ctx, cacheKey, &cached) {
			return cached
		}
		result := cfg.doClassify(ctx, imageURL)
		cfg.Cache.Set(ctx, cacheKey, result)
		return result
	}

	return cfg.doClassify(ctx, imageURL)
}

// IsRealPhoto returns true if the image is not a banner/graphic and not a stock photo.
// Returns true on any error (graceful degradation — never blocks the pipeline).
func (cfg *Config) IsRealPhoto(ctx context.Context, imageURL string) bool {
	cls := cfg.ClassifyImage(ctx, imageURL)
	return cls != "REJECT" && cls != "STOCK"
}

func (cfg *Config) doClassify(ctx context.Context, imageURL string) string {
	r, err := cfg.Download(ctx, imageURL, DownloadOpts{
		MaxBytes: visionMaxBytes,
	})
	if r == nil || err != nil {
		return "" // can't download → accept
	}

	dataURL := EncodeDataURL(r.Data, r.MIMEType)

	resp, err := cfg.Classifier.Classify(ctx, VisionPrompt, []ImageInput{{URL: dataURL}})
	if err != nil {
		slog.Debug("imagefy: vision LLM error", "url", imageURL, "error", err.Error())
		return "" // LLM error → accept
	}

	slog.Debug("imagefy: vision result", "url", imageURL, "response", resp)
	return ParseVisionResponse(resp)
}

// ParseVisionResponse normalizes an LLM response to one of: "PHOTO", "STOCK", "REJECT", or "".
func ParseVisionResponse(resp string) string {
	word := strings.ToUpper(strings.TrimSpace(resp))
	switch {
	case strings.HasPrefix(word, "PHOTO"):
		return "PHOTO"
	case strings.HasPrefix(word, "STOCK"):
		return "STOCK"
	case strings.HasPrefix(word, "REJECT"):
		return "REJECT"
	default:
		return ""
	}
}
