package ranjit

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientDoesNotFollowCredentialRedirects(t *testing.T) {
	c, err := NewClient("http://localhost:8080", "private-key")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	c.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {"https://another.example/v1/institutions"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	_, err = c.Institutions(t.Context())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("unexpected redirect result: %v", err)
	}
	if requests != 1 {
		t.Fatal("client followed redirect with credentials")
	}
}
