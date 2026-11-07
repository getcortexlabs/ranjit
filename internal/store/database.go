package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// Keep SQL and transaction boundaries shared by both engines. Only parameter
// syntax and transaction serialization differ; no bank data enters SQL text.
type database struct {
	*sql.DB
	postgres bool
}

type transaction struct {
	*sql.Tx
	postgres bool
}

func bind(query string, args []any, postgres bool) (string, []any) {
	if !postgres {
		return query, args
	}
	var out strings.Builder
	index := 0
	var quote rune
	for _, char := range query {
		if char == '\'' || char == '"' {
			if quote == char {
				quote = 0
			} else if quote == 0 {
				quote = char
			}
		}
		if char == '?' && quote == 0 {
			index++
			out.WriteByte('$')
			out.WriteString(strconv.Itoa(index))
		} else {
			out.WriteRune(char)
		}
	}
	values := append([]any(nil), args...)
	for i, value := range values {
		if flag, ok := value.(bool); ok {
			values[i] = 0
			if flag {
				values[i] = 1
			}
		}
	}
	return out.String(), values
}

func (d *database) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	query, args = bind(query, args, d.postgres)
	return d.DB.ExecContext(ctx, query, args...)
}

func (d *database) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	query, args = bind(query, args, d.postgres)
	return d.DB.QueryContext(ctx, query, args...)
}

func (d *database) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	query, args = bind(query, args, d.postgres)
	return d.DB.QueryRowContext(ctx, query, args...)
}

func (d *database) BeginTx(ctx context.Context, options *sql.TxOptions) (*transaction, error) {
	tx, err := d.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if d.postgres {
		// Match SQLite's immediate transactions across server processes. Lifecycle
		// changes, imports and sync completion must never interleave. This lock
		// belongs to this database and is released on commit, rollback or disconnect.
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1918987882, 1)`); err != nil {
			tx.Rollback()
			return nil, err
		}
	}
	return &transaction{Tx: tx, postgres: d.postgres}, nil
}

func (t *transaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	query, args = bind(query, args, t.postgres)
	return t.Tx.ExecContext(ctx, query, args...)
}

func (t *transaction) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	query, args = bind(query, args, t.postgres)
	return t.Tx.QueryContext(ctx, query, args...)
}

func (t *transaction) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	query, args = bind(query, args, t.postgres)
	return t.Tx.QueryRowContext(ctx, query, args...)
}
