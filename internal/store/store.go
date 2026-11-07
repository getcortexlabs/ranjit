package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/filelock"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	db    *sql.DB
	guard *filelock.Lock
}

func Open(ctx context.Context, path string) (*Store, error) { return open(ctx, path, true) }

func open(ctx context.Context, path string, acquire bool) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(filepath.ToSlash(abs), "//") {
		return nil, errors.New("SQLite requires a local disk")
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	abs, err = canonicalPath(abs)
	if err != nil {
		return nil, err
	}
	var guard *filelock.Lock
	if acquire {
		guard, err = filelock.Acquire(abs+".lock", false)
		if err != nil {
			return nil, err
		}
	}
	success := false
	defer func() {
		if !success && guard != nil {
			guard.Close()
		}
	}()
	f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	u := databaseURL(abs)
	q := url.Values{"_pragma": {"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)"}, "_txlock": {"immediate"}}
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	// One writer per installation. WAL still permits external backup readers.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, guard: guard}
	if err = s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	success = true
	return s, nil
}

func (s *Store) Close() error {
	err := s.db.Close()
	if s.guard != nil {
		err = errors.Join(err, s.guard.Close())
		s.guard = nil
	}
	return err
}
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) migrate(ctx context.Context) (resultErr error) {
	// Table rebuilds must not cascade-delete children. Pin this connection until
	// constraints are checked and enforcement is restored, including on failure.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() {
		_, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)
		resultErr = errors.Join(resultErr, err)
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	files, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, file := range files {
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE name = ?`, file.Name()).Scan(&exists)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		body, err := migrations.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(name) VALUES (?)`, file.Name()); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	broken := rows.Next()
	checkErr := rows.Err()
	rows.Close()
	if checkErr != nil {
		return checkErr
	}
	if broken {
		return errors.New("migration violates foreign key constraints")
	}
	return tx.Commit()
}

type scanner interface{ Scan(...any) error }

const connectionColumns = `id, customer_id, institution_id, provider, status, created_at, consent_expires_at, last_synced_at, authorization_mode, credential_id`

func scanConnection(row scanner) (ranjit.Connection, error) {
	var c ranjit.Connection
	var created, expires int64
	var synced sql.NullInt64
	err := row.Scan(&c.ID, &c.CustomerID, &c.InstitutionID, &c.Provider, &c.Status, &created, &expires, &synced, &c.AuthorizationMode, &c.CredentialID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ranjit.ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.CreatedAt, c.ConsentExpiresAt = instant(created), instant(expires)
	if synced.Valid {
		t := instant(synced.Int64)
		c.LastSyncedAt = &t
	}
	return c, nil
}

func instant(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

func changed(result sql.Result, err error, otherwise error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return otherwise
	}
	return nil
}

func databaseURL(path string) url.URL {
	uriPath := filepath.ToSlash(path)
	if volume := filepath.VolumeName(path); len(volume) == 2 && volume[1] == ':' {
		uriPath = "/" + uriPath
	}
	return url.URL{Scheme: "file", Path: uriPath}
}

func canonicalPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}
