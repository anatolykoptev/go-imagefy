package imagefy

import (
	"strconv"
	"strings"
)

// DefaultVisionPrompt is the default system prompt for LLM-based image classification.
// It extends the original 3-class prompt to 6 classes and requests a confidence score.
// Consumers may override this by setting Config.VisionPrompt.
const DefaultVisionPrompt = `You are an editorial image filter for a city guide website.
We only accept real photographs without stock watermarks.

Classify this image. Answer with one word and your confidence (0.0 to 1.0).

Categories:
- PHOTO — real photograph. Small corner watermark is OK.
- STOCK — photograph with visible stock watermark (Shutterstock, Getty, iStock, etc.)
- REJECT — banner, ad, promotional graphic, large text overlay, collage, meme.
- SCREENSHOT — screenshot of a website, app, or software interface.
- ILLUSTRATION — drawing, painting, digital art, cartoon, vector graphic.
- MAP — map, satellite view, floor plan, diagram.

Key distinctions:
- Small corner watermark of photographer → PHOTO
- Repeating diagonal stock watermark → STOCK
- Text/graphics dominate the image → REJECT

Answer format: CLASS 0.95
Example: PHOTO 0.92
Answer:`

// VisionPrompt is kept for backward compatibility.
//
// Deprecated: Use DefaultVisionPrompt instead.
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

// Classification class constants.
const (
	ClassPhoto        = "PHOTO"
	ClassStock        = "STOCK"
	ClassReject       = "REJECT"
	ClassScreenshot   = "SCREENSHOT"
	ClassIllustration = "ILLUSTRATION"
	ClassMap          = "MAP"
)

// classificationClasses lists valid classification labels, ordered longest-first
// to prevent prefix ambiguity during parsing (e.g. "SCREENSHOT" before "STOCK").
var classificationClasses = []string{
	ClassIllustration, ClassScreenshot, ClassReject, ClassPhoto, ClassStock, ClassMap,
}

// ClassificationEvent is emitted by the audit log callback for each classification decision.
type ClassificationEvent struct {
	URL        string  // image URL that was classified
	Class      string  // classification result (PHOTO, STOCK, etc.)
	Confidence float64 // 0.0–1.0
	Source     string  // "llm", "license_assessment", or "prefilter" (legacy)
}

// ClassificationResult holds the output of ClassifyImageFull.
type ClassificationResult struct {
	Class      string  // PHOTO, STOCK, REJECT, SCREENSHOT, ILLUSTRATION, MAP, or ""
	Confidence float64 // 0.0–1.0; 0 if not provided or out of range
}

// ParseClassificationResult parses an LLM response of the form "CLASS 0.95".
// It handles case insensitivity, extra whitespace, and trailing LLM noise.
// Confidence must be in (0, 1]; otherwise it is set to 0.
// Returns a zero-value ClassificationResult for unrecognized responses.
func ParseClassificationResult(resp string) ClassificationResult {
	upper := strings.ToUpper(strings.TrimSpace(resp))
	if upper == "" {
		return ClassificationResult{}
	}

	var matched string
	for _, cls := range classificationClasses {
		if strings.HasPrefix(upper, cls) {
			matched = cls
			break
		}
	}
	if matched == "" {
		return ClassificationResult{}
	}

	remainder := strings.TrimSpace(upper[len(matched):])
	if remainder == "" {
		return ClassificationResult{Class: matched}
	}

	fields := strings.Fields(remainder)
	conf, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || conf <= 0 || conf > 1 {
		return ClassificationResult{Class: matched}
	}

	return ClassificationResult{Class: matched, Confidence: conf}
}

// ParseVisionResponse normalizes an LLM response to one of: "PHOTO", "STOCK", "REJECT", or "".
//
// Deprecated: Only handles the legacy 3-class prompt. Responses from [DefaultVisionPrompt]
// (SCREENSHOT, ILLUSTRATION, MAP) will return "". Use [ParseClassificationResult] instead.
func ParseVisionResponse(resp string) string {
	word := strings.ToUpper(strings.TrimSpace(resp))
	switch {
	case strings.HasPrefix(word, ClassPhoto):
		return ClassPhoto
	case strings.HasPrefix(word, ClassStock):
		return ClassStock
	case strings.HasPrefix(word, ClassReject):
		return ClassReject
	default:
		return ""
	}
}
