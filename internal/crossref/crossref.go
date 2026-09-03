package crossref

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
)

const defaultEndpoint = "https://api.crossref.org/works/"

type Fetcher interface {
	Fetch(context.Context, string, string, []string, int64) ([]byte, error)
}
type Client struct {
	HTTP             Fetcher
	Endpoint, Mailto string
	MaxLookups       int
}

type response struct {
	Message Work `json:"message"`
}
type Work struct {
	DOI       string    `json:"DOI"`
	Title     []string  `json:"title"`
	Type      string    `json:"type"`
	Container []string  `json:"container-title"`
	URL       string    `json:"URL"`
	Published dateParts `json:"published"`
}
type dateParts struct {
	Parts [][]int `json:"date-parts"`
}

func Parse(body []byte) (Work, error) {
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return Work{}, fmt.Errorf("parse Crossref JSON: %w", err)
	}
	if r.Message.DOI == "" {
		return Work{}, fmt.Errorf("Crossref response has no DOI")
	}
	return r.Message, nil
}

func (c *Client) Enrich(ctx context.Context, items []article.Item) ([]article.Item, error) {
	limit := c.MaxLookups
	if limit <= 0 {
		limit = 8
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	var joined error
	lookups := 0
	for i := range items {
		if lookups >= limit {
			break
		}
		if items[i].DOI == "" || (items[i].SourceType != article.SourceArXiv && items[i].Journal != "" && !items[i].PublishedAt.IsZero()) {
			continue
		}
		u := endpoint + url.PathEscape(items[i].DOI)
		if c.Mailto != "" {
			u += "?mailto=" + url.QueryEscape(c.Mailto)
		}
		body, err := c.HTTP.Fetch(ctx, u, "application/json", []string{"application/json", "application/vnd.crossref-api-message+json"}, 2<<20)
		lookups++
		if err != nil {
			joined = errors.Join(joined, fmt.Errorf("Crossref %s: %w", items[i].DOI, err))
			continue
		}
		work, err := Parse(body)
		if err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		if article.NormalizeDOI(work.DOI) != items[i].DOI {
			joined = errors.Join(joined, fmt.Errorf("Crossref DOI mismatch for %s", items[i].DOI))
			continue
		}
		if items[i].Journal == "" && len(work.Container) > 0 {
			items[i].Journal = strings.TrimSpace(work.Container[0])
		}
		crossrefDate := work.Published.time()
		if items[i].PublishedAt.IsZero() {
			items[i].PublishedAt = crossrefDate
		}
		items[i].DOI = article.NormalizeDOI(work.DOI)
		if isReviewedType(work.Type) {
			wasArXiv := items[i].SourceType == article.SourceArXiv
			items[i].PeerReviewed = true
			items[i].SourceType = article.SourcePubMed
			if wasArXiv {
				if items[i].ArXivURL == "" {
					items[i].ArXivURL = items[i].URL
				}
				items[i].SourceName = "Crossref"
				if strings.HasPrefix(strings.ToLower(work.URL), "https://") {
					items[i].URL = work.URL
				}
				if !crossrefDate.IsZero() {
					items[i].PublishedAt = crossrefDate
					if crossrefDate.After(items[i].UpdatedAt) {
						items[i].UpdatedAt = crossrefDate
					}
				}
			}
		}
		items[i].Normalize()
	}
	return items, joined
}

func (d dateParts) time() time.Time {
	if len(d.Parts) == 0 || len(d.Parts[0]) == 0 {
		return time.Time{}
	}
	p := d.Parts[0]
	year := p[0]
	month, day := 1, 1
	if len(p) > 1 {
		month = p[1]
	}
	if len(p) > 2 {
		day = p[2]
	}
	if year < 1 || month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}
func isReviewedType(t string) bool {
	switch t {
	case "journal-article", "proceedings-article", "book-chapter":
		return true
	default:
		return false
	}
}
