package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/app"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/arxiv"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/collector"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/config"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/crossref"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/discord"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/feed"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/fixture"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/httpclient"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/pubmed"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/scheduler"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/storage"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/summarizer"
)

func main() {
	var mode config.Mode
	var sourcesPath string
	flag.BoolVar(&mode.Once, "once", false, "collect and publish once, then exit")
	flag.BoolVar(&mode.DryRun, "dry-run", false, "print the briefing without posting to Discord")
	flag.StringVar(&mode.Fixture, "fixture", "", "offline fixture directory; requires --once and forces --dry-run")
	flag.StringVar(&sourcesPath, "config", "config/sources.yaml", "RSS/Atom sources configuration")
	flag.Parse()
	os.Exit(run(mode, sourcesPath, httpclient.New))
}

func run(mode config.Mode, sourcesPath string, newHTTPClient func(httpclient.Config) (*httpclient.Client, error)) int {
	cfg, err := config.Load(mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		return 2
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var sources []collector.Source
	var crossrefClient *crossref.Client
	var httpClient *httpclient.Client
	var generator app.Summarizer
	now := time.Now
	databasePath := cfg.DatabasePath
	if cfg.Mode.Fixture != "" {
		fixtureSource, fixtureNow, loadErr := fixture.Load(cfg.Mode.Fixture)
		if loadErr != nil {
			logger.Error("load fixture", "error", loadErr)
			return 2
		}
		sources = []collector.Source{fixtureSource}
		now = func() time.Time { return fixtureNow }
		databasePath = ":memory:"
	} else {
		feeds, loadErr := config.LoadFeeds(sourcesPath)
		if loadErr != nil {
			logger.Error("load sources", "error", loadErr)
			return 2
		}
		httpClient, err = newHTTPClient(httpclient.Config{UserAgent: cfg.UserAgent})
		if err != nil {
			logger.Error("create HTTP client", "error", err)
			return 1
		}
		defer httpClient.CloseIdleConnections()
		sources = []collector.Source{&arxiv.Client{HTTP: httpClient}, &pubmed.Client{HTTP: httpClient, Tool: cfg.NCBITool, Email: cfg.NCBIEmail}}
		for _, configured := range feeds {
			if configured.Enabled {
				sources = append(sources, &feed.Client{HTTP: httpClient, Feeds: []config.Feed{configured}})
			}
		}
		crossrefClient = &crossref.Client{HTTP: httpClient, Mailto: cfg.CrossrefMailto}
		if cfg.GeminiEnabled && cfg.GeminiAPIKey != "" {
			generator = summarizer.NewGemini(httpClient, cfg.GeminiAPIKey)
		}
	}
	store, err := storage.Open(ctx, databasePath)
	if err != nil {
		logger.Error("open storage", "error", err)
		return 1
	}
	defer store.Close()
	var publisher app.Publisher
	if !cfg.Mode.DryRun {
		publisher = discord.New(cfg.DiscordBotToken)
	}
	service := &app.Service{Sources: sources, Crossref: crossrefClient, Store: store, Publisher: publisher,
		Summarizer: generator, ChannelID: cfg.DiscordChannelID, Location: cfg.Location, Logger: logger, Now: now}
	runType := "scheduled"
	if cfg.Mode.Once {
		runType = "once"
	}
	runOnce := func(parent context.Context) error {
		runCtx, cancel := context.WithTimeout(parent, 3*time.Minute)
		defer cancel()
		result, runErr := service.Run(runCtx, cfg.Mode.DryRun, runType)
		if cfg.Mode.DryRun {
			fmt.Print(result.Markdown)
		}
		if runErr == nil {
			logger.Info("brief completed", "scope_days", result.ScopeDays, "fetched", result.Fetched, "selected", result.Selected, "posted", result.Posted, "partial", result.Partial)
		}
		return runErr
	}
	if cfg.Mode.Once {
		if err = runOnce(ctx); err != nil {
			logger.Error("brief failed", "error", err)
			return 1
		}
		return 0
	}
	runner, err := scheduler.New(cfg.BriefCron, cfg.Location)
	if err != nil {
		logger.Error("parse schedule", "error", err)
		return 2
	}
	logger.Info("scheduler started", "cron", cfg.BriefCron, "timezone", cfg.Timezone)
	scheduledJob := func(parent context.Context) error {
		if jobErr := runOnce(parent); jobErr != nil {
			logger.Error("scheduled brief failed", "error", jobErr)
		}
		return nil
	}
	if err = runner.Run(ctx, scheduledJob); err != nil && ctx.Err() == nil {
		logger.Error("scheduler stopped", "error", err)
		return 1
	}
	return 0
}

func newLogger(level string) *slog.Logger {
	var value slog.Level
	switch level {
	case "debug":
		value = slog.LevelDebug
	case "warn":
		value = slog.LevelWarn
	case "error":
		value = slog.LevelError
	default:
		value = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: value}))
}
