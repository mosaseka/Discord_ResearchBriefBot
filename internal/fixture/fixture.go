package fixture

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/arxiv"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/pubmed"
)

type Manifest struct {
	Now time.Time `json:"now"`
}
type Source struct{ Items []article.Item }

func (s *Source) Name() string { return "offline fixture" }
func (s *Source) Collect(_ context.Context, since, until time.Time) ([]article.Item, error) {
	out := make([]article.Item, 0, len(s.Items))
	for _, item := range s.Items {
		if !item.PublishedAt.Before(since) && !item.PublishedAt.After(until) {
			out = append(out, item)
		}
	}
	return out, nil
}

func Load(dir string) (*Source, time.Time, error) {
	manifestBody, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read fixture manifest: %w", err)
	}
	var manifest Manifest
	if err = json.Unmarshal(manifestBody, &manifest); err != nil {
		return nil, time.Time{}, fmt.Errorf("parse fixture manifest: %w", err)
	}
	if manifest.Now.IsZero() {
		return nil, time.Time{}, fmt.Errorf("fixture manifest now is required")
	}
	arxivBody, err := os.ReadFile(filepath.Join(dir, "arxiv.xml"))
	if err != nil {
		return nil, time.Time{}, err
	}
	arxivItems, err := arxiv.Parse(arxivBody)
	if err != nil {
		return nil, time.Time{}, err
	}
	pubmedBody, err := os.ReadFile(filepath.Join(dir, "pubmed.xml"))
	if err != nil {
		return nil, time.Time{}, err
	}
	pubmedItems, err := pubmed.ParseArticles(pubmedBody)
	if err != nil {
		return nil, time.Time{}, err
	}
	return &Source{Items: append(arxivItems, pubmedItems...)}, manifest.Now, nil
}
