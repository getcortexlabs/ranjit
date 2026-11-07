package store

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
)

type Search struct {
	Customer, Account, Query, From, Through, Cursor string
	Limit                                           int
}

func (s *Store) SearchTransactions(ctx context.Context, f Search, now time.Time) (ranjit.Page[ranjit.Transaction], error) {
	result := ranjit.Page[ranjit.Transaction]{Data: []ranjit.Transaction{}}
	if f.Customer == "" || len(f.Customer) > 128 || len(f.Query) > 256 || f.Limit < 1 || f.Limit > 100 {
		return result, ranjit.ErrInvalid
	}
	for _, d := range []string{f.From, f.Through} {
		if d != "" {
			if _, err := time.Parse(time.DateOnly, d); err != nil {
				return result, ranjit.ErrInvalid
			}
		}
	}
	if f.From != "" && f.Through != "" && f.From > f.Through {
		return result, ranjit.ErrInvalid
	}
	query := `SELECT t.id,t.account_id,t.description,t.amount_minor,t.currency,t.posted_on,t.status FROM transactions t JOIN accounts a ON a.id=t.account_id JOIN connections c ON c.id=a.connection_id WHERE c.customer_id=? AND ((c.status='active' AND (c.provider='file' OR c.consent_expires_at>?)) OR (c.authorization_mode='api_credentials' AND c.status IN ('active','paused','expired')))`
	args := []any{f.Customer, now.UnixMilli()}
	if f.Account != "" {
		query += ` AND a.id=?`
		args = append(args, f.Account)
	}
	if f.Query != "" {
		query += ` AND instr(lower(t.description),lower(?))>0`
		args = append(args, f.Query)
	}
	if f.From != "" {
		query += ` AND t.posted_on>=?`
		args = append(args, f.From)
	}
	if f.Through != "" {
		query += ` AND t.posted_on<=?`
		args = append(args, f.Through)
	}
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		if err != nil || len(raw) > 150 {
			return result, ranjit.ErrInvalid
		}
		day, id, ok := strings.Cut(string(raw), "|")
		if !ok {
			return result, ranjit.ErrInvalid
		}
		query += ` AND (t.posted_on<? OR (t.posted_on=? AND t.id<?))`
		args = append(args, day, day, id)
	}
	query += ` ORDER BY t.posted_on DESC,t.id DESC LIMIT ?`
	args = append(args, f.Limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var t ranjit.Transaction
		if err = rows.Scan(&t.ID, &t.AccountID, &t.Description, &t.Amount.AmountMinor, &t.Amount.Currency, &t.PostedOn, &t.Status); err != nil {
			return result, err
		}
		result.Data = append(result.Data, t)
	}
	if len(result.Data) > f.Limit {
		result.Data = result.Data[:f.Limit]
		last := result.Data[len(result.Data)-1]
		result.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s|%s", last.PostedOn, last.ID)))
	}
	return result, rows.Err()
}
