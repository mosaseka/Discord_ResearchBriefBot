package discord

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/evidence"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/ranking"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/textutil"
)

func TestBuildLimitsUnicodeAndMentions(t *testing.T) {
	item := ranking.Ranked{Item: article.Item{Title: strings.Repeat("👩🏽‍💻", 200) + " @everyone", URL: "https://example.org/p", SourceType: article.SourceArXiv, PublishedAt: time.Now()}, Contribution: strings.Repeat("é", 800) + " <@123>", Evidence: evidence.Assessment{Reason: "低 — @here"}, UndergraduateMeaning: "test", Breakdown: ranking.Breakdown{Relevance: 10}, Topics: []string{"patient simulation"}}
	payloads, err := Build("2026-09-04", 30, []ranking.Ranked{item})
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range payloads {
		if textutil.Units(payload.Content) > MaxMessageContent {
			t.Fatal("content too long")
		}
		raw, _ := json.Marshal(payload)
		if strings.Contains(string(raw), "@everyone") || strings.Contains(string(raw), "<@123>") || strings.Contains(string(raw), "```") {
			t.Fatalf("mention remained: %s", raw)
		}
		if len(payload.AllowedMentions.Parse) != 0 {
			t.Fatal("mentions enabled")
		}
		for _, embed := range payload.Embeds {
			if err := ValidateEmbed(embed); err != nil {
				t.Fatal(err)
			}
		}
	}
}
