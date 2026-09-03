package ranking

import (
	"reflect"
	"testing"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
)

func TestRankDeterministicAndReviewedPreferred(t *testing.T) {
	now := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	items := []article.Item{{Title: "Safe reinforcement learning for mental health", Abstract: "We evaluate a simulation benchmark.", URL: "https://example.org/a", SourceType: article.SourceArXiv, PublishedAt: now}, {Title: "Contextual bandit mental health intervention", Abstract: "A randomized controlled trial included N=300 participants.", URL: "https://example.org/b", SourceType: article.SourcePubMed, PeerReviewed: true, PublishedAt: now}}
	a, b := Rank(items, now), Rank(items, now)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("ranking changed")
	}
	if len(a) != 2 || a[0].Item.URL != "https://example.org/b" {
		t.Fatalf("unexpected order: %+v", a)
	}
}
