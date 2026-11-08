package ranjit

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestIteratorsFetchLazilyAndPreserveFilters(t *testing.T) {
	c, _ := NewClient("http://localhost:8080", "key")
	calls := 0
	c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if q.Get("customer_id") != "customer" || q.Get("q") != "cafe" || q.Get("limit") != "1" {
			t.Fatal("filters lost", q)
		}
		if q.Get("cursor") == "" {
			return testResponse(r, 200, `{"data":[{"id":"first"}],"next_cursor":"second-page"}`), nil
		}
		if q.Get("cursor") != "second-page" {
			t.Fatal("cursor lost")
		}
		return testResponse(r, 200, `{"data":[{"id":"second"}]}`), nil
	})
	seq := c.IterSearchTransactions(t.Context(), TransactionFilter{CustomerID: "customer", Query: "cafe", Limit: 1})
	if calls != 0 {
		t.Fatal("eager request")
	}
	for row, err := range seq {
		if err != nil || row.ID != "first" {
			t.Fatal(row, err)
		}
		break
	}
	if calls != 1 {
		t.Fatal("fetched after break", calls)
	}
	calls = 0
	var ids []string
	for row, err := range c.IterSearchTransactions(t.Context(), TransactionFilter{CustomerID: "customer", Query: "cafe", Limit: 1}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, row.ID)
	}
	if calls != 2 || len(ids) != 2 || ids[1] != "second" {
		t.Fatal(ids, calls)
	}
}

func TestIteratorCancellationAndCursorCycles(t *testing.T) {
	c, _ := NewClient("http://localhost:8080", "key")
	calls := 0
	c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return testResponse(r, 200, `{"data":[{"id":"first"},{"id":"second"}],"next_cursor":"cycle"}`), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	seen := 0
	for _, err := range c.IterAccounts(ctx, "con", 2) {
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			break
		}
		seen++
		cancel()
	}
	if seen != 1 || calls != 1 {
		t.Fatal("cancellation ignored", seen, calls)
	}
	calls = 0
	var failure error
	for _, err := range c.IterConnections(t.Context(), "customer", 2) {
		if err != nil {
			failure = err
			break
		}
	}
	if !errors.Is(failure, ErrInvalidResponse) || calls != 2 {
		t.Fatal("cursor cycle accepted", failure, calls)
	}
}

func TestIteratorCanBeTraversedAgain(t *testing.T) {
	c, _ := NewClient("http://localhost:8080", "key")
	c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("cursor") == "" {
			return testResponse(r, 200, `{"data":[{"id":"first"}],"next_cursor":"second"}`), nil
		}
		return testResponse(r, 200, `{"data":[{"id":"second"}]}`), nil
	})
	sequence := c.IterSearchTransactions(t.Context(), TransactionFilter{CustomerID: "customer", Limit: 1})
	for i := 0; i < 2; i++ {
		var ids []string
		for item, err := range sequence {
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, item.ID)
		}
		if len(ids) != 2 || ids[0] != "first" {
			t.Fatal("iterator retained a previous traversal's cursor", ids)
		}
	}
}
