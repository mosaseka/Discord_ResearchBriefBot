package evidence

import (
	"strings"
	"testing"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
)

func TestClassificationAndSampleSize(t *testing.T) {
	tests := []struct {
		name, abstract string
		peer           bool
		want           Level
	}{
		{"rct", "A randomized controlled trial enrolled N=240 participants.", true, High},
		{"retrospective", "A retrospective study of 80 patients was evaluated.", true, Medium},
		{"simulation", "A prototype was tested using synthetic simulation only.", false, Low},
		{"synthetic benchmark", "A benchmark uses synthetic mental health data only.", false, Low},
		{"validated simulation", "A validated patient simulator compares two policies.", false, Medium},
		{"small pilot", "A pilot study evaluated 20 patients on a benchmark.", true, Low},
		{"llm judge only", "A benchmark uses LLM-as-a-judge evaluation only.", false, Low},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Analyze(article.Item{Abstract: test.abstract, PeerReviewed: test.peer, SourceType: article.SourceArXiv})
			if got.Level != test.want {
				t.Fatalf("got %s: %s", got.Level, got.Reason)
			}
			if test.name == "rct" && (got.Features.SampleSize != 240 || !strings.Contains(got.Reason, "N=240")) {
				t.Fatalf("sample not reported: %+v", got)
			}
		})
	}
}
