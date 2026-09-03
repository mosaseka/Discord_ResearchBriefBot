package evidence

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/textutil"
)

type Level string

const (
	High   Level = "高"
	Medium Level = "中"
	Low    Level = "低"
)

type Features struct {
	Randomized, RCT, Prospective, Retrospective, Participants, Patients, Simulation, Synthetic bool
	Benchmark, Clinician, ExpertEvaluation, HumanEvaluation, ExternalValidation, Validated     bool
	SystematicReview, MetaAnalysis, Longitudinal, Pilot, Feasibility                           bool
	Comparative, LLMJudge, Prototype, Conceptual                                               bool
	SampleSize                                                                                 int
}

type Assessment struct {
	Level    Level
	Reason   string
	Features Features
}

var samplePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bN\s*=\s*([0-9]{1,7})\b`),
	regexp.MustCompile(`(?i)\b([0-9]{1,7})\s+(?:participants?|patients?|subjects?)\b`),
}

func Analyze(item article.Item) Assessment {
	t := textutil.Comparison(strings.Join(append([]string{item.Abstract}, item.PublicationTypes...), " "))
	f := Features{
		Randomized:  has(t, "randomized", "randomised"),
		RCT:         has(t, "randomized controlled trial", "randomised controlled trial", " rct "),
		Prospective: has(t, "prospective"), Retrospective: has(t, "retrospective"),
		Participants: has(t, "participant"), Patients: has(t, "patient"),
		Simulation: has(t, "simulation", "simulator"), Synthetic: has(t, "synthetic"),
		Benchmark: has(t, "benchmark"), Clinician: has(t, "clinician"),
		ExpertEvaluation: has(t, "expert evaluation", "expert review"), HumanEvaluation: has(t, "human evaluation"),
		ExternalValidation: has(t, "external validation", "externally validated"),
		Validated:          has(t, "validated", "validation"),
		SystematicReview:   has(t, "systematic review"), MetaAnalysis: has(t, "meta analysis"),
		Longitudinal: has(t, "longitudinal"), Pilot: has(t, "pilot"), Feasibility: has(t, "feasibility"),
		Comparative: has(t, "compare", "comparison", "baseline", "outperform"),
		LLMJudge:    has(t, "llm as a judge", "llm judge"),
		Prototype:   has(t, "prototype"), Conceptual: has(t, "conceptual", "framework proposal"),
	}
	for _, pattern := range samplePatterns {
		for _, match := range pattern.FindAllStringSubmatch(item.Abstract, -1) {
			n, _ := strconv.Atoi(match[1])
			if n > f.SampleSize {
				f.SampleSize = n
			}
		}
	}
	peer := item.PeerReviewed
	level := Low
	switch {
	case f.SystematicReview || f.MetaAnalysis:
		level = High
	case (f.Pilot || f.Feasibility) && (f.SampleSize == 0 || f.SampleSize < 50):
		level = Low
	case f.LLMJudge && !f.HumanEvaluation && !f.Clinician && !f.ExternalValidation:
		level = Low
	case (f.Prototype || f.Conceptual) && !f.ExternalValidation:
		level = Low
	case f.RCT || (f.Randomized && has(t, "trial")):
		level = High
	case peer && f.SampleSize >= 200 && (f.Participants || f.Patients):
		level = High
	case peer && (f.ExternalValidation || (f.Clinician && f.Comparative)):
		level = High
	case !f.Synthetic && ((peer && (f.Prospective || f.Retrospective || f.Participants || f.Patients)) || f.Benchmark || f.ExpertEvaluation || f.HumanEvaluation || (f.Simulation && f.Validated) || (f.SampleSize > 0 && (f.Participants || f.Patients) && f.Comparative)):
		level = Medium
	}
	return Assessment{Level: level, Reason: reason(item, f, level, peer), Features: f}
}

func reason(item article.Item, f Features, level Level, peer bool) string {
	parts := make([]string, 0, 4)
	if peer {
		parts = append(parts, "publication metadata上で査読文献として扱う資料。")
	} else if item.SourceType == article.SourceArXiv {
		parts = append(parts, "arXiv preprint。")
	} else {
		parts = append(parts, "公開metadata上で査読済みとは確認できない資料。")
	}
	switch {
	case f.RCT:
		parts = append(parts, "ランダム化比較試験がabstract/metadataに明記。")
	case f.Randomized:
		parts = append(parts, "randomized studyがabstract/metadataに明記。")
	case f.SystematicReview:
		parts = append(parts, "systematic reviewがabstract/metadataに明記。")
	case f.MetaAnalysis:
		parts = append(parts, "meta-analysisがabstract/metadataに明記。")
	case f.ExternalValidation:
		parts = append(parts, "external validationが明記。")
	case f.Retrospective:
		parts = append(parts, "retrospective studyが明記。")
	case f.Prospective:
		parts = append(parts, "prospective studyが明記。")
	case f.Pilot || f.Feasibility:
		parts = append(parts, "pilot / feasibility studyと明記。")
	case f.Simulation && f.Validated:
		parts = append(parts, "validationを伴うsimulation / simulator評価が明記。")
	case f.Simulation:
		parts = append(parts, "simulation / simulator上の評価が明記。")
	case f.Benchmark:
		parts = append(parts, "benchmark評価が明記。")
	default:
		parts = append(parts, "abstractから強い臨床検証設計を確認できない。")
	}
	if f.SampleSize > 0 {
		parts = append(parts, fmt.Sprintf("明記された標本数はN=%d。", f.SampleSize))
	}
	if f.Synthetic {
		parts = append(parts, "synthetic dataの使用が明記。")
	}
	if f.LLMJudge {
		parts = append(parts, "LLM-as-a-judgeの使用が明記。")
	}
	return string(level) + " — " + strings.Join(parts, "")
}

func has(text string, needles ...string) bool {
	text = " " + text + " "
	for _, needle := range needles {
		if strings.Contains(text, textutil.Comparison(needle)) || strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
