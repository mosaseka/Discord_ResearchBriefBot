package pubmed

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
)

const baseEndpoint = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils"

type Fetcher interface {
	Fetch(context.Context, string, string, []string, int64) ([]byte, error)
}
type Client struct {
	HTTP                  Fetcher
	Endpoint, Tool, Email string
}

func (c *Client) Name() string { return "PubMed" }

func (c *Client) Collect(ctx context.Context, since, until time.Time) ([]article.Item, error) {
	base := strings.TrimRight(c.Endpoint, "/")
	if base == "" {
		base = baseEndpoint
	}
	query := `("reinforcement learning" OR "offline reinforcement learning" OR "contextual bandit" OR "adaptive intervention" OR "just-in-time adaptive intervention" OR "artificial intelligence" OR "conversational agent" OR "digital phenotyping") AND ("mental health" OR psychiatry OR depression OR anxiety OR psychotherapy OR "digital mental health")`
	search, _ := url.Parse(base + "/esearch.fcgi")
	q := search.Query()
	q.Set("db", "pubmed")
	q.Set("retmode", "xml")
	q.Set("retmax", "100")
	q.Set("sort", "pub date")
	q.Set("term", query)
	q.Set("datetype", "pdat")
	q.Set("mindate", since.Format("2006/01/02"))
	q.Set("maxdate", until.Format("2006/01/02"))
	c.params(q)
	search.RawQuery = q.Encode()
	body, err := c.HTTP.Fetch(ctx, search.String(), "application/xml, text/xml;q=0.9", []string{"application/xml", "text/xml"}, 2<<20)
	if err != nil {
		return nil, fmt.Errorf("PubMed esearch: %w", err)
	}
	ids, err := ParseSearch(body)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	fetch, _ := url.Parse(base + "/efetch.fcgi")
	q = fetch.Query()
	q.Set("db", "pubmed")
	q.Set("retmode", "xml")
	q.Set("id", strings.Join(ids, ","))
	c.params(q)
	fetch.RawQuery = q.Encode()
	body, err = c.HTTP.Fetch(ctx, fetch.String(), "application/xml, text/xml;q=0.9", []string{"application/xml", "text/xml"}, 8<<20)
	if err != nil {
		return nil, fmt.Errorf("PubMed efetch: %w", err)
	}
	items, err := ParseArticles(body)
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for _, i := range items {
		if !i.PublishedAt.Before(since) && !i.PublishedAt.After(until) {
			out = append(out, i)
		}
	}
	return out, nil
}

func (c *Client) params(q url.Values) {
	if c.Tool != "" {
		q.Set("tool", c.Tool)
	}
	if c.Email != "" {
		q.Set("email", c.Email)
	}
}

type searchResult struct {
	IDs      []string `xml:"IdList>Id"`
	APIError string   `xml:"ERROR"`
}

func ParseSearch(body []byte) ([]string, error) {
	var r searchResult
	if err := xml.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse PubMed search XML: %w", err)
	}
	if strings.TrimSpace(r.APIError) != "" {
		return nil, errors.New("PubMed search API returned an error")
	}
	return r.IDs, nil
}

type articleSet struct {
	Articles []pubmedArticle `xml:"PubmedArticle"`
}
type pubmedArticle struct {
	Citation citation   `xml:"MedlineCitation"`
	Data     pubmedData `xml:"PubmedData"`
}
type citation struct {
	PMID          string         `xml:"PMID"`
	Article       medlineArticle `xml:"Article"`
	DateCompleted xmlDate        `xml:"DateCompleted"`
}
type medlineArticle struct {
	Title    innerText `xml:"ArticleTitle"`
	Abstract struct {
		Texts []abstractText `xml:"AbstractText"`
	} `xml:"Abstract"`
	Journal struct {
		Title string `xml:"Title"`
		Issue struct {
			Date xmlDate `xml:"PubDate"`
		} `xml:"JournalIssue"`
	} `xml:"Journal"`
	ArticleDate      []xmlDate `xml:"ArticleDate"`
	PublicationTypes []string  `xml:"PublicationTypeList>PublicationType"`
}
type abstractText struct {
	Label string `xml:"Label,attr"`
	Text  string `xml:",innerxml"`
}
type innerText struct {
	Text string `xml:",innerxml"`
}
type xmlDate struct {
	Year    string `xml:"Year"`
	Month   string `xml:"Month"`
	Day     string `xml:"Day"`
	Medline string `xml:"MedlineDate"`
}
type pubmedData struct {
	IDs []articleID `xml:"ArticleIdList>ArticleId"`
}
type articleID struct {
	Type  string `xml:"IdType,attr"`
	Value string `xml:",chardata"`
}

func ParseArticles(body []byte) ([]article.Item, error) {
	var set articleSet
	if err := xml.Unmarshal(body, &set); err != nil {
		return nil, fmt.Errorf("parse PubMed article XML: %w", err)
	}
	if len(set.Articles) == 0 {
		return nil, errors.New("PubMed article response contains no articles")
	}
	out := make([]article.Item, 0, len(set.Articles))
	for _, record := range set.Articles {
		date := record.Citation.Article.Journal.Issue.Date
		if len(record.Citation.Article.ArticleDate) > 0 {
			date = record.Citation.Article.ArticleDate[0]
		}
		published := parseDate(date)
		if published.IsZero() {
			published = parseDate(record.Citation.DateCompleted)
		}
		abstracts := make([]string, 0, len(record.Citation.Article.Abstract.Texts))
		for _, part := range record.Citation.Article.Abstract.Texts {
			prefix := ""
			if strings.TrimSpace(part.Label) != "" {
				prefix = part.Label + ": "
			}
			abstracts = append(abstracts, prefix+part.Text)
		}
		doi := ""
		for _, id := range record.Data.IDs {
			if strings.EqualFold(id.Type, "doi") {
				doi = id.Value
			}
		}
		preprint := false
		reviewed := false
		for _, typ := range record.Citation.Article.PublicationTypes {
			switch strings.ToLower(strings.TrimSpace(typ)) {
			case "preprint":
				preprint = true
			case "journal article", "randomized controlled trial", "clinical trial", "review", "systematic review", "meta-analysis":
				reviewed = true
			}
		}
		pmid := strings.TrimSpace(record.Citation.PMID)
		if pmid == "" {
			continue
		}
		sourceType := article.SourcePubMed
		if preprint {
			sourceType = article.SourcePreprint
		}
		item := article.Item{Title: record.Citation.Article.Title.Text, Abstract: strings.Join(abstracts, " "), URL: "https://pubmed.ncbi.nlm.nih.gov/" + pmid + "/", DOI: doi, PMID: pmid, Journal: record.Citation.Article.Journal.Title, SourceName: "PubMed", SourceType: sourceType, Language: "en", PublishedAt: published, UpdatedAt: published, PublicationTypes: record.Citation.Article.PublicationTypes, PeerReviewed: reviewed && !preprint}
		item.Normalize()
		out = append(out, item)
	}
	return out, nil
}

func parseDate(d xmlDate) time.Time {
	year, _ := strconv.Atoi(strings.TrimSpace(d.Year))
	if year == 0 && len(d.Medline) >= 4 {
		year, _ = strconv.Atoi(d.Medline[:4])
	}
	if year == 0 {
		return time.Time{}
	}
	months := map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
	month := time.January
	if n, err := strconv.Atoi(d.Month); err == nil && n >= 1 && n <= 12 {
		month = time.Month(n)
	} else if len(d.Month) >= 3 {
		month = months[strings.ToLower(d.Month[:3])]
	}
	day, _ := strconv.Atoi(d.Day)
	if day < 1 {
		day = 1
	}
	if month == 0 {
		return time.Time{}
	}
	value := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if value.Year() != year || value.Month() != month || value.Day() != day {
		return time.Time{}
	}
	return value
}
