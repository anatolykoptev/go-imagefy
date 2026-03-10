package imagefy

import (
	"context"
	"log/slog"
)

// ClassifyImageFull uses a multimodal LLM to classify the image at imageURL.
// Returns a ClassificationResult with Class and Confidence.
// On error, returns a zero-value result (graceful degradation — never blocks the pipeline).
// Uses Config.VisionPrompt if set, otherwise DefaultVisionPrompt.
// Cache key prefix is "vision_cls_v2" (distinct from the legacy "vision_cls" prefix).
func (cfg *Config) ClassifyImageFull(ctx context.Context, imageURL string) ClassificationResult {
	cfg.defaults()

	if cfg.Classifier == nil {
		return ClassificationResult{} // no classifier → accept
	}

	if cfg.Cache != nil {
		cacheKey := cfg.Cache.Key("vision_cls_v2", imageURL)
		var cached ClassificationResult
		if cfg.Cache.Get(ctx, cacheKey, &cached) {
			return cached
		}
		result := cfg.doClassifyFull(ctx, imageURL)
		cfg.Cache.Set(ctx, cacheKey, result)
		return result
	}

	return cfg.doClassifyFull(ctx, imageURL)
}

// ClassifyImage uses a multimodal LLM to classify the image at imageURL.
// Returns "PHOTO", "STOCK", "REJECT", or "" on error (graceful degradation).
func (cfg *Config) ClassifyImage(ctx context.Context, imageURL string) string {
	return cfg.ClassifyImageFull(ctx, imageURL).Class
}

// IsRealPhoto returns true if the image is a real photograph (PHOTO class or graceful-degrade empty).
// Returns true on any error (graceful degradation — never blocks the pipeline).
func (cfg *Config) IsRealPhoto(ctx context.Context, imageURL string) bool {
	cls := cfg.ClassifyImage(ctx, imageURL)
	return cls == ClassPhoto || cls == ""
}

func (cfg *Config) doClassifyFull(ctx context.Context, imageURL string) ClassificationResult {
	r, err := cfg.Download(ctx, imageURL, DownloadOpts{
		MaxBytes: visionMaxBytes,
	})
	if r == nil || err != nil {
		return ClassificationResult{} // can't download → accept
	}

	return cfg.classifyFromData(ctx, imageURL, r.Data, r.MIMEType)
}

// classifyPredownloaded classifies an already-downloaded image, avoiding a
// redundant HTTP download. Uses the same cache key as ClassifyImageFull.
func (cfg *Config) classifyPredownloaded(ctx context.Context, imageURL string, data []byte, mimeType string) ClassificationResult {
	cfg.defaults()

	if cfg.Classifier == nil {
		return ClassificationResult{} // no classifier → accept
	}

	if cfg.Cache != nil {
		cacheKey := cfg.Cache.Key("vision_cls_v2", imageURL)
		var cached ClassificationResult
		if cfg.Cache.Get(ctx, cacheKey, &cached) {
			return cached
		}
		result := cfg.classifyFromData(ctx, imageURL, data, mimeType)
		cfg.Cache.Set(ctx, cacheKey, result)
		return result
	}

	return cfg.classifyFromData(ctx, imageURL, data, mimeType)
}

// classifyFromData sends image data to the LLM classifier and parses the result.
func (cfg *Config) classifyFromData(ctx context.Context, imageURL string, data []byte, mimeType string) ClassificationResult {
	if len(data) == 0 {
		return ClassificationResult{} // no data → accept
	}

	dataURL := EncodeDataURL(data, mimeType)

	prompt := cfg.VisionPrompt
	if prompt == "" {
		prompt = DefaultVisionPrompt
	}

	resp, err := cfg.Classifier.Classify(ctx, prompt, []ImageInput{{URL: dataURL}})
	if err != nil {
		slog.Debug("imagefy: vision LLM error", "url", imageURL, "error", err.Error())
		return ClassificationResult{} // LLM error → accept
	}

	slog.Debug("imagefy: vision result", "url", imageURL, "response", resp)
	result := ParseClassificationResult(resp)

	cfg.emitClassification(imageURL, result.Class, result.Confidence, "llm")

	return result
}
