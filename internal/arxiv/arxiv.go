package arxiv

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
)

const defaultEndpoint = "https://export.arxiv.org/api/query"

type Fetcher interface {
	Fetch(context.Context, string, string, []string, int64) ([]byte, error)
}

type Client struct {
	HTTP     Fetcher
	Endpoint string
}

func (c *Client) Name() string { return "arXiv" }

var idPattern = regexp.MustCompile(`(?i)/abs/([^?#/]+)`)

func (c *Client) Collect(ctx context.Context, since, until time.Time) ([]article.Item, error) {
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	termsA := `all:("reinforcement learning" OR "contextual bandit" OR "offline reinforcement learning" OR "safe reinforcement learning" OR agent OR "agentic AI" OR "agent memory" OR "multi-agent" OR "conversational agent" OR "patient simulator" OR "just-in-time adaptive intervention" OR JITAI OR "digital phenotyping" OR "adaptive intervention" OR "longitudinal personalization")`
	termsB := `all:("mental health" OR psychiatry OR psychiatric OR depression OR anxiety OR psychotherapy OR CBT OR psychological OR wellbeing OR suicide OR "self-harm")`
	date := fmt.Sprintf("submittedDate:[%s TO %s]", since.UTC().Format("200601021504"), until.UTC().Format("200601021504"))
	q := u.Query()
	q.Set("search_query", termsA+" AND "+termsB+" AND "+date)
	q.Set("start", "0")
	q.Set("max_results", "100")
	q.Set("sortBy", "submittedDate")
	q.Set("sortOrder", "descending")
	u.RawQuery = q.Encode()
	body, err := c.HTTP.Fetch(ctx, u.String(), "application/atom+xml, application/xml;q=0.9, text/xml;q=0.8", []string{"application/atom+xml", "application/xml", "text/xml"}, 4<<20)
	if err != nil {
		return nil, fmt.Errorf("fetch arXiv: %w", err)
	}
	items, parseErr := Parse(body)
	return filter(items, since, until), parseErr
}

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}
type atomEntry struct {
	ID         string `xml:"id"`
	Updated    string `xml:"updated"`
	Published  string `xml:"published"`
	Title      string `xml:"title"`
	Summary    string `xml:"summary"`
	DOI        string `xml:"doi"`
	JournalRef string `xml:"journal_ref"`
	Authors    []struct {
		Name string `xml:"name"`
	} `xml:"author"`
}

func Parse(body []byte) ([]article.Item, error) {
	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse arXiv XML: %w", err)
	}
	out := make([]article.Item, 0, len(feed.Entries))
	var joined error
	for index, entry := range feed.Entries {
		published, err := time.Parse(time.RFC3339, strings.TrimSpace(entry.Published))
		if err != nil {
			joined = errors.Join(joined, fmt.Errorf("arXiv entry %d has an invalid publication date", index))
			continue
		}
		updated, _ := time.Parse(time.RFC3339, strings.TrimSpace(entry.Updated))
		match := idPattern.FindStringSubmatch(strings.TrimSpace(entry.ID))
		id := ""
		if len(match) == 2 {
			id = match[1]
		}
		authors := make([]string, 0, len(entry.Authors))
		for _, a := range entry.Authors {
			authors = append(authors, strings.TrimSpace(a.Name))
		}
		item := article.Item{Title: entry.Title, Abstract: entry.Summary, URL: strings.Replace(strings.TrimSpace(entry.ID), "http://", "https://", 1), ArXivURL: strings.Replace(strings.TrimSpace(entry.ID), "http://", "https://", 1), DOI: entry.DOI, ArXivID: id, Journal: entry.JournalRef, SourceName: "arXiv", SourceType: article.SourceArXiv, Language: "en", PublishedAt: published, UpdatedAt: updated, Authors: authors}
		item.Normalize()
		if item.Title != "" && item.ArXivID != "" {
			out = append(out, item)
		} else {
			joined = errors.Join(joined, fmt.Errorf("arXiv entry %d is missing title or ID", index))
		}
	}
	return out, joined
}

func filter(items []article.Item, since, until time.Time) []article.Item {
	out := items[:0]
	for _, i := range items {
		if !i.PublishedAt.Before(since) && !i.PublishedAt.After(until) {
			out = append(out, i)
		}
	}
	return out
}
