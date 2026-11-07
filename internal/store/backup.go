package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/getcortexlabs/ranjit/internal/filelock"
)

// Backup creates a consistent standalone SQLite snapshot without stopping readers.
// An existing destination is never replaced, even if another process creates it.
func (s *Store) Backup(ctx context.Context, destination string) error {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(abs), ".ranjit-backup-*.db")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	defer os.Remove(name)
	if _, err = s.db.ExecContext(ctx, `VACUUM INTO ?`, name); err != nil {
		return err
	}
	if err = checkBackup(ctx, name); err != nil {
		return err
	}
	f, err = os.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	// Hard-link publication is atomic and fails when destination already exists.
	if err = os.Link(name, abs); err != nil {
		return fmt.Errorf("publish backup (choose an unused path on a local disk): %w", err)
	}
	return nil
}

func checkBackup(ctx context.Context, path string) error {
	u := databaseURL(path)
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return errors.New("backup is not a valid SQLite database")
	}
	rows, err := db.QueryContext(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return errors.New("backup is not a Ranjit database")
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return err
		}
		if _, err = migrations.ReadFile("migrations/" + name); err != nil {
			return errors.New("backup requires a newer Ranjit version")
		}
		count++
	}
	if count == 0 {
		return errors.New("backup has no Ranjit migrations")
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	rows, err = db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("backup has broken references")
	}
	return rows.Err()
}

// Restore keeps an automatic rollback copy before replacing an existing database.
// All Ranjit processes hold a shared lock; restoration requires exclusive access.
func Restore(ctx context.Context, source, destination string, replace bool) (string, error) {
	src, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	dst, err := filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if src == dst {
		return "", errors.New("source and destination must differ")
	}
	if err = checkBackup(ctx, src); err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return "", err
	}
	guard, err := filelock.Acquire(dst+".lock", true)
	if err != nil {
		return "", err
	}
	defer guard.Close()
	info, err := os.Lstat(dst)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if exists && (!replace || !info.Mode().IsRegular()) {
		return "", errors.New("destination exists; use --replace after stopping Ranjit")
	}
	if exists {
		sourceInfo, statErr := os.Stat(src)
		if statErr != nil {
			return "", statErr
		}
		if os.SameFile(sourceInfo, info) {
			return "", errors.New("source and destination must differ")
		}
	}
	rollback := ""
	if exists {
		old, err := open(ctx, dst, false)
		if err != nil {
			return "", err
		}
		rollback = dst + ".before-restore-" + time.Now().UTC().Format("20060102T150405.000000000") + ".db"
		err = old.Backup(ctx, rollback)
		if err == nil {
			err = old.checkpoint(ctx)
		}
		closeErr := old.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	f, err := os.CreateTemp(filepath.Dir(dst), ".ranjit-restore-*.db")
	if err != nil {
		return rollback, err
	}
	temp := f.Name()
	f.Close()
	defer os.Remove(temp)
	defer os.Remove(temp + ".lock")
	// VACUUM INTO also refuses incomplete source databases and captures active WAL.
	u := databaseURL(src)
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	input, err := sql.Open("sqlite", u.String())
	if err != nil {
		return rollback, err
	}
	_, err = input.ExecContext(ctx, `VACUUM INTO ?`, temp)
	input.Close()
	if err != nil {
		return rollback, err
	}
	restored, err := Open(ctx, temp)
	if err != nil {
		return rollback, err
	}
	// Never resurrect bank collection or outstanding consent links from a backup.
	_, err = restored.db.ExecContext(ctx, `UPDATE jobs SET status='cancelled',lease_token='',lease_until=0 WHERE status IN ('queued','running'); DELETE FROM sync_responses; UPDATE connections SET status='paused',token_hash=NULL WHERE authorization_mode='api_credentials' AND status IN ('active','paused','expired'); UPDATE connections SET status='expired',token_hash=NULL WHERE provider='sandbox' AND status IN ('active','pending');`)
	if err == nil {
		err = restored.checkpoint(ctx)
	}
	closeErr := restored.Close()
	if err != nil {
		return rollback, err
	}
	if closeErr != nil {
		return rollback, closeErr
	}
	if err = checkBackup(ctx, temp); err != nil {
		return rollback, err
	}
	f, err = os.OpenFile(temp, os.O_RDWR, 0600)
	if err != nil {
		return rollback, err
	}
	err = f.Sync()
	f.Close()
	if err != nil {
		return rollback, err
	}
	// The exclusive application lock and successful checkpoint make stale sidecars
	// disposable. Never let a previous WAL be applied to the restored database.
	for _, suffix := range []string{"-wal", "-shm"} {
		if err = os.Remove(dst + suffix); err != nil && !os.IsNotExist(err) {
			return rollback, err
		}
	}
	if exists {
		if err = os.Rename(temp, dst); err != nil {
			return rollback, err
		}
	} else {
		if err = os.Link(temp, dst); err != nil {
			return rollback, err
		}
	}
	return rollback, nil
}

func (s *Store) checkpoint(ctx context.Context) error {
	var busy, logged, done int
	if err := s.db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logged, &done); err != nil {
		return err
	}
	if busy != 0 {
		return errors.New("database has active readers; stop Ranjit before restoring")
	}
	return nil
}

// BackupStopped retains an exclusive lock until the caller completes an update.
func BackupStopped(ctx context.Context, path, destination string) (func(), error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, err
	}
	guard, err := filelock.Acquire(abs+".lock", true)
	if err != nil {
		return nil, err
	}
	release := func() { _ = guard.Close() }
	if _, err = os.Stat(abs); os.IsNotExist(err) {
		return release, nil
	} else if err != nil {
		release()
		return nil, err
	}
	db, err := open(ctx, abs, false)
	if err != nil {
		release()
		return nil, err
	}
	err = db.Backup(ctx, destination)
	err = errors.Join(err, db.Close())
	if err != nil {
		release()
		return nil, err
	}
	return release, nil
}
