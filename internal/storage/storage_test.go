package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
)

func TestDuplicateReservationRollsBack(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run1, _ := store.BeginRun(ctx, "test")
	item := article.Item{Title: "x", URL: "https://example.org", PublishedAt: time.Now(), UpdatedAt: time.Now()}
	first, err := store.Reserve(ctx, run1, "123", "nonce1", []article.Item{item})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.MarkSent(ctx, first.ID, "message"); err != nil {
		t.Fatal(err)
	}
	var storedChannel string
	if err = store.db.QueryRowContext(ctx, "SELECT channel_key FROM delivery_batches WHERE id=?", first.ID).Scan(&storedChannel); err != nil || storedChannel == "123" {
		t.Fatalf("raw channel ID was stored: value=%q err=%v", storedChannel, err)
	}
	seen, err := store.AlreadyNotified(ctx, "123", item)
	if err != nil || !seen {
		t.Fatalf("seen=%v err=%v", seen, err)
	}
	run2, _ := store.BeginRun(ctx, "test")
	_, err = store.Reserve(ctx, run2, "123", "nonce2", []article.Item{item})
	if !errors.Is(err, ErrAlreadyReserved) {
		t.Fatalf("got %v", err)
	}
	var count int
	if err = store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM delivery_batches WHERE nonce='nonce2'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
}

func TestConcurrentReservationHasOneWinner(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run1, _ := store.BeginRun(ctx, "test")
	run2, _ := store.BeginRun(ctx, "test")
	item := article.Item{Title: "concurrent", URL: "https://example.org/c", PublishedAt: time.Now(), UpdatedAt: time.Now()}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for index, runID := range []string{run1, run2} {
		wg.Add(1)
		go func(index int, runID string) {
			defer wg.Done()
			<-start
			_, reserveErr := store.Reserve(ctx, runID, "123", "concurrent"+string(rune('a'+index)), []article.Item{item})
			errs <- reserveErr
		}(index, runID)
	}
	close(start)
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for reserveErr := range errs {
		if reserveErr == nil {
			successes++
		} else if errors.Is(reserveErr, ErrAlreadyReserved) {
			conflicts++
		} else {
			t.Fatal(reserveErr)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestBusyFailureLeavesDatabaseUsable(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "busy.db")
	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.db.ExecContext(ctx, "PRAGMA busy_timeout=1"); err != nil {
		t.Fatal(err)
	}
	tx, err := first.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO runs(id,run_type,started_at) VALUES('locked','test','now')"); err != nil {
		t.Fatal(err)
	}
	if _, err = second.BeginRun(ctx, "busy"); err == nil {
		t.Fatal("expected SQLite busy error")
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = second.BeginRun(ctx, "after-busy"); err != nil {
		t.Fatalf("database did not recover after busy error: %v", err)
	}
}
