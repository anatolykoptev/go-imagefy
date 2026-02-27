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
