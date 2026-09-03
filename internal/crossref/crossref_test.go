package crossref

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
)

type fakeFetcher struct{ body []byte }

func (f fakeFetcher) Fetch(context.Context, string, string, []string, int64) ([]byte, error) {
	return f.body, nil
}

func TestParseFixture(t *testing.T) {
	body, err := os.ReadFile("../../testdata/crossref.json")
	if err != nil {
		t.Fatal(err)
	}
	work, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if work.DOI != "10.5555/example.1" || work.Published.time().Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("unexpected: %+v", work)
	}
}
func TestParseMalformed(t *testing.T) {
	if _, err := Parse([]byte("{")); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}

func TestEnrichPromotesArXivJournalVersion(t *testing.T) {
	body, err := os.ReadFile("../../testdata/crossref.json")
	if err != nil {
		t.Fatal(err)
	}
	items, err := (&Client{HTTP: fakeFetcher{body: body}}).Enrich(context.Background(), []article.Item{{DOI: "10.5555/example.1", URL: "https://arxiv.org/abs/1", SourceType: article.SourceArXiv, PublishedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].PeerReviewed || items[0].SourceType != article.SourcePubMed || items[0].ArXivURL == "" || items[0].URL != "https://doi.org/10.5555/example.1" {
		t.Fatalf("unexpected enrichment: %+v", items)
	}
}
