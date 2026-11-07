package efi

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
	cfg := Config{Environment: "homologation", ClientID: "test-app", ClientSecretFile: filepath.Join(dir, "secret"), CertificateFile: filepath.Join(dir, "cert.pem"), PrivateKeyFile: filepath.Join(dir, "key.pem"), LookbackDays: 90}
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
	return ranjit.Connection{ID: "con-test", Provider: "efi", InstitutionID: "efi", Status: "active", AuthorizationMode: "api_credentials", CredentialID: s.CredentialID(), CreatedAt: now, ConsentExpiresAt: now.Add(time.Hour)}
}

const authorized = `{"access_token":"test-token","token_type":"Bearer","expires_in":3600,"scope":"pix.read gn.balance.read"}`

func pageJSON(page int) string {
	if page == 0 {
		return `{"parametros":{"paginacao":{"paginaAtual":0,"quantidadeDePaginas":2,"quantidadeTotalDeItens":2}},"pix":[{"endToEndId":"receipt-1","valor":"123.45","horario":"2026-11-01T12:00:00Z","infoPagador":"Serviço","devolucoes":[{"id":"refund-1","valor":"23.45","status":"DEVOLVIDO","horario":{"liquidacao":"2026-11-02T12:00:00Z"}},{"id":"refund-2","status":"EM_PROCESSAMENTO"}]}]}`
	}
	return `{"parametros":{"paginacao":{"paginaAtual":1,"quantidadeDePaginas":2,"quantidadeTotalDeItens":2}},"pix":[{"endToEndId":"receipt-2","valor":"0.01","horario":"2026-11-03T12:00:00Z"}]}`
}

func bankTransport(t *testing.T) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "pix-h.api.efipay.com.br" {
			t.Errorf("unexpected origin %s", r.URL.Host)
		}
		switch r.URL.Path {
		case "/oauth/token":
			if r.Method != http.MethodPost {
				t.Errorf("token method = %s", r.Method)
			}
			id, secret, ok := r.BasicAuth()
			if !ok || id != "test-app" || secret != "test-secret" {
				t.Error("bad Basic authentication")
			}
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload["grant_type"] != "client_credentials" {
				t.Error("bad grant")
			}
			r.Body.Close()
			return response(200, authorized), nil
		case "/v2/gn/saldo", "/v2/pix":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("unexpected bank operation")
			}
			if r.URL.Path == "/v2/gn/saldo" {
				return response(200, `{"saldo":"100.01"}`), nil
			}
			q := r.URL.Query()
			if q.Get("inicio") != now.Add(-90*24*time.Hour).Format(time.RFC3339) || q.Get("fim") != now.Format(time.RFC3339) || q.Get("paginacao.itensPorPagina") != "100" {
				t.Error("inconsistent history window")
			}
			page, err := strconv.Atoi(q.Get("paginacao.paginaAtual"))
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

func TestFetchAllPagesAndCompletedRefunds(t *testing.T) {
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
		if r.URL.Path == "/v2/pix" {
			return response(200, `{"parametros":{"paginacao":{"paginaAtual":0,"quantidadeDePaginas":0,"quantidadeTotalDeItens":0}},"pix":[]}`), nil
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
	if p.CredentialID() == h.CredentialID() || p.baseURL != "https://pix.api.efipay.com.br" {
		t.Fatal("environment binding missing")
	}
	transport := p.client.Transport.(*http.Transport)
	if transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || len(transport.TLSClientConfig.Certificates) != 1 {
		t.Fatal("mTLS configuration is incomplete")
	}
}

func TestTokenRefreshAndConcurrentCache(t *testing.T) {
	s := sourceFixture(t)
	var tokens, balances atomic.Int32
	s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/oauth/token" {
			n := tokens.Add(1)
			return response(200, strings.Replace(authorized, "test-token", fmtToken(n), 1)), nil
		}
		if balances.Add(1) == 1 {
			return response(401, `{"message":"secret upstream data"}`), nil
		}
		return response(200, `{"saldo":"1.00"}`), nil
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
		`{"pix":[]}`,
		`{"parametros":{"paginacao":{"paginaAtual":0,"quantidadeDePaginas":2,"quantidadeTotalDeItens":2}},"pix":[]}`,
		`{"parametros":{"paginacao":{"paginaAtual":1,"quantidadeDePaginas":3,"quantidadeTotalDeItens":2}},"pix":[]}`,
		`{"parametros":{"paginacao":{"paginaAtual":1,"quantidadeDePaginas":2,"quantidadeTotalDeItens":2}},"pix":[]}`,
	} {
		t.Run(bad, func(t *testing.T) {
			s := sourceFixture(t)
			normal := bankTransport(t)
			s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("paginacao.paginaAtual") == "1" {
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
			return response(200, strings.Replace(authorized, "pix.read gn.balance.read", "pix.read", 1))
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
	for value, want := range map[string]int64{"0.01": 1, "123.45": 12345, "-9.99": -999, "90071992547409.91": 9007199254740991} {
		got, err := money(value)
		if err != nil || got.AmountMinor != want {
			t.Fatalf("%s: %+v %v", value, got, err)
		}
	}
	for _, value := range []string{"NaN", "1e2", "1", "1.001", "+1.00", "1,00", "90071992547409.92", "--1.00"} {
		if _, err := money(value); err == nil {
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
