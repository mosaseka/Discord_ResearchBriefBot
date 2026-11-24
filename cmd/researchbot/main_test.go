package main

import (
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/config"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/httpclient"
)

func TestFixtureSkipsHTTPWithGeminiEnabled(t *testing.T) {
	previousLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	t.Setenv("GEMINI_ENABLED", "true")
	t.Setenv("GEMINI_API_KEY", "fixture-placeholder")
	t.Setenv("APP_TIMEZONE", "UTC")
	t.Setenv("BRIEF_CRON", config.DefaultCron)
	t.Setenv("LOG_LEVEL", "error")
	mode := config.Mode{Once: true, Fixture: filepath.Join("..", "..", "testdata")}
	newHTTPClient := func(httpclient.Config) (*httpclient.Client, error) {
		t.Error("fixture attempted to create an outbound HTTP client")
		return nil, errors.New("outbound HTTP is unavailable")
	}
	if code := run(mode, filepath.Join(t.TempDir(), "missing-sources.yaml"), newHTTPClient); code != 0 {
		t.Fatalf("fixture exit code = %d", code)
	}
}
