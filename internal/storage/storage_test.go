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

func TestRetryableReservationPreservesFailedBatch(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	stamp := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	item := article.Item{Title: "retryable study", PublishedAt: stamp, UpdatedAt: stamp}
	run1, _ := store.BeginRun(ctx, "test")
	first, err := store.Reserve(ctx, run1, "123", "rejected", []article.Item{item})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.MarkFailed(ctx, first.ID, false, errors.New("Discord returned HTTP 403")); err != nil {
		t.Fatal(err)
	}
	seen, err := store.AlreadyNotified(ctx, "123", item)
	if err != nil || seen {
		t.Fatalf("definite rejection should be available: seen=%v err=%v", seen, err)
	}
	run2, _ := store.BeginRun(ctx, "test")
	second, err := store.Reserve(ctx, run2, "123", "retry", []article.Item{item})
	if err != nil {
		t.Fatal(err)
	}
	var state, lastError string
	if err = store.db.QueryRowContext(ctx, "SELECT state,last_error FROM delivery_batches WHERE id=?", first.ID).Scan(&state, &lastError); err != nil || state != "failed" || lastError != "Discord returned HTTP 403" {
		t.Fatalf("previous failure audit changed: state=%s error=%s err=%v", state, lastError, err)
	}
	if err = store.MarkSent(ctx, second.ID, "message"); err != nil {
		t.Fatal(err)
	}
	seen, err = store.AlreadyNotified(ctx, "123", item)
	if err != nil || !seen {
		t.Fatalf("successful retry was not suppressed: seen=%v err=%v", seen, err)
	}
}

func TestAmbiguousAndLegacyFailuresRemainSuppressed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ambiguous bool
		cause     error
	}{
		{"unknown", true, errors.New("Discord returned HTTP 403")},
		{"legacy canceled", false, context.Canceled},
		{"legacy deadline", false, context.DeadlineExceeded},
		{"legacy server error", false, errors.New("Discord returned HTTP 503")},
		{"request timeout", false, errors.New("Discord returned HTTP 408")},
		{"missing error", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(ctx, ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			item := article.Item{Title: "uncertain study"}
			run1, _ := store.BeginRun(ctx, "test")
			reservation, err := store.Reserve(ctx, run1, "123", "first", []article.Item{item})
			if err != nil {
				t.Fatal(err)
			}
			if err = store.MarkFailed(ctx, reservation.ID, tc.ambiguous, tc.cause); err != nil {
				t.Fatal(err)
			}
			seen, err := store.AlreadyNotified(ctx, "123", item)
			if err != nil || !seen {
				t.Fatalf("uncertain outcome not suppressed: seen=%v err=%v", seen, err)
			}
			run2, _ := store.BeginRun(ctx, "test")
			if _, err = store.Reserve(ctx, run2, "123", "second", []article.Item{item}); !errors.Is(err, ErrAlreadyReserved) {
				t.Fatalf("uncertain outcome re-reserved: %v", err)
			}
		})
	}
}

func TestRetryReservationRollbackOnLaterConflict(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	item := article.Item{Title: "retry rollback study"}
	run1, _ := store.BeginRun(ctx, "test")
	first, err := store.Reserve(ctx, run1, "123", "original", []article.Item{item})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.MarkFailed(ctx, first.ID, false, errors.New("Discord returned HTTP 401")); err != nil {
		t.Fatal(err)
	}
	run2, _ := store.BeginRun(ctx, "test")
	if _, err = store.Reserve(ctx, run2, "123", "duplicate-in-batch", []article.Item{item, item}); !errors.Is(err, ErrAlreadyReserved) {
		t.Fatalf("expected duplicate conflict: %v", err)
	}
	var batchID int64
	var state string
	if err = store.db.QueryRowContext(ctx, "SELECT batch_id,state FROM deliveries").Scan(&batchID, &state); err != nil || batchID != first.ID || state != "failed" {
		t.Fatalf("failed retry was not rolled back: batch=%d state=%s err=%v", batchID, state, err)
	}
	var count int
	if err = store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM delivery_batches").Scan(&count); err != nil || count != 1 {
		t.Fatalf("rolled-back batch was retained: count=%d err=%v", count, err)
	}
}
