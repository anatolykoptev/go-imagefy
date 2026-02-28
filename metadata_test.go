package imagefy

import (
	"testing"
)

func TestIsStockByMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta *ImageMetadata
		want bool
	}{
		{
			name: "nil metadata",
			meta: nil,
			want: false,
		},
		{
			name: "empty metadata",
			meta: &ImageMetadata{},
			want: false,
		},
		{
			name: "shutterstock in IPTC copyright",
			meta: &ImageMetadata{IPTCCopyright: "Copyright Shutterstock Inc."},
			want: true,
		},
		{
			name: "getty in IPTC credit",
			meta: &ImageMetadata{IPTCCredit: "Getty Images"},
			want: true,
		},
		{
			name: "istock in EXIF copyright",
			meta: &ImageMetadata{EXIFCopyright: "iStockPhoto.com/photographer"},
			want: true,
		},
		{
			name: "alamy in IPTC source",
			meta: &ImageMetadata{IPTCSource: "Alamy Stock Photo"},
			want: true,
		},
		{
			name: "depositphotos in byline",
			meta: &ImageMetadata{IPTCByline: "Depositphotos user"},
			want: true,
		},
		{
			name: "adobe stock in DC rights",
			meta: &ImageMetadata{DCRights: "Licensed via Adobe Stock"},
			want: true,
		},
		{
			name: "regular photographer returns false",
			meta: &ImageMetadata{
				IPTCCopyright: "Copyright 2024 John Smith",
				IPTCByline:    "John Smith",
				EXIFArtist:    "John Smith",
			},
			want: false,
		},
		{
			name: "case insensitive match",
			meta: &ImageMetadata{IPTCCopyright: "SHUTTERSTOCK, INC."},
			want: true,
		},
		{
			name: "dreamstime in EXIF artist",
			meta: &ImageMetadata{EXIFArtist: "Dreamstime.com/photographer"},
			want: true,
		},
		{
			name: "123rf in IPTC credit",
			meta: &ImageMetadata{IPTCCredit: "123RF Stock Photos"},
			want: true,
		},
		{
			name: "gettyimages as single word",
			meta: &ImageMetadata{IPTCSource: "gettyimages.com"},
			want: true,
		},
		{
			name: "istockphoto in DC creator",
			meta: &ImageMetadata{DCCreator: "istockphoto contributor"},
			want: true,
		},
		{
			name: "adobestock as single word",
			meta: &ImageMetadata{IPTCCopyright: "AdobeStock_123456"},
			want: true,
		},
		{
			name: "bigstockphoto in credit",
			meta: &ImageMetadata{IPTCCredit: "BigStockPhoto"},
			want: true,
		},
		{
			name: "stocksy in source",
			meta: &ImageMetadata{IPTCSource: "Stocksy United"},
			want: true,
		},
		{
			name: "pond5 in copyright",
			meta: &ImageMetadata{IPTCCopyright: "Pond5 Media"},
			want: true,
		},
		{
			name: "masterfile in credit",
			meta: &ImageMetadata{IPTCCredit: "Masterfile Stock"},
			want: true,
		},
		{
			name: "superstock in source",
			meta: &ImageMetadata{IPTCSource: "SuperStock Images"},
			want: true,
		},
		{
			name: "agefotostock in credit",
			meta: &ImageMetadata{IPTCCredit: "agefotostock agency"},
			want: true,
		},
		{
			name: "age fotostock with space",
			meta: &ImageMetadata{IPTCCredit: "Age Fotostock"},
			want: true,
		},
		{
			name: "colourbox in source",
			meta: &ImageMetadata{IPTCSource: "Colourbox"},
			want: true,
		},
		{
			name: "yayimages in copyright",
			meta: &ImageMetadata{IPTCCopyright: "YAYImages"},
			want: true,
		},
		{
			name: "vectorstock in credit",
			meta: &ImageMetadata{IPTCCredit: "VectorStock"},
			want: true,
		},
		{
			name: "freepik in DC rights",
			meta: &ImageMetadata{DCRights: "Freepik Company"},
			want: true,
		},
		{
			name: "canstockphoto in EXIF copyright",
			meta: &ImageMetadata{EXIFCopyright: "CanStockPhoto"},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsStockByMetadata(tc.meta)
			if got != tc.want {
				t.Errorf("IsStockByMetadata() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsCCByMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta *ImageMetadata
		want bool
	}{
		{
			name: "nil metadata",
			meta: nil,
			want: false,
		},
		{
			name: "empty metadata",
			meta: &ImageMetadata{},
			want: false,
		},
		{
			name: "CC BY 4.0 in XMP license",
			meta: &ImageMetadata{XMPLicense: "https://creativecommons.org/licenses/by/4.0/"},
			want: true,
		},
		{
			name: "CC0 in web statement",
			meta: &ImageMetadata{XMPWebStatement: "https://creativecommons.org/publicdomain/zero/1.0/"},
			want: true,
		},
		{
			name: "CC BY-SA in usage terms",
			meta: &ImageMetadata{XMPUsageTerms: "This work is licensed under https://creativecommons.org/licenses/by-sa/4.0/"},
			want: true,
		},
		{
			name: "non-CC URL returns false",
			meta: &ImageMetadata{XMPLicense: "https://example.com/license"},
			want: false,
		},
		{
			name: "CC in DC rights",
			meta: &ImageMetadata{DCRights: "Licensed under https://creativecommons.org/licenses/by/4.0/ by Author"},
			want: true,
		},
		{
			name: "CC publicdomain in DC rights",
			meta: &ImageMetadata{DCRights: "https://creativecommons.org/publicdomain/mark/1.0/"},
			want: true,
		},
		{
			name: "empty license fields",
			meta: &ImageMetadata{
				IPTCCopyright: "Some photographer",
				EXIFArtist:    "John Doe",
			},
			want: false,
		},
		{
			name: "partial CC URL without licenses path returns false",
			meta: &ImageMetadata{XMPLicense: "https://creativecommons.org/about"},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsCCByMetadata(tc.meta)
			if got != tc.want {
				t.Errorf("IsCCByMetadata() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractImageMetadata_NilAndEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "nil data returns nil",
			data: nil,
		},
		{
			name: "empty data returns nil",
			data: []byte{},
		},
		{
			name: "garbage data returns nil",
			data: []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22, 0x33},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractImageMetadata(tc.data)
			if got != nil {
				t.Errorf("ExtractImageMetadata(%v) = %+v, want nil", tc.data, got)
			}
		})
	}
}
