package article

import (
	"math"
	"sort"
	"strings"
)

// Dedupe applies identifier precedence first and then conservative fuzzy title
// matching. Input ordering never changes the selected canonical record.
func Dedupe(items []Item) []Item {
	items = append([]Item(nil), items...)
	for i := range items {
		items[i].Normalize()
	}
	sort.SliceStable(items, func(i, j int) bool { return canonicalLess(items[i], items[j]) })
	var groups []Item
	for _, candidate := range items {
		matched := -1
		for idx := range groups {
			if sameIdentity(groups[idx], candidate) || FuzzyTitleSimilarity(groups[idx].Title, candidate.Title) >= 0.9 {
				matched = idx
				break
			}
		}
		if matched < 0 {
			groups = append(groups, candidate)
			continue
		}
		groups[matched] = merge(groups[matched], candidate)
	}
	return groups
}

func sameIdentity(a, b Item) bool {
	if a.DOI != "" && b.DOI != "" && a.DOI == b.DOI {
		return true
	}
	if a.ArXivID != "" && b.ArXivID != "" && a.ArXivID == b.ArXivID {
		return true
	}
	if a.PMID != "" && b.PMID != "" && a.PMID == b.PMID {
		return true
	}
	return a.NormalizedTitle() != "" && a.NormalizedTitle() == b.NormalizedTitle()
}

func canonicalLess(a, b Item) bool {
	qa, qb := sourcePriority(a), sourcePriority(b)
	if qa != qb {
		return qa > qb
	}
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.After(b.UpdatedAt)
	}
	return a.Identity() < b.Identity()
}

func sourcePriority(i Item) int {
	if i.PeerReviewed {
		return 4
	}
	if i.SourceType == SourceArXiv {
		return 3
	}
	if i.SourceType == SourceTechnicalReport {
		return 2
	}
	return 1
}

func merge(primary, secondary Item) Item {
	if primary.DOI == "" {
		primary.DOI = secondary.DOI
	}
	if primary.ArXivID == "" {
		primary.ArXivID = secondary.ArXivID
	}
	if primary.PMID == "" {
		primary.PMID = secondary.PMID
	}
	if primary.ArXivURL == "" {
		if secondary.SourceType == SourceArXiv {
			primary.ArXivURL = secondary.URL
		} else {
			primary.ArXivURL = secondary.ArXivURL
		}
	}
	if primary.Abstract == "" {
		primary.Abstract = secondary.Abstract
	}
	if primary.Journal == "" {
		primary.Journal = secondary.Journal
	}
	if secondary.UpdatedAt.After(primary.UpdatedAt) {
		primary.UpdatedAt = secondary.UpdatedAt
	}
	return primary
}

func FuzzyTitleSimilarity(a, b string) float64 {
	a, b = (&Item{Title: a}).NormalizedTitle(), (&Item{Title: b}).NormalizedTitle()
	if a == b && a != "" {
		return 1
	}
	ga, gb := trigrams(a), trigrams(b)
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	intersection := 0
	for token := range ga {
		if _, ok := gb[token]; ok {
			intersection++
		}
	}
	union := len(ga) + len(gb) - intersection
	return math.Max(0, float64(intersection)/float64(union))
}

func trigrams(s string) map[string]struct{} {
	s = strings.ReplaceAll(s, " ", "")
	r := []rune(s)
	out := make(map[string]struct{})
	if len(r) < 3 {
		if len(r) > 0 {
			out[string(r)] = struct{}{}
		}
		return out
	}
	for i := 0; i+3 <= len(r); i++ {
		out[string(r[i:i+3])] = struct{}{}
	}
	return out
}
