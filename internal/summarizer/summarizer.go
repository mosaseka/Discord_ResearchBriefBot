package summarizer

import (
	"regexp"
	"sort"
	"strings"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/textutil"
)

var sentenceBoundary = regexp.MustCompile(`(?:[.!?。！？])(?:\s+|$)`)

type scored struct {
	index int
	text  string
	kind  int
	score int
}

var cues = [][]string{
	{"aim", "objective", "purpose", "we investigate", "we study", "we propose", "本研究", "目的"},
	{"method", "we use", "we develop", "we evaluate", "participants", "dataset", "simulation", "方法"},
	{"result", "found", "show", "demonstrate", "improv", "outperform", "significant", "結果"},
}

func Extract(abstract string, maxUnits int) string {
	abstract = textutil.Clean(abstract)
	if abstract == "" {
		return "Abstractに要約可能な記述がありません。"
	}
	sentences := split(abstract)
	candidates := make([]scored, 0, len(sentences)*3)
	for idx, sentence := range sentences {
		normalized := textutil.Comparison(sentence)
		for kind, words := range cues {
			score := 0
			for _, word := range words {
				if strings.Contains(normalized, textutil.Comparison(word)) {
					score += 5
				}
			}
			for _, topic := range []string{"reinforcement learning", "bandit", "agent", "mental health", "intervention", "patient", "simulation"} {
				if strings.Contains(normalized, topic) {
					score++
				}
			}
			if kind == 0 {
				score += max(0, 3-idx)
			}
			candidates = append(candidates, scored{idx, sentence, kind, score})
		}
	}
	selected := make(map[int]struct{})
	ordered := make([]scored, 0, 3)
	for kind := range cues {
		var choices []scored
		for _, c := range candidates {
			if c.kind == kind {
				choices = append(choices, c)
			}
		}
		sort.SliceStable(choices, func(i, j int) bool {
			if choices[i].score != choices[j].score {
				return choices[i].score > choices[j].score
			}
			return choices[i].index < choices[j].index
		})
		for _, choice := range choices {
			if _, exists := selected[choice.index]; exists {
				continue
			}
			selected[choice.index] = struct{}{}
			ordered = append(ordered, choice)
			break
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].kind < ordered[j].kind })
	parts := make([]string, 0, len(ordered))
	for _, value := range ordered {
		parts = append(parts, value.text)
	}
	return textutil.Truncate(strings.Join(parts, " "), maxUnits)
}

func split(text string) []string {
	indices := sentenceBoundary.FindAllStringIndex(text, -1)
	start := 0
	out := make([]string, 0, len(indices)+1)
	for _, bounds := range indices {
		end := bounds[0] + 1
		if sentence := strings.TrimSpace(text[start:end]); sentence != "" {
			out = append(out, sentence)
		}
		start = bounds[1]
	}
	if tail := strings.TrimSpace(text[start:]); tail != "" {
		out = append(out, tail)
	}
	if len(out) == 0 {
		return []string{text}
	}
	return out
}
