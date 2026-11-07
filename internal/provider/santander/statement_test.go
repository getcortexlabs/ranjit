package santander

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/getcortexlabs/ranjit"
)

const firstPage = `{"_pageable":{"_moreElements":true},"_content":[{"transactionId":"tx-1","completedAuthorisedPaymentType":"TRANSACAO_EFETIVADA","creditDebitType":"CREDITO","transactionName":"Recebimento","amount":"100.01","transactionCurrency":"BRL","transactionDate":"2026-11-01"},{"transactionId":"tx-2","completedAuthorisedPaymentType":"TRANSACAO_EFETIVADA","creditDebitType":"DEBITO","transactionName":"Pagamento","amount":"-0.01","transactionCurrency":"BRL","transactionDate":"2026-11-02"}]}`
const lastPage = `{"_pageable":{"_moreElements":false},"_content":[{"transactionId":"tx-3","completedAuthorisedPaymentType":"TRANSACAO_EFETIVADA","creditDebitType":"DEBITO","transactionName":"Pagamento","amount":"1.23","transactionCurrency":"BRL","transactionDate":"2026-11-03"}]}`

func bankTransport(t *testing.T) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "trust-sandbox.api.santander.com.br" {
			t.Error("incorrect Santander origin")
		}
		if r.URL.Path == "/auth/oauth/v2/token" {
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("incorrect token request")
			}
			if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "test-app" || r.Form.Get("client_secret") != "test-secret" || r.Form.Get("grant_type") != "client_credentials" {
				t.Error("incorrect token form")
			}
			r.Body.Close()
			return response(200, authorized), nil
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Application-Key") != "test-app" {
			t.Error("incorrect account request")
		}
		if r.URL.Path == bankPath+"/balances/0001.000000123456" {
			return response(200, `{"availableAmount":"100.01","availableAmountCurrency":"BRL"}`), nil
		}
		if r.URL.Path != bankPath+"/statements/0001.000000123456" {
			t.Fatal("unexpected endpoint")
		}
		q := r.URL.Query()
		if q.Get("initialDate") != "2026-08-10" || q.Get("finalDate") != "2026-11-07" || q.Get("_limit") != "50" {
			t.Error("incorrect dates or limit")
		}
		switch q.Get("_offset") {
		case "1":
			return response(200, firstPage), nil
		case "3":
			return response(200, lastPage), nil
		default:
			t.Fatal("incorrect record offset")
			return nil, nil
		}
	})
}

func TestFetchAllPagesAndSignedAmounts(t *testing.T) {
	s := sourceFixture(t)
	s.client.Transport = bankTransport(t)
	for range 2 {
		batch, err := s.Fetch(t.Context(), connection(s))
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Accounts) != 1 || batch.Accounts[0].Balance.AmountMinor != 10001 || len(batch.Transactions) != 3 {
			t.Fatalf("incorrect batch %+v", batch)
		}
		for i, want := range []int64{10001, -1, -123} {
			if batch.Transactions[i].Amount.AmountMinor != want {
				t.Error("incorrect sign or precision")
			}
		}
	}
}

func TestIncompleteOrRepeatedPagesReturnNoBatch(t *testing.T) {
	for _, bad := range []string{`{}`, `{"_content":[]}`, `{"_pageable":{"_moreElements":true},"_content":[]}`, strings.Replace(lastPage, "tx-3", "tx-1", 1), strings.Replace(lastPage, "2026-11-03", "2026-12-01", 1), strings.Replace(lastPage, "BRL", "USD", 1)} {
		t.Run(bad, func(t *testing.T) {
			s := sourceFixture(t)
			normal := bankTransport(t)
			s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("_offset") == "3" {
					return response(200, bad), nil
				}
				return normal.RoundTrip(r)
			})
			batch, err := s.Fetch(t.Context(), connection(s))
			if err == nil || len(batch.Accounts) != 0 || len(batch.Transactions) != 0 {
				t.Fatal("partial or repeated import accepted")
			}
		})
	}
}

func TestNormalizeRejectsAmbiguousFinancialData(t *testing.T) {
	base := transaction{ID: "tx", Status: "TRANSACAO_EFETIVADA", Operation: "CREDITO", Date: "2026-11-07", Value: json.RawMessage(`"1.00"`), Currency: "BRL"}
	for _, mutate := range []func(*transaction){func(t *transaction) { t.ID = "" }, func(t *transaction) { t.Status = "unknown" }, func(t *transaction) { t.Operation = "D" }, func(t *transaction) { t.Value = json.RawMessage(`"-1.00"`) }, func(t *transaction) { t.Currency = "USD" }, func(t *transaction) { t.Date = "2026-02-30" }} {
		entry := base
		mutate(&entry)
		if _, err := normalize(entry); !errors.Is(err, ranjit.ErrInvalid) {
			t.Fatal("ambiguous entry accepted", err)
		}
	}
}

func TestConfigurationAndCredentialBinding(t *testing.T) {
	cfg := configFixture(t)
	for _, account := range []string{"12345", "0001.123456", "0001.00000012345x", "0001/000000123456", "0001.000000123456?"} {
		bad := cfg
		bad.Account = account
		if _, err := New(bad); err == nil {
			t.Fatal("invalid account accepted")
		}
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cfg.Account = "0002.000000123456"
	other, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if other.CredentialID() == s.CredentialID() {
		t.Fatal("account is not bound to credentials")
	}
	cfg.Environment = "production"
	prod, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer prod.Close()
	if prod.CredentialID() == other.CredentialID() || prod.baseURL != "https://trust-open.api.santander.com.br" {
		t.Fatal("environment binding missing")
	}
	s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("called bank for wrong binding"); return nil, nil })
	c := connection(s)
	c.CredentialID = other.CredentialID()
	if _, err := s.Fetch(t.Context(), c); !errors.Is(err, ranjit.ErrProviderCredentials) {
		t.Fatal(err)
	}
}
