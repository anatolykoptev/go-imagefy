package imagefy

import (
	"strings"
	"testing"
)

func TestBuildImageQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		title    string
		city     string
		contains []string // substrings that must appear in result
		excludes []string // substrings that must NOT appear in result
		wantFull string   // exact match when set
	}{
		{
			name:     "Russian stop words stripped",
			title:    "Новый ресторан в центре Петербурга",
			city:     "",
			contains: []string{"Новый", "ресторан", "центре", "Петербурга"},
			excludes: []string{" в "},
		},
		{
			name:     "short words under 3 runes stripped",
			title:    "Об XX веке и эпохе",
			city:     "",
			contains: []string{"веке", "эпохе"},
			excludes: []string{"Об", "XX", " и "},
		},
		{
			name:  "max 5 meaningful words enforced",
			title: "Открытие большого красивого нового современного культурного центра города",
			city:  "",
			// result must contain exactly 5 space-separated tokens (no city appended)
		},
		{
			name:     "city appended when absent from title",
			title:    "Открытие музея",
			city:     "Москва",
			contains: []string{"Москва"},
		},
		{
			name:     "city not appended when already present verbatim",
			title:    "Открытие выставки Москва 2026",
			city:     "Москва",
			excludes: []string{"Москва Москва"},
		},
		{
			name:     "punctuation stripped from words",
			title:    "«Лучший» ресторан!",
			city:     "",
			contains: []string{"Лучший", "ресторан"},
			excludes: []string{"«", "»", "!"},
		},
		{
			name:  "empty title with city returns city with leading space",
			title: "",
			city:  "Казань",
			// BuildImageQuery appends city as " "+city when query is empty,
			// so the result is " Казань" (leading space). This is the actual behaviour.
			wantFull: " Казань",
		},
		{
			name:     "empty title and empty city returns empty string",
			title:    "",
			city:     "",
			wantFull: "",
		},
		{
			name:     "city comparison is case-insensitive",
			title:    "Выставка современного искусства москва",
			city:     "Москва",
			// "москва" (lower) contains "москва" (lower of city), so city must NOT be appended again
			excludes: []string{"Москва"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := BuildImageQuery(tc.title, tc.city)

			if tc.wantFull != "" || (tc.title == "" && tc.city == "") {
				if got != tc.wantFull {
					t.Errorf("BuildImageQuery(%q, %q) = %q, want %q", tc.title, tc.city, got, tc.wantFull)
				}
				return
			}

			for _, sub := range tc.contains {
				if !strings.Contains(got, sub) {
					t.Errorf("BuildImageQuery(%q, %q) = %q; want it to contain %q", tc.title, tc.city, got, sub)
				}
			}
			for _, sub := range tc.excludes {
				if strings.Contains(got, sub) {
					t.Errorf("BuildImageQuery(%q, %q) = %q; want it NOT to contain %q", tc.title, tc.city, got, sub)
				}
			}

			// Enforce max-5-words rule separately for the dedicated test case.
			if tc.name == "max 5 meaningful words enforced" {
				wordCount := len(strings.Fields(got))
				if wordCount > 5 {
					t.Errorf("BuildImageQuery(%q, %q) = %q; got %d words, want at most 5", tc.title, tc.city, got, wordCount)
				}
			}
		})
	}
}

func TestBuildImageQueryLang_EN(t *testing.T) {
	t.Parallel()
	got := BuildImageQueryLang("best coffee shops in San Francisco", "San Francisco", "en")
	if !strings.Contains(got, "coffee") || !strings.Contains(got, "shops") {
		t.Errorf("got %q, want query containing coffee+shops", got)
	}
	if strings.Contains(strings.ToLower(got), "best") {
		t.Errorf("got %q, should strip 'best'", got)
	}
}

func TestBuildImageQueryLang_EN_StripsTop(t *testing.T) {
	t.Parallel()
	got := BuildImageQueryLang("top 10 restaurants Los Angeles", "Los Angeles", "en")
	if strings.Contains(strings.ToLower(got), "top") {
		t.Errorf("got %q, should strip 'top'", got)
	}
}

func TestBuildImageQueryLang_RU_Backcompat(t *testing.T) {
	t.Parallel()
	// Legacy call path must produce identical output to explicit ru call.
	got1 := BuildImageQuery("Лучшие кофейни в Санкт-Петербурге", "Санкт-Петербург")
	got2 := BuildImageQueryLang("Лучшие кофейни в Санкт-Петербурге", "Санкт-Петербург", "ru")
	if got1 != got2 {
		t.Errorf("BuildImageQuery and BuildImageQueryLang(ru) differ: %q vs %q", got1, got2)
	}
}

func TestBuildImageQueryLang_UnknownLang_FallsBackRU(t *testing.T) {
	t.Parallel()
	// Unknown lang should behave like ru (safe default).
	got1 := BuildImageQueryLang("Лучшие кофейни", "СПб", "unknown")
	got2 := BuildImageQueryLang("Лучшие кофейни", "СПб", "ru")
	if got1 != got2 {
		t.Errorf("unknown lang should fall back to ru, got %q vs %q", got1, got2)
	}
}

func TestBuildImageQueryLang_EN_KeepsProperNouns(t *testing.T) {
	t.Parallel()
	got := BuildImageQueryLang("coffee shops in New York", "New York", "en")
	if !strings.Contains(got, "New") {
		t.Errorf("got %q, should keep 'New' — 'New York' is a proper noun", got)
	}
}

func TestBuildImageQueryLang_EN_UpperCaseLang(t *testing.T) {
	t.Parallel()
	got1 := BuildImageQueryLang("best coffee SF", "SF", "EN")
	got2 := BuildImageQueryLang("best coffee SF", "SF", "en")
	if got1 != got2 {
		t.Errorf("uppercase 'EN' should match 'en': %q vs %q", got1, got2)
	}
}

func TestBuildImageQueryLang_EN_LocaleTag(t *testing.T) {
	t.Parallel()
	got1 := BuildImageQueryLang("best coffee SF", "SF", "en-US")
	got2 := BuildImageQueryLang("best coffee SF", "SF", "en")
	if got1 != got2 {
		t.Errorf("en-US should behave like en: %q vs %q", got1, got2)
	}
}
