package imagefy

import (
	"strings"
	"unicode/utf8"
)

// minWordRunes is the minimum rune count for a word to be kept in the query.
const minWordRunes = 3

// maxQueryWords is the maximum number of meaningful words in the image query.
const maxQueryWords = 5

// ruStopWords are common Russian stop words to strip from image search queries.
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

// BuildImageQuery extracts 3-5 meaningful words from title for image search.
// Strips Russian stop words and short words. Appends city if not already present.
func BuildImageQuery(title, city string) string {
	words := strings.Fields(title)
	var meaningful []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}«»—–-")
		if w == "" {
			continue
		}
		lower := strings.ToLower(w)
		if ruStopWords[lower] {
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
