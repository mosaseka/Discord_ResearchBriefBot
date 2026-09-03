package ranking

import (
	"sort"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/evidence"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/researchrelevance"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/summarizer"
)

type Breakdown struct {
	Relevance          int `json:"relevance"`
	SourceQuality      int `json:"source_quality"`
	Evidence           int `json:"evidence"`
	Freshness          int `json:"freshness"`
	TechnicalRelevance int `json:"technical_relevance"`
}

func (b Breakdown) Total() int {
	return b.Relevance + b.SourceQuality + b.Evidence + b.Freshness + b.TechnicalRelevance
}

type Ranked struct {
	Item                 article.Item
	Breakdown            Breakdown
	Evidence             evidence.Assessment
	Topics               []string
	Contribution         string
	UndergraduateMeaning string
}

func Rank(items []article.Item, now time.Time) []Ranked {
	out := make([]Ranked, 0, len(items))
	for _, item := range items {
		rel := researchrelevance.Analyze(item)
		if !rel.Relevant {
			continue
		}
		ev := evidence.Analyze(item)
		breakdown := Breakdown{
			Relevance:          rel.Score,
			SourceQuality:      sourceScore(item),
			Evidence:           map[evidence.Level]int{evidence.High: 20, evidence.Medium: 12, evidence.Low: 4}[ev.Level],
			Freshness:          freshness(item.PublishedAt, now),
			TechnicalRelevance: min(10, len(rel.Topics)*3),
		}
		out = append(out, Ranked{Item: item, Breakdown: breakdown, Evidence: ev, Topics: rel.Topics,
			Contribution: summarizer.Extract(item.Abstract, 700), UndergraduateMeaning: researchrelevance.UndergraduateMeaning(rel.Topics)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Breakdown.Total() != out[j].Breakdown.Total() {
			return out[i].Breakdown.Total() > out[j].Breakdown.Total()
		}
		if sourceScore(out[i].Item) != sourceScore(out[j].Item) {
			return sourceScore(out[i].Item) > sourceScore(out[j].Item)
		}
		if !out[i].Item.PublishedAt.Equal(out[j].Item.PublishedAt) {
			return out[i].Item.PublishedAt.After(out[j].Item.PublishedAt)
		}
		return out[i].Item.NormalizedTitle() < out[j].Item.NormalizedTitle()
	})
	return out
}

func sourceScore(item article.Item) int {
	if item.PeerReviewed {
		return 25
	}
	weight := min(4, max(0, item.SourceWeight-1))
	switch item.SourceType {
	case article.SourceArXiv:
		return 14
	case article.SourceTechnicalReport:
		return 10 + weight
	default:
		return 5 + weight
	}
}

func freshness(published, now time.Time) int {
	if published.IsZero() {
		return 0
	}
	days := int(now.Sub(published).Hours() / 24)
	switch {
	case days <= 7:
		return 10
	case days <= 14:
		return 8
	case days <= 30:
		return 5
	default:
		return 0
	}
}
