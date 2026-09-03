package researchrelevance

import (
	"strings"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/textutil"
)

type Analysis struct {
	Relevant bool
	Score    int
	Topics   []string
}

type term struct {
	value  string
	weight int
	topic  string
}

var technical = []term{
	{"offline reinforcement learning", 9, "offline RL"}, {"constrained reinforcement learning", 9, "safe / constrained RL"},
	{"safe reinforcement learning", 9, "safe / constrained RL"}, {"contextual bandit", 9, "JITAI / intervention timing"},
	{"multi armed bandit", 8, "JITAI / intervention timing"}, {"reinforcement learning", 8, "reinforcement learning"},
	{"patient simulator", 9, "patient simulation"}, {"patient simulation", 9, "patient simulation"},
	{"just in time adaptive intervention", 10, "JITAI / intervention timing"}, {"jitai", 10, "JITAI / intervention timing"},
	{"intervention timing", 8, "JITAI / intervention timing"}, {"adaptive intervention", 6, "JITAI / intervention timing"},
	{"longitudinal personalization", 9, "longitudinal personalization"}, {"digital phenotyping", 7, "digital phenotyping"},
	{"multi agent", 6, "multi-agent"}, {"conversational agent", 5, "mental-health agent"},
	{"agentic ai", 4, "mental-health agent"}, {"agent memory", 7, "longitudinal personalization"},
	{"強化学習", 8, "reinforcement learning"}, {"文脈付きバンディット", 9, "JITAI / intervention timing"},
	{"患者シミュレータ", 9, "patient simulation"}, {"適応的介入", 6, "JITAI / intervention timing"},
}

var mental = []string{
	"mental health", "psychiatry", "psychiatric", "depression", "anxiety", "psychotherapy", "cognitive behavioral therapy", "cbt", "psychological", "wellbeing", "well being", "suicide", "self harm",
	"メンタルヘルス", "精神保健", "精神医学", "うつ", "不安症", "心理療法", "認知行動療法", "自殺", "自傷",
}

func Analyze(item article.Item) Analysis {
	title := textutil.Comparison(item.Title)
	body := textutil.Comparison(item.Title + " " + item.Abstract)
	mentalHits := 0
	for _, word := range mental {
		if strings.Contains(body, textutil.Comparison(word)) {
			mentalHits++
		}
	}
	technicalScore := 0
	topics := make(map[string]struct{})
	for _, candidate := range technical {
		normalized := textutil.Comparison(candidate.value)
		if strings.Contains(body, normalized) {
			technicalScore += candidate.weight
			if strings.Contains(title, normalized) {
				technicalScore += candidate.weight / 2
			}
			topics[candidate.topic] = struct{}{}
		}
	}
	// A bare "agent" is weak evidence and is accepted only with another
	// technical concept. This blocks generic mental-health chatbot material.
	if strings.Contains(body, " agent ") || strings.HasPrefix(body, "agent ") {
		technicalScore += 2
	}
	relevant := mentalHits > 0 && technicalScore >= 6
	score := 0
	if relevant {
		score = min(45, 12+mentalHits*3+technicalScore)
		if item.Language == "ja" || strings.HasPrefix(item.Language, "ja-") {
			score += 3
		}
	}
	order := []string{"patient simulation", "JITAI / intervention timing", "offline RL", "safe / constrained RL", "longitudinal personalization", "multi-agent", "digital phenotyping", "mental-health agent", "reinforcement learning"}
	list := make([]string, 0, len(topics))
	for _, topic := range order {
		if _, ok := topics[topic]; ok {
			list = append(list, topic)
		}
	}
	return Analysis{Relevant: relevant, Score: score, Topics: list}
}
