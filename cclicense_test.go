package imagefy

import (
	"testing"
)

func TestIsCCLicenseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "CC BY 4.0",
			url:  "https://creativecommons.org/licenses/by/4.0/",
			want: true,
		},
		{
			name: "CC BY-SA 3.0",
			url:  "https://creativecommons.org/licenses/by-sa/3.0/",
			want: true,
		},
		{
			name: "CC0 public domain",
			url:  "https://creativecommons.org/publicdomain/zero/1.0/",
			want: true,
		},
		{
			name: "public domain mark",
			url:  "https://creativecommons.org/publicdomain/mark/1.0/",
			want: true,
		},
		{
			name: "not CC URL",
			url:  "https://example.com/licenses/mit",
			want: false,
		},
		{
			name: "empty string",
			url:  "",
			want: false,
		},
		{
			name: "CC homepage without license path",
			url:  "https://creativecommons.org/",
			want: false,
		},
		{
			name: "http scheme",
			url:  "http://creativecommons.org/licenses/by/4.0/",
			want: true,
		},
		{
			name: "protocol-relative URL",
			url:  "//creativecommons.org/licenses/by-sa/4.0/",
			want: true,
		},
		{
			name: "uppercase URL",
			url:  "HTTPS://CREATIVECOMMONS.ORG/LICENSES/BY/4.0/",
			want: true,
		},
		{
			name: "CC BY-NC-ND 4.0",
			url:  "https://creativecommons.org/licenses/by-nc-nd/4.0/",
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsCCLicenseURL(tc.url)
			if got != tc.want {
				t.Errorf("IsCCLicenseURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestExtractCCLicense(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "empty HTML",
			html: "",
			want: "",
		},
		{
			name: "no license link",
			html: `<html><head><title>No License</title></head><body><p>Hello</p></body></html>`,
			want: "",
		},
		{
			name: "rel=license with CC BY 4.0",
			html: `<html><body><a rel="license" href="https://creativecommons.org/licenses/by/4.0/">CC BY 4.0</a></body></html>`,
			want: "https://creativecommons.org/licenses/by/4.0/",
		},
		{
			name: "rel=license with CC BY-SA link tag",
			html: `<html><head><link rel="license" href="https://creativecommons.org/licenses/by-sa/4.0/"/></head></html>`,
			want: "https://creativecommons.org/licenses/by-sa/4.0/",
		},
		{
			name: "CC0 public domain",
			html: `<html><body><a rel="license" href="https://creativecommons.org/publicdomain/zero/1.0/">CC0</a></body></html>`,
			want: "https://creativecommons.org/publicdomain/zero/1.0/",
		},
		{
			name: "non-CC license link ignored",
			html: `<html><body><a rel="license" href="https://example.com/license">MIT</a></body></html>`,
			want: "",
		},
		{
			name: "CC URL in href without rel=license (bare CC href)",
			html: `<html><body><a href="https://creativecommons.org/licenses/by/4.0/">License</a></body></html>`,
			want: "https://creativecommons.org/licenses/by/4.0/",
		},
		{
			name: "CC URL with http scheme",
			html: `<html><body><a rel="license" href="http://creativecommons.org/licenses/by/3.0/">CC BY 3.0</a></body></html>`,
			want: "http://creativecommons.org/licenses/by/3.0/",
		},
		{
			name: "mixed quotes and whitespace",
			html: `<html><body><a  rel='license'   href='https://creativecommons.org/licenses/by-nc/4.0/' >CC BY-NC</a></body></html>`,
			want: "https://creativecommons.org/licenses/by-nc/4.0/",
		},
		{
			name: "CC URL in meta tag",
			html: `<html><head><meta name="license" content="https://creativecommons.org/licenses/by/4.0/"></head></html>`,
			want: "https://creativecommons.org/licenses/by/4.0/",
		},
		{
			name: "real-world Wikipedia page with protocol-relative URL",
			html: `<html><head><link rel="license" href="//creativecommons.org/licenses/by-sa/4.0/"></head><body>Wikipedia content</body></html>`,
			want: "//creativecommons.org/licenses/by-sa/4.0/",
		},
		{
			name: "href before rel=license attribute order",
			html: `<html><body><a href="https://creativecommons.org/licenses/by/4.0/" rel="license">CC BY 4.0</a></body></html>`,
			want: "https://creativecommons.org/licenses/by/4.0/",
		},
		{
			name: "CC homepage URL without license path ignored",
			html: `<html><body><a href="https://creativecommons.org/">CC Home</a></body></html>`,
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ExtractCCLicense(tc.html)
			if got != tc.want {
				t.Errorf("ExtractCCLicense(...) = %q, want %q", got, tc.want)
			}
		})
	}
}
