package feed

import (
	"testing"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/config"
)

func TestParseAtom(t *testing.T) {
	body := []byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Contextual bandit mental health</title><link href="https://example.org/p"/><published>2026-09-01T00:00:00Z</published><summary>mental health intervention</summary></entry></feed>`)
	items, err := Parse(body, config.Feed{Name: "Research", Language: "en", Type: "technical_report"})
	if err != nil || len(items) != 1 || items[0].URL != "https://example.org/p" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
func TestParseMalformed(t *testing.T) {
	if _, err := Parse([]byte("<rss>"), config.Feed{Name: "x"}); err == nil {
		t.Fatal("expected error")
	}
}
