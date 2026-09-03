package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/collector"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/discord"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/storage"
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

type fakePublisher struct{ calls int }

func (f *fakePublisher) Post(context.Context, string, discord.Payload) (string, error) {
	f.calls++
	return "message", nil
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
