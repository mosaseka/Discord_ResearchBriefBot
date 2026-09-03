package article

import (
	"testing"
	"time"
)

func TestDedupePrefersReviewedAndKeepsArXiv(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	preprint := Item{Title: "Contextual Bandits for Mental Health", DOI: "https://doi.org/10.1/X", ArXivID: "2601.1v2", URL: "https://arxiv.org/abs/2601.1", SourceType: SourceArXiv, UpdatedAt: now}
	reviewed := Item{Title: "Contextual Bandits for Mental Health", DOI: "10.1/x", PMID: "42", URL: "https://pubmed.ncbi.nlm.nih.gov/42/", SourceType: SourcePubMed, PeerReviewed: true, UpdatedAt: now.Add(-time.Hour)}
	got := Dedupe([]Item{preprint, reviewed})
	if len(got) != 1 {
		t.Fatalf("got %d items", len(got))
	}
	if got[0].PMID != "42" || got[0].URL != reviewed.URL || got[0].ArXivURL != preprint.URL {
		t.Fatalf("unexpected merge: %+v", got[0])
	}
}

func TestFuzzyTitleSimilarity(t *testing.T) {
	similar := FuzzyTitleSimilarity("Offline RL for Personalized Mental-Health Care", "Offline RL for Personalized Mental Health Care.")
	if similar < .9 {
		t.Fatalf("similarity %f", similar)
	}
	if FuzzyTitleSimilarity("A cat", "Warehouse routing") >= .9 {
		t.Fatal("unrelated titles matched")
	}
}
