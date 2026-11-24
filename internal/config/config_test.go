package config

import (
	"strings"
	"testing"
)

func TestGeminiEnvironment(t *testing.T) {
	t.Setenv("APP_TIMEZONE", "UTC")
	t.Setenv("BRIEF_CRON", DefaultCron)
	t.Setenv("LOG_LEVEL", "info")
	mode := Mode{Once: true, DryRun: true}
	for _, tc := range []struct {
		name, flag, key  string
		fixture, enabled bool
	}{
		{name: "opt in required", key: "test-placeholder"},
		{name: "enabled for dry run", flag: "true", key: "test-placeholder", enabled: true},
		{name: "missing key allowed", flag: "true", enabled: true},
		{name: "fixture ignores opt in and key", flag: "true", key: "test-placeholder", fixture: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GEMINI_ENABLED", tc.flag)
			t.Setenv("GEMINI_API_KEY", tc.key)
			mode := mode
			if tc.fixture {
				mode.Fixture = "testdata"
			}
			cfg, err := Load(mode)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.GeminiEnabled != tc.enabled || (cfg.GeminiAPIKey != "") != (tc.enabled && tc.key != "") {
				t.Fatal("unexpected Gemini configuration")
			}
		})
	}
}

func TestDecodeFeeds(t *testing.T) {
	feeds, err := DecodeFeeds(strings.NewReader("feeds:\n  - name: Research\n    url: https://example.org/feed.xml\n    enabled: true\n    weight: 3\n    language: ja\n    category: institute\n    type: technical_report\n"))
	if err != nil || len(feeds) != 1 || !feeds[0].Enabled {
		t.Fatalf("feeds=%v err=%v", feeds, err)
	}
}
func TestDecodeFeedsRejectsHTTP(t *testing.T) {
	_, err := DecodeFeeds(strings.NewReader("feeds:\n  - name: x\n    url: http://127.0.0.1/x\n    enabled: true\n    weight: 1\n    language: en\n    category: x\n    type: research_feed\n"))
	if err == nil {
		t.Fatal("expected HTTP rejection")
	}
}

func TestDecodeFeedsRejectsDuplicate(t *testing.T) {
	_, err := DecodeFeeds(strings.NewReader("feeds:\n  - name: Research\n    url: https://example.org/a.xml\n    enabled: true\n    weight: 1\n    language: en\n    category: x\n    type: research_feed\n  - name: Research\n    url: https://example.org/b.xml\n    enabled: false\n    weight: 1\n    language: en\n    category: x\n    type: research_feed\n"))
	if err == nil {
		t.Fatal("expected duplicate rejection")
	}
}
