package article

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/textutil"
)

type SourceType string

const (
	SourcePubMed          SourceType = "Peer-reviewed"
	SourcePreprint        SourceType = "Preprint"
	SourceArXiv           SourceType = "arXiv"
	SourceTechnicalReport SourceType = "Technical Report"
	SourceResearchFeed    SourceType = "Research Feed"
)

type Item struct {
	Title            string
	Abstract         string
	URL              string
	ArXivURL         string
	DOI              string
	ArXivID          string
	PMID             string
	Journal          string
	SourceName       string
	SourceWeight     int
	SourceType       SourceType
	Language         string
	PublishedAt      time.Time
	UpdatedAt        time.Time
	Authors          []string
	PublicationTypes []string
	PeerReviewed     bool
}

var arxivVersion = regexp.MustCompile(`(?i)v\d+$`)

func (i *Item) Normalize() {
	i.Title = textutil.Clean(i.Title)
	i.Abstract = textutil.Clean(i.Abstract)
	i.DOI = NormalizeDOI(i.DOI)
	i.ArXivID = strings.ToLower(arxivVersion.ReplaceAllString(strings.TrimSpace(i.ArXivID), ""))
	i.PMID = strings.TrimSpace(i.PMID)
	i.URL = strings.TrimSpace(i.URL)
	i.ArXivURL = strings.TrimSpace(i.ArXivURL)
	i.Journal = textutil.Clean(i.Journal)
	i.SourceName = textutil.Clean(i.SourceName)
	if i.UpdatedAt.IsZero() {
		i.UpdatedAt = i.PublishedAt
	}
	sort.Strings(i.PublicationTypes)
}

func NormalizeDOI(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "doi:"} {
		raw = strings.TrimPrefix(raw, prefix)
	}
	return strings.TrimSpace(raw)
}

func (i Item) NormalizedTitle() string { return textutil.Comparison(i.Title) }

func (i Item) Identity() string {
	switch {
	case i.DOI != "":
		return "doi:" + NormalizeDOI(i.DOI)
	case i.ArXivID != "":
		return "arxiv:" + strings.ToLower(i.ArXivID)
	case i.PMID != "":
		return "pmid:" + i.PMID
	default:
		return "title:" + i.NormalizedTitle()
	}
}

func (i Item) Revision() string {
	stamp := i.UpdatedAt
	if stamp.IsZero() {
		stamp = i.PublishedAt
	}
	return fmt.Sprintf("%s:%s", i.Identity(), stamp.UTC().Format("2006-01-02T15:04:05Z"))
}

func (i Item) DisplaySource() string {
	if i.Journal != "" {
		return i.Journal
	}
	if i.SourceName != "" {
		return i.SourceName
	}
	return string(i.SourceType)
}
