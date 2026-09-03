package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/collector"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/crossref"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/discord"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/ranking"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/storage"
)

const MaxItems = 5

type Publisher interface {
	Post(context.Context, string, discord.Payload) (string, error)
}
type Service struct {
	Sources   []collector.Source
	Crossref  *crossref.Client
	Store     *storage.Store
	Publisher Publisher
	ChannelID string
	Location  *time.Location
	Logger    *slog.Logger
	Now       func() time.Time
}
type Result struct {
	RunID                                string
	ScopeDays, Fetched, Selected, Posted int
	Items                                []ranking.Ranked
	Markdown                             string
	Partial                              bool
}

func (s *Service) Run(ctx context.Context, dryRun bool, requestedType ...string) (result Result, runErr error) {
	var auditErr error
	if s.Store == nil {
		return result, errors.New("storage is required")
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Location == nil {
		s.Location = time.UTC
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	now := s.Now().In(s.Location)
	runType := "once"
	if len(requestedType) > 0 && requestedType[0] == "scheduled" {
		runType = "scheduled"
	}
	if dryRun {
		runType = "dry-run"
	}
	runID, err := s.Store.BeginRun(ctx, runType)
	if err != nil {
		return result, err
	}
	result.RunID = runID
	defer func() {
		status := "success"
		if runErr != nil {
			status = "failure"
		} else if result.Partial {
			status = "partial_success"
		}
		finishErr := s.Store.FinishRun(context.WithoutCancel(ctx), runID, storage.RunResult{ScopeDays: result.ScopeDays, Fetched: result.Fetched, Selected: result.Selected, Posted: result.Posted, Status: status, Err: errors.Join(runErr, auditErr)})
		if runErr == nil && finishErr != nil {
			runErr = finishErr
		}
	}()
	raw, success, collectErr := s.collect(ctx, now.AddDate(0, 0, -7), now, runID, 7)
	raw7, success7, collectErr7 := raw, success, collectErr
	result.Fetched = len(raw)
	ranked := ranking.Rank(article.Dedupe(raw), now)
	available, err := s.unnotified(ctx, ranked)
	if err != nil {
		return result, err
	}
	result.ScopeDays = 7
	if len(available) < MaxItems {
		raw30, success30, collectErr30 := s.collect(ctx, now.AddDate(0, 0, -30), now, runID, 30)
		if success30 || len(raw30) > 0 {
			raw, success, collectErr = raw30, success30, errors.Join(collectErr7, collectErr30)
			result.Fetched = len(raw)
			ranked = ranking.Rank(article.Dedupe(raw), now)
			result.ScopeDays = 30
		} else {
			raw, success, collectErr = raw7, success7, errors.Join(collectErr7, collectErr30)
			result.Partial = true
			result.ScopeDays = 30
		}
	}
	if !success && len(raw) == 0 {
		return result, fmt.Errorf("all collectors failed: %w", collectErr)
	}
	if collectErr != nil {
		result.Partial = true
		auditErr = errors.Join(auditErr, collectErr)
		s.Logger.Warn("one or more collectors failed", "error", collectErr)
	}
	deduped := article.Dedupe(raw)
	if s.Crossref != nil {
		enriched, enrichErr := s.Crossref.Enrich(ctx, deduped)
		deduped = enriched
		if enrichErr != nil {
			result.Partial = true
			auditErr = errors.Join(auditErr, enrichErr)
			s.Logger.Warn("Crossref enrichment was partial", "error", enrichErr)
		}
	}
	ranked = ranking.Rank(deduped, now)
	available, err = s.unnotified(ctx, ranked)
	if err != nil {
		return result, err
	}
	if len(available) > MaxItems {
		available = available[:MaxItems]
	}
	result.Items = available
	result.Selected = len(available)
	selected := make(map[string]bool, len(available))
	for _, item := range available {
		selected[item.Item.Identity()] = true
	}
	if err = s.Store.Record(ctx, ranked, selected); err != nil {
		return result, err
	}
	date := now.Format("2006-01-02")
	result.Markdown = discord.FormatMarkdown(date, result.ScopeDays, available)
	if dryRun {
		return result, nil
	}
	if s.Publisher == nil {
		return result, errors.New("Discord publisher is required")
	}
	payloads, err := discord.Build(date, result.ScopeDays, available)
	if err != nil {
		return result, err
	}
	offset := 0
	for index, payload := range payloads {
		count := min(len(payload.Embeds), len(available)-offset)
		batchItems := make([]article.Item, 0, count)
		for _, item := range available[offset:min(offset+count, len(available))] {
			batchItems = append(batchItems, item.Item)
		}
		offset += count
		nonce := discord.Nonce(s.ChannelID, date, runID, index)
		payload.Nonce = nonce
		payload.EnforceNonce = true
		reservation, err := s.Store.Reserve(ctx, runID, s.ChannelID, nonce, batchItems)
		if err != nil {
			return result, err
		}
		messageID, postErr := s.Publisher.Post(ctx, s.ChannelID, payload)
		if postErr != nil {
			_ = s.Store.MarkFailed(context.WithoutCancel(ctx), reservation.ID, discord.IsAmbiguous(postErr), postErr)
			return result, postErr
		}
		if err = s.Store.MarkSent(context.WithoutCancel(ctx), reservation.ID, messageID); err != nil {
			return result, err
		}
		result.Posted += count
	}
	return result, nil
}

func (s *Service) collect(ctx context.Context, since, until time.Time, runID string, windowDays int) ([]article.Item, bool, error) {
	var out []article.Item
	var joined error
	success := false
	for _, source := range s.Sources {
		items, err := source.Collect(ctx, since, until)
		filtered := items[:0]
		for _, item := range items {
			if !item.PublishedAt.IsZero() && !item.PublishedAt.Before(since) && !item.PublishedAt.After(until) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
		if auditErr := s.Store.RecordSourceFetch(ctx, runID, source.Name(), windowDays, len(items), err); auditErr != nil {
			joined = errors.Join(joined, auditErr)
		}
		if len(items) > 0 || err == nil {
			success = true
		}
		out = append(out, items...)
		if err != nil {
			joined = errors.Join(joined, fmt.Errorf("%s: %w", source.Name(), err))
		}
		if ctx.Err() != nil {
			return out, success, errors.Join(joined, ctx.Err())
		}
	}
	return out, success, joined
}
func (s *Service) unnotified(ctx context.Context, ranked []ranking.Ranked) ([]ranking.Ranked, error) {
	if s.ChannelID == "" {
		return ranked, nil
	}
	out := make([]ranking.Ranked, 0, len(ranked))
	for _, item := range ranked {
		seen, err := s.Store.AlreadyNotified(ctx, s.ChannelID, item.Item)
		if err != nil {
			return nil, err
		}
		if !seen {
			out = append(out, item)
		}
	}
	return out, nil
}
