package discord

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/ranking"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/security"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/textutil"
)

const (
	MaxMessageContent   = 2000
	MaxEmbedsPerMessage = 10
	MaxEmbedTotal       = 6000
	MaxEmbedTitle       = 256
	MaxEmbedDescription = 4096
	MaxFieldName        = 256
	MaxFieldValue       = 1024
	MaxEmbedURL         = 2048
)

type Field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}
type Footer struct {
	Text string `json:"text"`
}
type Embed struct {
	Title       string  `json:"title"`
	URL         string  `json:"url,omitempty"`
	Description string  `json:"description,omitempty"`
	Color       int     `json:"color,omitempty"`
	Fields      []Field `json:"fields,omitempty"`
	Footer      *Footer `json:"footer,omitempty"`
}
type AllowedMentions struct {
	Parse       []string `json:"parse"`
	Users       []string `json:"users,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	RepliedUser bool     `json:"replied_user"`
}
type Payload struct {
	Content         string          `json:"content,omitempty"`
	Embeds          []Embed         `json:"embeds,omitempty"`
	AllowedMentions AllowedMentions `json:"allowed_mentions"`
	Nonce           string          `json:"nonce,omitempty"`
	EnforceNonce    bool            `json:"enforce_nonce,omitempty"`
}

func Build(date string, scopeDays int, items []ranking.Ranked) ([]Payload, error) {
	header := fmt.Sprintf("# 🧠 Mental-Health RL / Agent Brief\n%s\n\n探索範囲:\n直近7日のみ", date)
	if scopeDays > 7 {
		header = fmt.Sprintf("# 🧠 Mental-Health RL / Agent Brief\n%s\n\n探索範囲:\n直近7日では候補不足のため、直近30日まで拡張", date)
	}
	if len(items) == 0 {
		header += "\n\n対象期間に通知可能な新規資料はありませんでした。"
	}
	header = textutil.SuppressMentions(textutil.Truncate(header, MaxMessageContent))
	embeds := make([]Embed, 0, len(items))
	for idx, item := range items {
		e := Embed{Title: textutil.SuppressMentions(textutil.Truncate(fmt.Sprintf("%d. %s", idx+1, item.Item.Title), MaxEmbedTitle)), URL: safeURL(item.Item.URL), Description: textutil.SuppressMentions(textutil.Truncate(fmt.Sprintf("**公開:** %s\n**種別:** %s\n**Source:** %s", item.Item.PublishedAt.Format("2006-01-02"), item.Item.SourceType, item.Item.DisplaySource()), 700)), Color: 0x5865F2, Fields: []Field{{Name: "貢献", Value: textutil.SuppressMentions(textutil.Truncate(item.Contribution, MaxFieldValue))}, {Name: "エビデンス品質", Value: textutil.SuppressMentions(textutil.Truncate(item.Evidence.Reason, MaxFieldValue))}, {Name: "学部研究への意味", Value: textutil.SuppressMentions(textutil.Truncate(item.UndergraduateMeaning, MaxFieldValue))}}, Footer: &Footer{Text: fmt.Sprintf("deterministic score: %d (relevance %d / source %d / evidence %d / freshness %d / technical %d)", item.Breakdown.Total(), item.Breakdown.Relevance, item.Breakdown.SourceQuality, item.Breakdown.Evidence, item.Breakdown.Freshness, item.Breakdown.TechnicalRelevance)}}
		if err := ValidateEmbed(e); err != nil {
			return nil, err
		}
		embeds = append(embeds, e)
	}
	signal := Embed{Title: "今週の研究シグナル", Description: signalText(items), Color: 0x57F287}
	if err := ValidateEmbed(signal); err != nil {
		return nil, err
	}
	embeds = append(embeds, signal)
	base := Payload{Content: header, AllowedMentions: noMentions()}
	var payloads []Payload
	current := base
	characters := 0
	for _, embed := range embeds {
		count := EmbedUnits(embed)
		if len(current.Embeds) > 0 && (len(current.Embeds) >= MaxEmbedsPerMessage || characters+count > MaxEmbedTotal) {
			payloads = append(payloads, current)
			current = Payload{AllowedMentions: noMentions()}
			characters = 0
		}
		current.Embeds = append(current.Embeds, embed)
		characters += count
	}
	payloads = append(payloads, current)
	return payloads, nil
}

func ValidateEmbed(e Embed) error {
	if textutil.Units(e.Title) > MaxEmbedTitle || textutil.Units(e.Description) > MaxEmbedDescription {
		return errors.New("embed title or description exceeds Discord limit")
	}
	if len(e.Fields) > 25 {
		return errors.New("too many embed fields")
	}
	for _, f := range e.Fields {
		if textutil.Units(f.Name) > MaxFieldName || textutil.Units(f.Value) > MaxFieldValue {
			return errors.New("embed field exceeds Discord limit")
		}
	}
	if EmbedUnits(e) > MaxEmbedTotal {
		return errors.New("embed exceeds Discord total")
	}
	return nil
}
func EmbedUnits(e Embed) int {
	total := textutil.Units(e.Title) + textutil.Units(e.Description)
	for _, f := range e.Fields {
		total += textutil.Units(f.Name) + textutil.Units(f.Value)
	}
	if e.Footer != nil {
		total += textutil.Units(e.Footer.Text)
	}
	return total
}
func noMentions() AllowedMentions {
	return AllowedMentions{Parse: []string{}, Users: []string{}, Roles: []string{}, RepliedUser: false}
}

func signalText(items []ranking.Ranked) string {
	counts := map[string]int{}
	for _, item := range items {
		for _, topic := range item.Topics {
			counts[topic]++
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	if len(keys) == 0 {
		b.WriteString("- 該当なし")
		return b.String()
	}
	for index, key := range keys {
		if index > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "- %s: %d件", key, counts[key])
	}
	return b.String()
}
func safeURL(raw string) string {
	if textutil.Units(raw) > MaxEmbedURL {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && !security.IsPublicIP(ip) {
		return ""
	}
	return u.String()
}

func FormatMarkdown(date string, scopeDays int, items []ranking.Ranked) string {
	payloads, _ := Build(date, scopeDays, items)
	var b strings.Builder
	for _, payload := range payloads {
		if payload.Content != "" {
			b.WriteString(payload.Content)
			b.WriteString("\n\n")
		}
		for _, embed := range payload.Embeds {
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", embed.Title, embed.Description)
			for _, field := range embed.Fields {
				fmt.Fprintf(&b, "**%s**\n%s\n\n", field.Name, field.Value)
			}
			if embed.URL != "" {
				fmt.Fprintf(&b, "🔗 %s\n\n", embed.URL)
			}
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}
