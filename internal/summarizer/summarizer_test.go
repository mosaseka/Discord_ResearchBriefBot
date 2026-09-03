package summarizer

import (
	"strings"
	"testing"
)

func TestExtractIsDeterministicAndExtractive(t *testing.T) {
	abstract := "We propose a contextual bandit for mental health care. We evaluate it with 80 participants. Results show improved adherence. Background text is omitted."
	a := Extract(abstract, 700)
	b := Extract(abstract, 700)
	if a != b {
		t.Fatal("not deterministic")
	}
	for _, sentence := range strings.Split(a, ". ") {
		sentence = strings.TrimSuffix(sentence, ".")
		if sentence != "" && !strings.Contains(abstract, sentence) {
			t.Fatalf("invented text %q", sentence)
		}
	}
}
