package ranjit

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"X-Request-Id": {"req-test"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestReadRetriesAreBoundedAndNeverRepeatWrites(t *testing.T) {
	c, _ := NewClient("http://localhost:8080", "key")
	c.ReadRetries = RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, MaxElapsedTime: time.Second}
	calls := 0
	c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return testResponse(r, 503, `{"error":{"code":"unavailable","message":"try later"}}`), nil
		}
		return testResponse(r, 200, `{"data":[]}`), nil
	})
	if _, err := c.Institutions(t.Context()); err != nil || calls != 3 {
		t.Fatal(calls, err)
	}
	calls = 0
	if _, err := c.ConnectWithKey(t.Context(), ConnectRequest{CustomerID: "customer", InstitutionID: "sandbox-bank"}, "operation-key-1234"); err == nil || calls != 1 {
		t.Fatal("write retried", calls, err)
	}
	calls = 0
	c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, io.EOF
		}
		return testResponse(r, 200, `{"data":[]}`), nil
	})
	if _, err := c.Institutions(t.Context()); err != nil || calls != 2 {
		t.Fatal("transport failure not recovered", calls, err)
	}
}

func TestRetryAfterBudgetCancellationAndPermanentErrors(t *testing.T) {
	c, _ := NewClient("http://localhost:8080", "key")
	c.ReadRetries = RetryPolicy{MaxAttempts: 3, MaxElapsedTime: 20 * time.Millisecond}
	calls := 0
	c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		resp := testResponse(r, 429, `{"error":{"code":"rate_limited","message":"later"}}`)
		resp.Header.Set("Retry-After", "60")
		return resp, nil
	})
	_, err := c.Institutions(t.Context())
	var api *APIError
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &api) || api.RequestID != "req-test" || calls != 1 {
		t.Fatal(calls, err)
	}
	if got := parseRetryAfter(time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), time.Now()); got < 59*time.Minute {
		t.Fatal(got)
	}
	if got := parseRetryAfter("9223372036854775807", time.Now()); got < time.Hour {
		t.Fatal("overflowed delay", got)
	}
	c.ReadRetries = RetryPolicy{MaxAttempts: 3, MaxElapsedTime: time.Second, BaseDelay: time.Second, MaxDelay: time.Second}
	ctx, cancel := context.WithCancel(t.Context())
	calls = 0
	c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return testResponse(r, 503, `{}`), nil
	})
	if _, err = c.Institutions(ctx); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatal(calls, err)
	}
	calls = 0
	c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return testResponse(r, 401, `{"error":{"code":"unauthorized","message":"invalid key"}}`), nil
	})
	if _, err = c.Institutions(t.Context()); !errors.Is(err, ErrUnauthorized) || calls != 1 {
		t.Fatal(calls, err)
	}
}

func TestClientRejectsOversizedAndTrailingResponses(t *testing.T) {
	c, _ := NewClient("http://localhost:8080", "key")
	for _, tt := range []struct {
		body string
		want error
	}{{`{"data":[]}` + strings.Repeat(" ", maxResponseBytes), ErrResponseTooLarge}, {`{"data":[]} {"extra":true}`, ErrInvalidResponse}} {
		c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return testResponse(r, 200, tt.body), nil })
		_, err := c.Institutions(t.Context())
		var api *APIError
		if !errors.Is(err, tt.want) || !errors.As(err, &api) || api.StatusCode != 200 || api.RequestID != "req-test" {
			t.Fatal(err)
		}
	}
}
