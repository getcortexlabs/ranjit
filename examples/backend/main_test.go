package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getcortexlabs/ranjit"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBackendOwnsCustomerIdentityAndServiceKey(t *testing.T) {
	client, err := ranjit.NewClient("http://localhost:8080", "service-only-key")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	client.HTTPClient.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer service-only-key" || r.URL.Query().Get("customer_id") != "example-customer" {
			t.Fatal("service key or trusted customer identity lost")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Request: r}, nil
	})
	h := routes(client, "backend-token")
	for _, token := range []string{"", "wrong", "backend-token"} {
		r := httptest.NewRequest(http.MethodGet, "/transactions?customer_id=someone-else", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := http.StatusUnauthorized
		if token == "backend-token" {
			want = http.StatusOK
		}
		if w.Code != want || strings.Contains(w.Body.String(), "service-only-key") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatal("unauthenticated request reached Ranjit")
	}
}
