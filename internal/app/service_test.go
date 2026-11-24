package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/collector"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/discord"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/storage"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/summarizer"
)

type fakeSource struct {
	items []article.Item
	err   error
	calls int
}

func (f *fakeSource) Name() string { return "fake" }
func (f *fakeSource) Collect(context.Context, time.Time, time.Time) ([]article.Item, error) {
	f.calls++
	return append([]article.Item(nil), f.items...), f.err
}

type fakePublisher struct {
	calls    int
	err      error
	payloads []discord.Payload
}

func (f *fakePublisher) Post(_ context.Context, _ string, payload discord.Payload) (string, error) {
	f.calls++
	f.payloads = append(f.payloads, payload)
	if f.err != nil {
		return "", f.err
	}
	return "message", nil
}

type fakeSummarizer func(context.Context, article.Item) (summarizer.Generated, error)

func (f fakeSummarizer) Summarize(ctx context.Context, item article.Item) (summarizer.Generated, error) {
	return f(ctx, item)
}

func TestGenerationRunsOnceForEachFinalSelection(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	notified := makeItem("Previously delivered", now)
	source := &fakeSource{items: []article.Item{notified}}
	publisher := &fakePublisher{}
	var logs bytes.Buffer
	service := &Service{Sources: toSources(source), Store: openStore(t), Publisher: publisher,
		ChannelID: "123", Now: func() time.Time { return now }, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	if _, err := service.Run(ctx, false); err != nil {
		t.Fatal(err)
	}
	publisher.payloads = nil
	for i := 1; i <= 7; i++ {
		item := makeItem(fmt.Sprintf("Paper %d", i), now.AddDate(0, 0, -i-3))
		item.DOI = fmt.Sprintf("10.0000/fixture-%d", i)
		item.URL = fmt.Sprintf("https://example.org/paper-%d", i)
		source.items = append(source.items, item)
	}
	source.items = append(source.items, source.items[1])
	var calls []string
	service.Summarizer = fakeSummarizer(func(_ context.Context, item article.Item) (summarizer.Generated, error) {
		calls = append(calls, item.Title)
		if item.Title == "Paper 2" {
			return summarizer.Generated{}, errors.New("untrusted response marker")
		}
		return summarizer.Generated{Findings: "目的：介入時期の評価 @everyone", Application: "JITAI の比較実験"}, nil
	})

	preview, err := service.Run(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"Paper 1", "Paper 2", "Paper 3", "Paper 4", "Paper 5"}
	if !slices.Equal(calls, wantCalls) || preview.ScopeDays != 30 || len(publisher.payloads) != 0 {
		t.Fatalf("preview calls=%v, scope=%d, payloads=%d", calls, preview.ScopeDays, len(publisher.payloads))
	}
	calls = nil
	posted, err := service.Run(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, wantCalls) || posted.Posted != MaxItems || posted.Markdown != preview.Markdown {
		t.Fatalf("posting calls=%v, posted=%d, same Markdown=%v", calls, posted.Posted, posted.Markdown == preview.Markdown)
	}
	if got := discord.FormatMarkdown(publisher.payloads); got != preview.Markdown {
		t.Fatal("published embeds and preview contain different summaries")
	}
	for _, fragment := range []string{"**要約方式:** Gemini 生成", "**要約方式:** 抽出型", "**目的・方法・主な結果**", "**学部研究への応用案**",
		"JITAI の比較実験", "We evaluate a contextual bandit", "**DOI:** 10.0000/fixture-1", "https://example.org/paper-1", "2026-10-01"} {
		if !strings.Contains(preview.Markdown, fragment) {
			t.Errorf("preview missing %q", fragment)
		}
	}
	if strings.Contains(preview.Markdown, "@everyone") || strings.Contains(logs.String(), "untrusted response marker") {
		t.Fatal("unsuppressed mention or raw error reached output")
	}
}

func TestRetryAfterDefiniteDiscordRejection(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	source := &fakeSource{items: []article.Item{makeItem("contextual bandit for mental health", now)}}
	publisher := &fakePublisher{err: &discord.HTTPError{Status: 403}}
	service := &Service{Sources: toSources(source), Store: openStore(t), Publisher: publisher, ChannelID: "123", Now: func() time.Time { return now }}
	if _, err := service.Run(context.Background(), false); err == nil {
		t.Fatal("expected rejected Discord request")
	}
	publisher.err = nil
	retried, err := service.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("retry after restoring Discord permissions: %v", err)
	}
	if retried.Posted != 1 || publisher.calls != 2 {
		t.Fatalf("posted=%d publisher calls=%d", retried.Posted, publisher.calls)
	}
	third, err := service.Run(context.Background(), false)
	if err != nil || third.Selected != 0 {
		t.Fatalf("successfully retried item was selected again: selected=%d err=%v", third.Selected, err)
	}
}

func makeItem(title string, published time.Time) article.Item {
	return article.Item{Title: title, Abstract: "We evaluate a contextual bandit for personalized mental health intervention timing with 50 participants. Results show improved adherence.", URL: "https://example.org/" + title, SourceType: article.SourceArXiv, PublishedAt: published, UpdatedAt: published}
}
func openStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSevenToThirtyDayFallbackAndDateFilter(t *testing.T) {
	now := time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)
	source := &fakeSource{items: []article.Item{makeItem("recent contextual bandit mental health", now.AddDate(0, 0, -2)), makeItem("older contextual bandit mental health", now.AddDate(0, 0, -20)), makeItem("stale contextual bandit mental health", now.AddDate(0, 0, -40))}}
	service := &Service{Sources: []collector.Source{source}, Store: openStore(t), Location: time.UTC, Now: func() time.Time { return now }, Logger: slog.Default()}
	result, err := service.Run(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if result.ScopeDays != 30 || source.calls != 2 || result.Selected != 2 {
		t.Fatalf("result=%+v calls=%d", result, source.calls)
	}
}

func TestPartialFailureAndNoResult(t *testing.T) {
	now := time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)
	good := &fakeSource{items: []article.Item{makeItem("contextual bandit for mental health", now)}}
	bad := &fakeSource{err: errors.New("offline")}
	service := &Service{Sources: toSources(good, bad), Store: openStore(t), Now: func() time.Time { return now }}
	result, err := service.Run(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Partial || result.Selected != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	empty := &Service{Sources: toSources(&fakeSource{}), Store: openStore(t), Now: func() time.Time { return now }}
	none, err := empty.Run(context.Background(), true)
	if err != nil || none.Selected != 0 {
		t.Fatalf("none=%+v err=%v", none, err)
	}
}

func TestDuplicateSuppression(t *testing.T) {
	now := time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)
	source := &fakeSource{items: []article.Item{makeItem("contextual bandit for mental health", now)}}
	publisher := &fakePublisher{}
	service := &Service{Sources: toSources(source), Store: openStore(t), Publisher: publisher, ChannelID: "123", Now: func() time.Time { return now }}
	first, err := service.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	source.items[0].UpdatedAt = now.Add(time.Hour)
	third, err := service.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Selected != 1 || second.Selected != 0 || third.Selected != 1 {
		t.Fatalf("first=%d second=%d revised=%d", first.Selected, second.Selected, third.Selected)
	}
}

func TestAllCollectorsFailed(t *testing.T) {
	now := time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC)
	service := &Service{Sources: toSources(&fakeSource{err: errors.New("offline")}), Store: openStore(t), Now: func() time.Time { return now }}
	if _, err := service.Run(context.Background(), true); err == nil {
		t.Fatal("expected all-collector failure")
	}
}

func toSources(values ...*fakeSource) []collector.Source {
	out := make([]collector.Source, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}
