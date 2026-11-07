package inter

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

var now = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func configFixture(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test client"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Environment: "sandbox", Account: "123456", ClientID: "test-app", ClientSecretFile: filepath.Join(dir, "secret"), CertificateFile: filepath.Join(dir, "cert.pem"), PrivateKeyFile: filepath.Join(dir, "key.pem"), LookbackDays: 90}
	for path, body := range map[string][]byte{cfg.ClientSecretFile: []byte("test-secret\n"), cfg.CertificateFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cfg.PrivateKeyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})} {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func sourceFixture(t *testing.T) *Source {
	t.Helper()
	s, err := New(configFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.now = func() time.Time { return now }
	return s
}

func connection(s *Source) ranjit.Connection {
	return ranjit.Connection{ID: "con-test", Provider: "inter", InstitutionID: "inter", Status: "active", AuthorizationMode: "api_credentials", CredentialID: s.CredentialID(), CreatedAt: now, ConsentExpiresAt: now.Add(time.Hour)}
}

const authorized = `{"access_token":"test-token","token_type":"Bearer","expires_in":3600,"scope":"extrato.read"}`

func pageJSON(page int) string {
	if page == 0 {
		return `{"totalPaginas":2,"totalElementos":3,"primeiraPagina":true,"ultimaPagina":false,"numeroDeElementos":2,"transacoes":[{"idTransacao":"tx-1","valor":"123.45","tipoOperacao":"C","dataTransacao":"2026-11-01","descricao":"Recebimento"},{"idTransacao":"tx-2","valor":"23.45","tipoOperacao":"D","dataTransacao":"2026-11-02","titulo":"Pagamento"}]}`
	}
	return `{"totalPaginas":2,"totalElementos":3,"primeiraPagina":false,"ultimaPagina":true,"numeroDeElementos":1,"transacoes":[{"idTransacao":"tx-3","valor":"0.01","tipoOperacao":"C","dataTransacao":"2026-11-03"}]}`
}

func bankTransport(t *testing.T) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "cdpj-sandbox.partners.uatinter.co" {
			t.Errorf("unexpected origin %s", r.URL.Host)
		}
		switch r.URL.Path {
		case "/oauth/v2/token":
			if r.Method != http.MethodPost {
				t.Errorf("token method = %s", r.Method)
			}
			if r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("unexpected token authentication")
			}
			if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "test-app" || r.Form.Get("client_secret") != "test-secret" || r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("scope") != "extrato.read" {
				t.Error("incorrect token form or write scope requested")
			}
			r.Body.Close()
			return response(200, authorized), nil
		case "/banking/v2/saldo", "/banking/v2/extrato/completo":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("x-conta-corrente") != "123456" {
				t.Error("unexpected bank operation")
			}
			if r.URL.Path == "/banking/v2/saldo" {
				return response(200, `{"disponivel":"100.01"}`), nil
			}
			q := r.URL.Query()
			if q.Get("dataInicio") != "2026-08-10" || q.Get("dataFim") != "2026-11-07" || q.Get("tamanhoPagina") != "100" {
				t.Error("inconsistent history window")
			}
			page, err := strconv.Atoi(q.Get("pagina"))
			if err != nil || page < 0 || page > 1 {
				t.Error("invalid page request")
			}
			return response(200, pageJSON(page)), nil
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			return response(404, `{}`), nil
		}
	})
}

func TestFetchAllPagesWithCreditsAndDebits(t *testing.T) {
	s := sourceFixture(t)
	s.client.Transport = bankTransport(t)
	for range 2 {
		batch, err := s.Fetch(t.Context(), connection(s))
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Accounts) != 1 || batch.Accounts[0].Balance.AmountMinor != 10001 || len(batch.Transactions) != 3 {
			t.Fatalf("incorrect normalized batch: %+v", batch)
		}
		amounts := []int64{12345, -2345, 1}
		for i, tx := range batch.Transactions {
			if tx.Amount.AmountMinor != amounts[i] {
				t.Fatalf("amount = %d", tx.Amount.AmountMinor)
			}
		}
	}
}

func TestEmptyHistoryStillImportsBalance(t *testing.T) {
	s := sourceFixture(t)
	normal := bankTransport(t)
	s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/banking/v2/extrato/completo" {
			return response(200, `{"totalPaginas":0,"totalElementos":0,"primeiraPagina":true,"ultimaPagina":true,"numeroDeElementos":0,"transacoes":[]}`), nil
		}
		return normal.RoundTrip(r)
	})
	batch, err := s.Fetch(t.Context(), connection(s))
	if err != nil || len(batch.Accounts) != 1 || batch.Accounts[0].Balance.AmountMinor != 10001 || len(batch.Transactions) != 0 {
		t.Fatalf("empty history discarded balance: %+v %v", batch, err)
	}
}

func TestCredentialAndEnvironmentBinding(t *testing.T) {
	s := sourceFixture(t)
	var calls atomic.Int32
	s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return response(200, authorized), nil })
	c := connection(s)
	c.CredentialID = "a-different-application"
	if _, err := s.Fetch(t.Context(), c); !errors.Is(err, ranjit.ErrProviderCredentials) {
		t.Fatalf("changed credentials accepted: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("contacted bank before checking binding")
	}
	cfg := configFixture(t)
	h, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	cfg.Environment = "production"
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.CredentialID() == h.CredentialID() || p.baseURL != "https://cdpj.partners.bancointer.com.br" {
		t.Fatal("environment binding missing")
	}
	cfg.Environment = "sandbox"
	cfg.Account = "654321"
	other, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if other.CredentialID() == h.CredentialID() {
		t.Fatal("account binding missing")
	}
	transport := p.client.Transport.(*http.Transport)
	if transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || len(transport.TLSClientConfig.Certificates) != 1 {
		t.Fatal("mTLS configuration is incomplete")
	}
}

func TestStatementUsesBankCalendarAcrossUTCMidnight(t *testing.T) {
	s := sourceFixture(t)
	s.now = func() time.Time { return time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC) }
	s.lookbackDays = 1
	normal := bankTransport(t)
	s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/banking/v2/extrato/completo" {
			if r.URL.Query().Get("dataInicio") != "2026-11-07" || r.URL.Query().Get("dataFim") != "2026-11-07" {
				t.Error("UTC date used instead of bank date")
			}
			return response(200, `{"totalPaginas":0,"totalElementos":0,"primeiraPagina":true,"ultimaPagina":true,"numeroDeElementos":0,"transacoes":[]}`), nil
		}
		return normal.RoundTrip(r)
	})
	c := connection(s)
	c.ConsentExpiresAt = s.now().Add(time.Hour)
	if _, err := s.Fetch(t.Context(), c); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidStatementEntriesAreRejected(t *testing.T) {
	base := transaction{ID: "tx", Date: "2026-11-07", Operation: "D", Value: json.RawMessage(`"10.00"`)}
	for _, change := range []func(*transaction){
		func(t *transaction) { t.ID = "" },
		func(t *transaction) { t.Date = "2026-02-30" },
		func(t *transaction) { t.Operation = "unknown" },
		func(t *transaction) { t.Value = json.RawMessage(`"-10.00"`) },
		func(t *transaction) { t.Value = json.RawMessage(`"0.001"`) },
	} {
		entry := base
		change(&entry)
		if _, err := normalize(entry); !errors.Is(err, ranjit.ErrInvalid) {
			t.Fatalf("invalid entry accepted: %v", err)
		}
	}
}

func TestRateLimitCarriesBoundedRetryDelay(t *testing.T) {
	for header, want := range map[string]time.Duration{"": time.Minute, "120": 2 * time.Minute, "999999": 5 * time.Minute, now.Add(90 * time.Second).Format(http.TimeFormat): 90 * time.Second} {
		t.Run(header, func(t *testing.T) {
			s := sourceFixture(t)
			s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				r := response(429, `{"secret":"private bank details"}`)
				r.Header.Set("Retry-After", header)
				return r, nil
			})
			err := s.Verify(t.Context())
			var retry *provider.RetryError
			if !errors.As(err, &retry) || retry.After != want || !errors.Is(err, ranjit.ErrProviderUnavailable) {
				t.Fatalf("retry = %v", err)
			}
		})
	}
}

func TestTokenRefreshAndConcurrentCache(t *testing.T) {
	s := sourceFixture(t)
	var tokens, balances atomic.Int32
	s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/oauth/v2/token" {
			n := tokens.Add(1)
			return response(200, strings.Replace(authorized, "test-token", fmtToken(n), 1)), nil
		}
		if balances.Add(1) == 1 {
			return response(401, `{"message":"secret upstream data"}`), nil
		}
		return response(200, `{"disponivel":"1.00"}`), nil
	})
	if err := s.Verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 2 {
		t.Fatalf("refresh count = %d", tokens.Load())
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if err := s.Verify(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if tokens.Load() != 2 {
		t.Fatal("concurrent requests ignored token cache")
	}
	s.now = func() time.Time { return now.Add(time.Hour) }
	if err := s.Verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	if tokens.Load() != 3 {
		t.Fatal("expired token reused")
	}
}

func fmtToken(n int32) string { return "token-" + strconv.Itoa(int(n)) }

func TestIncompletePaginationReturnsNoBatch(t *testing.T) {
	for _, bad := range []string{
		`{"transacoes":[]}`,
		`{"totalPaginas":2,"totalElementos":3,"primeiraPagina":false,"ultimaPagina":true,"numeroDeElementos":0,"transacoes":[]}`,
		`{"totalPaginas":3,"totalElementos":3,"primeiraPagina":false,"ultimaPagina":false,"numeroDeElementos":0,"transacoes":[]}`,
		`{"totalPaginas":2,"totalElementos":3,"primeiraPagina":true,"ultimaPagina":true,"numeroDeElementos":0,"transacoes":[]}`,
		strings.Replace(pageJSON(1), "tx-3", "tx-1", 1),
	} {
		t.Run(bad, func(t *testing.T) {
			s := sourceFixture(t)
			normal := bankTransport(t)
			s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("pagina") == "1" {
					return response(200, bad), nil
				}
				return normal.RoundTrip(r)
			})
			batch, err := s.Fetch(t.Context(), connection(s))
			if err == nil || len(batch.Accounts) != 0 || len(batch.Transactions) != 0 {
				t.Fatalf("partial import accepted: %+v %v", batch, err)
			}
		})
	}
}

func TestRejectScopeErrorsRedirectsAndOversizedResponses(t *testing.T) {
	for name, result := range map[string]func() *http.Response{
		"missing scope": func() *http.Response {
			return response(200, strings.Replace(authorized, "extrato.read", "cob.read", 1))
		},
		"invalid credentials": func() *http.Response { return response(401, `{"secret":"upstream-secret"}`) },
		"oversized":           func() *http.Response { return response(200, strings.Repeat(" ", maxResponseBytes+1)) },
		"redirect": func() *http.Response {
			r := response(307, "")
			r.Header.Set("Location", "https://untrusted.example/")
			return r
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := sourceFixture(t)
			calls := 0
			s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return result(), nil })
			err := s.Verify(t.Context())
			if err == nil || strings.Contains(err.Error(), "upstream-secret") || calls != 1 {
				t.Fatalf("unexpected result: %v, %d calls", err, calls)
			}
		})
	}
}

func TestMoneyUsesExactMinorUnits(t *testing.T) {
	for value, want := range map[string]int64{`"1.2"`: 120, "0": 0, "2": 200, "1.2": 120, "0.01": 1, "123.45": 12345, "-9.99": -999, "90071992547409.91": 9007199254740991} {
		got, err := money(json.RawMessage(value))
		if err != nil || got.AmountMinor != want {
			t.Fatalf("%s: %+v %v", value, got, err)
		}
	}
	for _, value := range []string{"NaN", "1e2", "null", "1.001", "+1.00", "1,00", "90071992547409.92", "--1.00"} {
		if _, err := money(json.RawMessage(value)); err == nil {
			t.Fatalf("invalid amount accepted: %s", value)
		}
	}
}

func TestCancelledRequestReturnsContextError(t *testing.T) {
	s := sourceFixture(t)
	s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Verify(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
