package imagefy

import (
	"strings"
	"unicode/utf8"
)

const (
	minWordRunes  = 3
	maxQueryWords = 5
)

var ruStopWords = map[string]bool{
	"в": true, "на": true, "и": true, "из": true, "для": true,
	"что": true, "как": true, "это": true, "по": true, "от": true,
	"с": true, "о": true, "к": true, "не": true, "за": true,
	"у": true, "но": true, "же": true, "все": true, "так": true,
	"его": true, "её": true, "их": true, "мы": true, "вы": true,
	"он": true, "она": true, "они": true, "был": true, "была": true,
	"будет": true, "уже": true, "ещё": true, "еще": true,
	"или": true, "ни": true, "бы": true, "до": true, "под": true,
	"при": true, "без": true, "над": true, "через": true,
}

var enStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "best": true,
	"top": true, "great": true, "good": true, "new": true, "your": true,
	"you": true, "this": true, "that": true, "these": true, "those": true,
	"from": true, "into": true, "about": true, "over": true, "under": true,
	"some": true, "any": true, "all": true, "more": true, "most": true,
	"very": true, "really": true, "just": true, "only": true, "also": true,
}

// BuildImageQuery is the legacy entrypoint — defaults lang to "ru" for
// backwards compatibility. New code should call BuildImageQueryLang.
func BuildImageQuery(title, city string) string {
	return BuildImageQueryLang(title, city, "ru")
}

// BuildImageQueryLang extracts 3-5 meaningful words from title for image search,
// using the appropriate stop-word list for the given language. For unknown langs
// the RU list is used (safe default).
func BuildImageQueryLang(title, city, lang string) string {
	stopWords := ruStopWords
	if lang == "en" {
		stopWords = enStopWords
	}
	words := strings.Fields(title)
	meaningful := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}«»—–-")
		if w == "" {
			continue
		}
		lower := strings.ToLower(w)
		if stopWords[lower] {
			continue
		}
		if utf8.RuneCountInString(w) < minWordRunes {
			continue
		}
		meaningful = append(meaningful, w)
	}
	if len(meaningful) > maxQueryWords {
		meaningful = meaningful[:maxQueryWords]
	}
	query := strings.Join(meaningful, " ")
	if city != "" && !strings.Contains(strings.ToLower(query), strings.ToLower(city)) {
		query += " " + city
	}
	return query
}
