package ranjit

import (
	"context"
	"iter"
)

func iterate[T any](ctx context.Context, startCursor string, fetch func(string) (Page[T], error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		cursor := startCursor
		var zero T
		seen := map[string]bool{cursor: true}
		for {
			if err := ctx.Err(); err != nil {
				yield(zero, err)
				return
			}
			page, err := fetch(cursor)
			if err != nil {
				yield(zero, err)
				return
			}
			if page.NextCursor != "" && seen[page.NextCursor] {
				yield(zero, ErrInvalidResponse)
				return
			}
			for _, item := range page.Data {
				if err = ctx.Err(); err != nil {
					yield(zero, err)
					return
				}
				if !yield(item, nil) {
					return
				}
			}
			if page.NextCursor == "" {
				return
			}
			cursor = page.NextCursor
			seen[cursor] = true
		}
	}
}

// IterConnections fetches pages lazily. Breaking the loop stops further requests.
func (c *Client) IterConnections(ctx context.Context, customer string, limit int) iter.Seq2[Connection, error] {
	return iterate(ctx, "", func(cursor string) (Page[Connection], error) { return c.Connections(ctx, customer, cursor, limit) })
}
func (c *Client) IterAccounts(ctx context.Context, connectionID string, limit int) iter.Seq2[Account, error] {
	return iterate(ctx, "", func(cursor string) (Page[Account], error) { return c.Accounts(ctx, connectionID, cursor, limit) })
}
func (c *Client) IterTransactions(ctx context.Context, accountID string, limit int) iter.Seq2[Transaction, error] {
	return iterate(ctx, "", func(cursor string) (Page[Transaction], error) { return c.Transactions(ctx, accountID, cursor, limit) })
}
func (c *Client) IterSearchTransactions(ctx context.Context, filter TransactionFilter) iter.Seq2[Transaction, error] {
	return iterate(ctx, filter.Cursor, func(cursor string) (Page[Transaction], error) {
		pageFilter := filter
		pageFilter.Cursor = cursor
		return c.SearchTransactions(ctx, pageFilter)
	})
}
