package textutil

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"golang.org/x/text/unicode/norm"
)

var tagPattern = regexp.MustCompile(`<[^>]*>`)

func Clean(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = html.UnescapeString(tagPattern.ReplaceAllString(s, " "))
	return strings.Join(strings.Fields(s), " ")
}

func Comparison(s string) string {
	s = strings.ToLower(norm.NFKC.String(strings.ToValidUTF8(s, "�")))
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			space = false
		} else {
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func Units(s string) int { return len(utf16.Encode([]rune(strings.ToValidUTF8(s, "�")))) }

func Truncate(s string, maxUnits int) string {
	if maxUnits <= 0 {
		return ""
	}
	s = strings.ToValidUTF8(s, "�")
	if Units(s) <= maxUnits {
		return s
	}
	const suffix = "…"
	budget := maxUnits - Units(suffix)
	if budget <= 0 {
		return suffix
	}
	it := graphemes.FromString(s)
	used, end := 0, 0
	for it.Next() {
		cost := Units(it.Value())
		if used+cost > budget {
			break
		}
		used += cost
		end = it.End()
	}
	return strings.TrimSpace(s[:end]) + suffix
}

// SuppressMentions keeps untrusted metadata visibly readable while ensuring it
// cannot form a Discord mention. The REST payload also disables all mentions.
func SuppressMentions(s string) string {
	s = strings.ReplaceAll(s, "```", "``\u200b`")
	return strings.ReplaceAll(s, "@", "@\u200b")
}
