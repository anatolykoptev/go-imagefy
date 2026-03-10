package imagefy

import (
	"bytes"
	"strings"

	"github.com/bep/imagemeta"
)

// ImageMetadata holds EXIF, IPTC, XMP, and Dublin Core fields extracted from
// image binary data. Used for stock-photo detection and CC license detection.
type ImageMetadata struct {
	EXIFCopyright   string
	EXIFArtist      string
	IPTCCopyright   string
	IPTCCredit      string
	IPTCSource      string
	IPTCByline      string
	XMPLicense      string
	XMPWebStatement string
	XMPUsageTerms   string
	XMPMarked       bool // xmpRights:Marked
	DCRights        string
	DCCreator       string
}

// stockMetadataKeywords are substrings that indicate a stock-photo agency when
// found (case-insensitive) in any metadata field.
var stockMetadataKeywords = []string{
	"shutterstock",
	"gettyimages",
	"getty images",
	"istockphoto",
	"istock",
	"alamy",
	"depositphotos",
	"dreamstime",
	"123rf",
	"adobestock",
	"adobe stock",
	"bigstockphoto",
	"stocksy",
	"pond5",
	"masterfile",
	"superstock",
	"agefotostock",
	"age fotostock",
	"colourbox",
	"yayimages",
	"vectorstock",
	"freepik",
	"canstockphoto",
}

// IsStockByMetadata reports whether the image metadata contains fingerprints
// of a known stock-photo agency (case-insensitive word-boundary match).
func IsStockByMetadata(meta *ImageMetadata) bool {
	if meta == nil {
		return false
	}
	fields := []string{
		meta.EXIFCopyright,
		meta.EXIFArtist,
		meta.IPTCCopyright,
		meta.IPTCCredit,
		meta.IPTCSource,
		meta.IPTCByline,
		meta.DCRights,
		meta.DCCreator,
	}
	for _, f := range fields {
		if f == "" {
			continue
		}
		lower := strings.ToLower(f)
		for _, kw := range stockMetadataKeywords {
			if containsWord(lower, kw) {
				return true
			}
		}
	}
	return false
}

// containsWord reports whether text contains word at a word boundary.
// A word boundary is the start/end of text or a non-alphanumeric character.
func containsWord(text, word string) bool {
	for {
		idx := strings.Index(text, word)
		if idx < 0 {
			return false
		}
		end := idx + len(word)
		leftOK := idx == 0 || !isWordChar(text[idx-1])
		rightOK := end >= len(text) || !isWordChar(text[end])
		if leftOK && rightOK {
			return true
		}
		text = text[idx+1:]
	}
}

// isWordChar reports whether b is an ASCII alphanumeric character.
func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// IsCCByMetadata reports whether the image metadata contains a Creative
// Commons license URL in any XMP/DC license field.
func IsCCByMetadata(meta *ImageMetadata) bool {
	if meta == nil {
		return false
	}
	for _, f := range []string{
		meta.XMPLicense,
		meta.XMPWebStatement,
		meta.XMPUsageTerms,
		meta.DCRights,
	} {
		if IsCCLicenseURL(f) {
			return true
		}
	}
	return false
}

// wantedTags maps (source, tag-name) → true for every tag we care about.
var wantedTags = map[imagemeta.Source]map[string]bool{
	imagemeta.IPTC: {
		"CopyrightNotice": true,
		"Credit":          true,
		"Byline":          true,
		"Source":          true,
	},
	imagemeta.EXIF: {
		"Copyright": true,
		"Artist":    true,
	},
	imagemeta.XMP: {
		"WebStatement": true,
		"UsageTerms":   true,
		"License":      true,
		"Marked":       true,
		"Rights":       true,
		"Creator":      true,
	},
}

// ExtractImageMetadata parses EXIF/IPTC/XMP metadata from raw image bytes.
// Returns nil if the data is nil, empty, or cannot be parsed.
// Graceful degradation: never returns an error.
func ExtractImageMetadata(data []byte) *ImageMetadata {
	if len(data) == 0 {
		return nil
	}

	meta := &ImageMetadata{}
	found := false

	_, err := imagemeta.Decode(imagemeta.Options{
		R:       bytes.NewReader(data),
		Sources: imagemeta.EXIF | imagemeta.IPTC | imagemeta.XMP,
		ShouldHandleTag: func(ti imagemeta.TagInfo) bool {
			if tags, ok := wantedTags[ti.Source]; ok {
				return tags[ti.Tag]
			}
			return false
		},
		HandleTag: func(ti imagemeta.TagInfo) error {
			switch ti.Source {
			case imagemeta.IPTC:
				handleIPTCTag(meta, ti, &found)
			case imagemeta.EXIF:
				handleEXIFTag(meta, ti, &found)
			case imagemeta.XMP:
				handleXMPTag(meta, ti, &found)
			}
			return nil
		},
	})

	if err != nil || !found {
		return nil
	}

	return meta
}

