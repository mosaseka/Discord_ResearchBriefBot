package collector

import (
	"context"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
)

type Source interface {
	Name() string
	Collect(context.Context, time.Time, time.Time) ([]article.Item, error)
}
