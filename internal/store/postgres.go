package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed postgres/*.sql
var postgresMigrations embed.FS

func IsPostgres(value string) bool {
	return strings.HasPrefix(value, "postgres://") || strings.HasPrefix(value, "postgresql://")
}

func openPostgres(ctx context.Context, connectionString string) (*Store, error) {
	config, err := pgx.ParseConfig(connectionString)
	if err != nil {
		// Driver errors can contain passwords or the full DSN.
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	config.ConnectTimeout = 10 * time.Second
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	s := &Store{db: &database{DB: db, postgres: true}}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("cannot connect to PostgreSQL; check the database URL, network and TLS configuration")
	}
	if err = s.migratePostgres(ctx); err != nil {
		db.Close()
		return nil, errors.New("cannot migrate PostgreSQL; check database permissions and server compatibility")
	}
	return s, nil
}

func (s *Store) migratePostgres(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		return err
	}
	files, err := postgresMigrations.ReadDir("postgres")
	if err != nil {
		return err
	}
	for _, file := range files {
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM schema_migrations WHERE name=?`, file.Name()).Scan(&exists)
		if err == nil {
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		body, err := postgresMigrations.ReadFile("postgres/" + file.Name())
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
	return tx.Commit()
}
