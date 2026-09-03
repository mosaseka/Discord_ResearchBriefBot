package feed

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/config"
)

type Fetcher interface {
	Fetch(context.Context, string, string, []string, int64) ([]byte, error)
}
type Client struct {
	HTTP  Fetcher
	Feeds []config.Feed
}

func (c *Client) Name() string {
	if len(c.Feeds) == 1 {
		return "feed: " + c.Feeds[0].Name
	}
	return "configured feeds"
}

func (c *Client) Collect(ctx context.Context, since, until time.Time) ([]article.Item, error) {
	var out []article.Item
	var joined error
	for _, source := range c.Feeds {
		if !source.Enabled {
			continue
		}
		body, err := c.HTTP.Fetch(ctx, source.URL, "application/atom+xml, application/rss+xml, application/rdf+xml, application/xml, text/xml;q=0.9", []string{"application/atom+xml", "application/rss+xml", "application/rdf+xml", "application/xml", "text/xml"}, 4<<20)
		if err != nil {
			joined = errors.Join(joined, fmt.Errorf("%s: %w", source.Name, err))
			continue
		}
		items, err := Parse(body, source)
		if err != nil {
			joined = errors.Join(joined, fmt.Errorf("%s: %w", source.Name, err))
			continue
		}
		for _, item := range items {
			if !item.PublishedAt.Before(since) && !item.PublishedAt.After(until) {
				out = append(out, item)
			}
		}
	}
	return out, joined
}

type document struct {
	XMLName  xml.Name
	Channel  *rssChannel `xml:"channel"`
	Entries  []atomEntry `xml:"entry"`
	RDFItems []rssItem   `xml:"item"`
}
type rssChannel struct {
	Items []rssItem `xml:"item"`
}
type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	Date        string `xml:"date"`
	DOI         string `xml:"doi"`
}
type atomEntry struct {
	Title     string `xml:"title"`
	Summary   string `xml:"summary"`
	Content   string `xml:"content"`
	Published string `xml:"published"`
	Updated   string `xml:"updated"`
	Links     []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
	} `xml:"link"`
}

func Parse(body []byte, source config.Feed) ([]article.Item, error) {
	var doc document
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse feed XML: %w", err)
	}
	var out []article.Item
	kind := article.SourceResearchFeed
	if source.Type == "technical_report" {
		kind = article.SourceTechnicalReport
	}
	if strings.EqualFold(doc.XMLName.Local, "feed") {
		for _, entry := range doc.Entries {
			link := ""
			for _, candidate := range entry.Links {
				if candidate.Rel == "" || candidate.Rel == "alternate" {
					link = candidate.Href
					break
				}
			}
			published := parseTime(entry.Published)
			updated := parseTime(entry.Updated)
			if published.IsZero() {
				published = updated
			}
			item := article.Item{Title: entry.Title, Abstract: first(entry.Summary, entry.Content), URL: link, SourceName: source.Name, SourceWeight: source.Weight, SourceType: kind, Language: source.Language, PublishedAt: published, UpdatedAt: updated}
			item.Normalize()
			if item.Title != "" && item.URL != "" && !item.PublishedAt.IsZero() {
				out = append(out, item)
			}
		}
	} else {
		items := doc.RDFItems
		if doc.Channel != nil {
			items = doc.Channel.Items
		}
		for _, entry := range items {
			published := parseTime(first(entry.PubDate, entry.Date))
			item := article.Item{Title: entry.Title, Abstract: entry.Description, URL: entry.Link, DOI: entry.DOI, SourceName: source.Name, SourceWeight: source.Weight, SourceType: kind, Language: source.Language, PublishedAt: published, UpdatedAt: published}
			item.Normalize()
			if item.Title != "" && item.URL != "" && !item.PublishedAt.IsZero() {
				out = append(out, item)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("feed contains no usable dated entries")
	}
	return out, nil
}
func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC822Z, "2006-01-02"} {
		if value, err := time.Parse(layout, raw); err == nil {
			return value
		}
	}
	return time.Time{}
}
