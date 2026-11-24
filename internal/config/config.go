package config

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTimezone = "Asia/Tokyo"
	DefaultCron     = "0 18 * * 5"
	DefaultDatabase = "data/researchbot.db"
)

type Mode struct {
	Once, DryRun bool
	Fixture      string
}

type Config struct {
	Mode                                           Mode
	DiscordBotToken, DiscordChannelID              string
	Timezone                                       string
	Location                                       *time.Location
	BriefCron, DatabasePath, LogLevel              string
	NCBITool, NCBIEmail, CrossrefMailto, UserAgent string
	GeminiEnabled                                  bool
	GeminiAPIKey                                   string
}

type Feed struct {
	Name, URL, Language, Category string
	Enabled                       bool
	Weight                        int
	Type                          string
}

func Load(mode Mode) (Config, error) {
	if mode.Fixture != "" {
		if !mode.Once {
			return Config{}, errors.New("--fixture requires --once")
		}
		mode.DryRun = true
	}
	if mode.DryRun && !mode.Once {
		return Config{}, errors.New("--dry-run requires --once")
	}
	tz := env("APP_TIMEZONE", DefaultTimezone)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("APP_TIMEZONE: %w", err)
	}
	cron := env("BRIEF_CRON", DefaultCron)
	if len(strings.Fields(cron)) != 5 {
		return Config{}, errors.New("BRIEF_CRON must be a five-field expression")
	}
	level := strings.ToLower(env("LOG_LEVEL", "info"))
	if level != "debug" && level != "info" && level != "warn" && level != "error" {
		return Config{}, errors.New("LOG_LEVEL must be debug, info, warn, or error")
	}
	cfg := Config{Mode: mode, DiscordBotToken: strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")), DiscordChannelID: strings.TrimSpace(os.Getenv("DISCORD_CHANNEL_ID")),
		Timezone: tz, Location: loc, BriefCron: cron, DatabasePath: env("DATABASE_PATH", DefaultDatabase), LogLevel: level,
		NCBITool: env("NCBI_TOOL", "discord-research-brief-bot"), NCBIEmail: strings.TrimSpace(os.Getenv("NCBI_EMAIL")),
		CrossrefMailto: strings.TrimSpace(os.Getenv("CROSSREF_MAILTO")), UserAgent: env("HTTP_USER_AGENT", "DiscordResearchBriefBot/1.0 (+https://github.com/mosaseka/Discord_ResearchBriefBot)")}
	cfg.GeminiEnabled = mode.Fixture == "" && strings.EqualFold(env("GEMINI_ENABLED", "false"), "true")
	if cfg.GeminiEnabled {
		cfg.GeminiAPIKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}
	if !mode.DryRun {
		if cfg.DiscordBotToken == "" || cfg.DiscordChannelID == "" {
			return Config{}, errors.New("DISCORD_BOT_TOKEN and DISCORD_CHANNEL_ID are required when posting")
		}
		channelID, err := strconv.ParseUint(cfg.DiscordChannelID, 10, 64)
		if err != nil || channelID == 0 {
			return Config{}, errors.New("DISCORD_CHANNEL_ID must be a Discord snowflake")
		}
	}
	return cfg, nil
}

func LoadFeeds(path string) ([]Feed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open sources config: %w", err)
	}
	defer f.Close()
	return DecodeFeeds(f)
}

// DecodeFeeds intentionally supports the small documented YAML subset only:
// a top-level `feeds` sequence whose entries contain scalar key/value pairs.
func DecodeFeeds(r io.Reader) ([]Feed, error) {
	scanner := bufio.NewScanner(r)
	var out []Feed
	current := map[string]string(nil)
	flush := func() error {
		if current == nil {
			return nil
		}
		feed, err := decodeFeed(current)
		if err != nil {
			return err
		}
		out = append(out, feed)
		return nil
	}
	seenRoot := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "feeds:" {
			seenRoot = true
			continue
		}
		if !seenRoot {
			return nil, errors.New("sources config must start with feeds:")
		}
		if strings.HasPrefix(line, "-") {
			if err := flush(); err != nil {
				return nil, err
			}
			current = make(map[string]string)
			line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
			if line == "" {
				continue
			}
		}
		if current == nil {
			return nil, errors.New("feed property outside a list item")
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid sources line %q", line)
		}
		key, value = strings.TrimSpace(key), unquote(strings.TrimSpace(value))
		if _, exists := current[key]; exists {
			return nil, fmt.Errorf("duplicate feed field %q", key)
		}
		current[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("sources config has no feeds")
	}
	names, urls := make(map[string]bool), make(map[string]bool)
	for _, feed := range out {
		nameKey, urlKey := strings.ToLower(feed.Name), strings.ToLower(feed.URL)
		if names[nameKey] || urls[urlKey] {
			return nil, fmt.Errorf("duplicate feed name or URL: %q", feed.Name)
		}
		names[nameKey], urls[urlKey] = true, true
	}
	return out, nil
}

func decodeFeed(values map[string]string) (Feed, error) {
	known := map[string]bool{"name": true, "url": true, "enabled": true, "weight": true, "language": true, "category": true, "type": true}
	for key := range values {
		if !known[key] {
			return Feed{}, fmt.Errorf("unknown feed field %q", key)
		}
	}
	enabled, err := strconv.ParseBool(values["enabled"])
	if err != nil {
		return Feed{}, errors.New("feed enabled must be true or false")
	}
	weight, err := strconv.Atoi(values["weight"])
	if err != nil || weight < 1 || weight > 5 {
		return Feed{}, errors.New("feed weight must be 1..5")
	}
	f := Feed{Name: values["name"], URL: values["url"], Enabled: enabled, Weight: weight, Language: values["language"], Category: values["category"], Type: values["type"]}
	if f.Name == "" || f.URL == "" || f.Language == "" || f.Category == "" || f.Type == "" {
		return Feed{}, errors.New("feed has an empty required field")
	}
	u, err := url.Parse(f.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return Feed{}, fmt.Errorf("feed %q must use a valid HTTPS URL", f.Name)
	}
	if f.Type != "technical_report" && f.Type != "research_feed" {
		return Feed{}, fmt.Errorf("feed %q has unsupported type", f.Name)
	}
	return f, nil
}

func unquote(s string) string {
	if len(s) >= 2 && ((s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')) {
		return s[1 : len(s)-1]
	}
	return s
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
