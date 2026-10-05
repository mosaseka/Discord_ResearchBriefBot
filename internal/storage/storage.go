package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mosaseka/Discord_ResearchBriefBot/internal/article"
	"github.com/mosaseka/Discord_ResearchBriefBot/internal/ranking"
	"github.com/mosaseka/Discord_ResearchBriefBot/migrations"
	_ "modernc.org/sqlite"
)

var ErrAlreadyReserved = errors.New("one or more items are already reserved or delivered")

// Old versions also recorded canceled or timed-out requests as failed. Only
// explicit Discord 4xx rejections are safe to retry, not those legacy rows.
// A 408 response remains ambiguous because processing may have started.
const retryableDeliverySQL = `deliveries.state='failed' AND EXISTS (
    SELECT 1 FROM delivery_batches b WHERE b.id=deliveries.batch_id
    AND b.state='failed' AND b.last_error GLOB 'Discord returned HTTP 4[0-9][0-9]'
    AND b.last_error<>'Discord returned HTTP 408'
)`

type Store struct {
	db   *sql.DB
	path string
}
type RunResult struct {
	ScopeDays, Fetched, Selected, Posted int
	Status                               string
	Err                                  error
}

func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is empty")
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file::memory:") {
		if err := mkdir(filepath.Dir(path)); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	if path != ":memory:" && !strings.HasPrefix(path, "file::memory:") {
		if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
			db.Close()
			return nil, err
		}
	}
	s := &Store{db: db, path: path}
	if err = s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

var mkdir = func(path string) error {
	if path == "" || path == "." {
		return nil
	}
	return osMkdirAll(path)
}
var osMkdirAll = func(path string) error { return mkdirAll(path) }

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Path() string { return s.path }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY,checksum TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, name := range files {
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])
		var existing string
		err = s.db.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=?", name).Scan(&existing)
		if err == nil {
			if existing != checksum {
				return fmt.Errorf("migration %s checksum changed", name)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(?,?,?)", name, checksum, now())
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) BeginRun(ctx context.Context, runType string) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO runs(id,run_type,started_at) VALUES(?,?,?)", id, runType, now())
	if err != nil {
		return "", fmt.Errorf("begin run: %w", err)
	}
	return id, nil
}
func (s *Store) FinishRun(ctx context.Context, id string, result RunResult) error {
	message := ""
	if result.Err != nil {
		message = sanitize(result.Err.Error())
	}
	if result.Status == "" {
		result.Status = "success"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE runs SET finished_at=?,scope_days=?,fetched_count=?,selected_count=?,posted_count=?,status=?,last_error=? WHERE id=?`, now(), result.ScopeDays, result.Fetched, result.Selected, result.Posted, result.Status, nullable(message), id)
	return err
}

func (s *Store) RecordSourceFetch(ctx context.Context, runID, sourceName string, windowDays, count int, fetchErr error) error {
	status, message := "success", ""
	if fetchErr != nil {
		status = "failure"
		if count > 0 {
			status = "partial"
		}
		message = sanitize(fetchErr.Error())
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO source_fetches(run_id,source_name,window_days,checked_at,status,item_count,last_error) VALUES(?,?,?,?,?,?,?)`, runID, sourceName, windowDays, now(), status, count, nullable(message))
	if err != nil {
		return fmt.Errorf("record source fetch: %w", err)
	}
	return nil
}

func (s *Store) Record(ctx context.Context, items []ranking.Ranked, selected map[string]bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now()
	for _, ranked := range items {
		i := ranked.Item
		flag := 0
		if selected[i.Identity()] {
			flag = 1
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO items(identity,revision,title,url,doi,arxiv_id,pmid,source_name,source_type,published_at,updated_at,score,selected,first_seen_at,last_seen_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(identity) DO UPDATE SET revision=excluded.revision,title=excluded.title,url=excluded.url,doi=excluded.doi,arxiv_id=excluded.arxiv_id,pmid=excluded.pmid,source_name=excluded.source_name,source_type=excluded.source_type,published_at=excluded.published_at,updated_at=excluded.updated_at,score=excluded.score,selected=excluded.selected,last_seen_at=excluded.last_seen_at`, i.Identity(), i.Revision(), i.Title, i.URL, nullable(i.DOI), nullable(i.ArXivID), nullable(i.PMID), i.SourceName, string(i.SourceType), date(i.PublishedAt), date(i.UpdatedAt), ranked.Breakdown.Total(), flag, stamp, stamp)
		if err != nil {
			return fmt.Errorf("record item: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) AlreadyNotified(ctx context.Context, channel string, item article.Item) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM deliveries WHERE channel_key=? AND identity=? AND revision=? AND NOT (`+retryableDeliverySQL+`))`, channelKey(channel), item.Identity(), item.Revision()).Scan(&exists)
	return exists == 1, err
}

type Reservation struct {
	ID    int64
	Nonce string
}

func (s *Store) Reserve(ctx context.Context, runID, channel, nonce string, items []article.Item) (Reservation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Reservation{}, err
	}
	defer tx.Rollback()
	key := channelKey(channel)
	res, err := tx.ExecContext(ctx, `INSERT INTO delivery_batches(run_id,channel_key,nonce,state,created_at) VALUES(?,?,?,'sending',?)`, runID, key, nonce, now())
	if err != nil {
		return Reservation{}, fmt.Errorf("reserve batch: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Reservation{}, err
	}
	for _, item := range items {
		r, insertErr := tx.ExecContext(ctx, `INSERT INTO deliveries(batch_id,run_id,channel_key,identity,revision,title,state) VALUES(?,?,?,?,?,?,'sending') ON CONFLICT(channel_key,identity,revision) DO UPDATE SET batch_id=excluded.batch_id,run_id=excluded.run_id,title=excluded.title,state='sending',notified_at=NULL WHERE `+retryableDeliverySQL, id, runID, key, item.Identity(), item.Revision(), item.Title)
		if insertErr != nil {
			return Reservation{}, insertErr
		}
		affected, err := r.RowsAffected()
		if err != nil {
			return Reservation{}, fmt.Errorf("check delivery reservation: %w", err)
		}
		if affected != 1 {
			return Reservation{}, ErrAlreadyReserved
		}
	}
	if err = tx.Commit(); err != nil {
		return Reservation{}, err
	}
	return Reservation{ID: id, Nonce: nonce}, nil
}
func (s *Store) MarkSent(ctx context.Context, id int64, messageID string) error {
	return s.mark(ctx, id, "sent", messageID, nil)
}
func (s *Store) MarkFailed(ctx context.Context, id int64, ambiguous bool, cause error) error {
	state := "failed"
	if ambiguous {
		state = "unknown"
	}
	return s.mark(ctx, id, state, "", cause)
}
func (s *Store) mark(ctx context.Context, id int64, state, messageID string, cause error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	message := ""
	if cause != nil {
		message = sanitize(cause.Error())
	}
	var sent any
	if state == "sent" {
		sent = now()
	}
	if _, err = tx.ExecContext(ctx, "UPDATE delivery_batches SET state=?,message_id=?,sent_at=?,last_error=? WHERE id=?", state, nullable(messageID), sent, nullable(message), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE deliveries SET state=?,notified_at=? WHERE batch_id=?", state, sent, id); err != nil {
		return err
	}
	return tx.Commit()
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func date(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func sanitize(s string) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 512 {
		r = r[:512]
	}
	return string(r)
}

func channelKey(channel string) string {
	sum := sha256.Sum256([]byte("discord-research-brief-channel\x00" + channel))
	return hex.EncodeToString(sum[:])
}
