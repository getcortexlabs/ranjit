package c6

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
)

func bankTransport(t *testing.T) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "baas-api-sandbox.c6bank.info" {
			t.Error("incorrect C6 origin")
		}
		if r.URL.Path == "/v1/auth/" {
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("incorrect auth request")
			}
			if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "test-app" || r.Form.Get("client_secret") != "test-secret" || r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("scope") != "" {
				t.Error("incorrect OAuth form")
			}
			r.Body.Close()
			return response(200, authorized), nil
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("incorrect resource request")
		}
		if r.URL.Path == "/v1/statement/balance" {
			return response(200, `{"available":100.01,"currency_code":"BRL","total_available":99999,"limit":{"available":99898.99}}`), nil
		}
		if r.URL.Path != "/v1/statement/" {
			t.Fatal("unexpected endpoint")
		}
		q := r.URL.Query()
		start, end := q.Get("start_date"), q.Get("end_date")
		want := map[string]string{"2026-08-10": "2026-09-08", "2026-09-09": "2026-10-08", "2026-10-09": "2026-11-07"}
		if want[start] != end {
			t.Errorf("unexpected interval %s..%s", start, end)
		}
		body := fmt.Sprintf(`{"entries":[{"reference":"ref-%s","local_reference":"same-internal-reference","entry_date":"%s","amount":"1.23","operation_type":"OUTGOING","title":"Pagamento"}]}`, start, start)
		return response(200, body), nil
	})
}

func TestFetchWindowsAndExcludeCreditLimit(t *testing.T) {
	s := sourceFixture(t)
	s.client.Transport = bankTransport(t)
	for range 2 {
		batch, err := s.Fetch(t.Context(), connection(s))
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Accounts) != 1 || batch.Accounts[0].Balance.AmountMinor != 10001 || len(batch.Transactions) != 3 {
			t.Fatalf("incorrect batch: %+v", batch)
		}
		for _, tx := range batch.Transactions {
			if tx.Amount.AmountMinor != -123 || !strings.HasPrefix(tx.ExternalID, "ref-") {
				t.Fatal("incorrect normalized debit or ID")
			}
		}
	}
}

func TestRejectMalformedLaterWindowWithoutPartialResult(t *testing.T) {
	for _, bad := range []string{`{}`, `{"entries":null}`, `{"entries":[{"reference":"x","entry_date":"2026-11-01","amount":"1.001","operation_type":"OUTGOING"}]}`, `{"entries":[{"reference":"x","entry_date":"2026-12-01","amount":"1.00","operation_type":"INCOMING"}]}`} {
		t.Run(bad, func(t *testing.T) {
			s := sourceFixture(t)
			normal := bankTransport(t)
			s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("start_date") == "2026-10-09" {
					return response(200, bad), nil
				}
				return normal.RoundTrip(r)
			})
			batch, err := s.Fetch(t.Context(), connection(s))
			if err == nil || len(batch.Accounts) != 0 || len(batch.Transactions) != 0 {
				t.Fatal("partial import accepted")
			}
		})
	}
}

func TestNormalizeRequiresStableReference(t *testing.T) {
	base := transaction{Reference: "ref-1", Date: "2026-11-07", Operation: "INCOMING", Value: json.RawMessage(`"0.01"`)}
	tx, err := normalize(base)
	if err != nil || tx.Amount.AmountMinor != 1 {
		t.Fatal(tx, err)
	}
	for _, mutate := range []func(*transaction){func(t *transaction) { t.Reference = "" }, func(t *transaction) { t.Date = "2026-02-30" }, func(t *transaction) { t.Operation = "C" }, func(t *transaction) { t.Value = json.RawMessage(`"-1.00"`) }} {
		entry := base
		mutate(&entry)
		if _, err := normalize(entry); !errors.Is(err, ranjit.ErrInvalid) {
			t.Fatal("invalid entry accepted", err)
		}
	}
}

func TestBindingAndBankCalendar(t *testing.T) {
	s := sourceFixture(t)
	s.now = func() time.Time { return time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC) }
	s.lookbackDays = 1
	normal := bankTransport(t)
	s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v1/statement/" {
			if r.URL.Query().Get("start_date") != "2026-11-07" || r.URL.Query().Get("end_date") != "2026-11-07" {
				t.Error("UTC day leaked into bank window")
			}
			return response(200, `{"entries":[]}`), nil
		}
		return normal.RoundTrip(r)
	})
	c := connection(s)
	c.ConsentExpiresAt = s.now().Add(time.Hour)
	if _, err := s.Fetch(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("called bank for wrong binding"); return nil, nil })
	c.CredentialID = "other-app"
	if _, err := s.Fetch(t.Context(), c); !errors.Is(err, ranjit.ErrProviderCredentials) {
		t.Fatal(err)
	}
}

func TestMissingBalanceOrCurrencyNeverBecomesZero(t *testing.T) {
	for _, body := range []string{`{}`, `{"available":0}`, `{"available":1,"currency_code":"USD"}`, `{"available":null,"currency_code":"BRL"}`} {
		s := sourceFixture(t)
		s.token = "cached"
		s.expiresAt = now.Add(time.Hour)
		s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, body), nil })
		if _, err := s.Fetch(t.Context(), connection(s)); err == nil {
			t.Fatal("invalid balance accepted")
		}
	}
}
