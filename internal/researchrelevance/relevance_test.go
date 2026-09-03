package researchrelevance

import (
	"strings"
	"testing"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
)

func TestAnalyzeRequiresBothDomains(t *testing.T) {
	relevant := Analyze(article.Item{Title: "Contextual bandit for mental health intervention timing"})
	if !relevant.Relevant {
		t.Fatalf("expected relevant: %+v", relevant)
	}
	unrelated := Analyze(article.Item{Title: "Reinforcement learning for warehouse routing"})
	if unrelated.Relevant {
		t.Fatal("generic RL must be rejected")
	}
}
func TestUndergraduateMeaningUsesAtMostTwoTemplates(t *testing.T) {
	text := UndergraduateMeaning([]string{"JITAI / intervention timing", "offline RL", "patient simulation"})
	if !strings.Contains(text, "介入タイミング") || !strings.Contains(text, "既存ログ") || strings.Contains(text, "実患者で探索") {
		t.Fatalf("unexpected template selection: %s", text)
	}
}
