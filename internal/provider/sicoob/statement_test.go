package sicoob

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit"
)

func bankTransport(t *testing.T) http.RoundTripper {
	t.Helper()
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" {
			t.Error("insecure bank URL")
		}
		if r.URL.Path == "/auth/realms/cooperado/protocol/openid-connect/token" {
			if r.URL.Host != "auth.sicoob.com.br" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Error("incorrect token request")
			}
			if err := r.ParseForm(); err != nil || r.Form.Get("scope") != "cco_consulta" || r.Form.Get("client_id") != "test-app" || r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_secret") != "" {
				t.Error("incorrect token form")
			}
			r.Body.Close()
			return response(200, authorized), nil
		}
		if r.URL.Host != "api.sicoob.com.br" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("client_id") != "test-app" || r.URL.Query().Get("numeroContaCorrente") != "123456" {
			t.Error("incorrect account request")
		}
		if r.URL.Path == "/conta-corrente/v4/saldo" {
			return response(200, `{"resultado":{"saldo":100.01}}`), nil
		}
		var month, year int
		if _, err := fmt.Sscanf(r.URL.Path, "/conta-corrente/v4/extrato/%d/%d", &month, &year); err != nil {
			t.Fatal("unexpected path")
		}
		q := r.URL.Query()
		if year != 2026 || month < 8 || month > 11 || q.Get("agruparCNAB") != "false" {
			t.Error("incorrect statement query")
		}
		first, last := "1", "31"
		if month == 8 {
			first = "10"
		}
		if month == 9 {
			last = "30"
		}
		if month == 11 {
			last = "7"
		}
		if q.Get("diaInicial") != first || q.Get("diaFinal") != last {
			t.Errorf("unexpected month interval %v", q)
		}
		day, _ := strconv.Atoi(first)
		date := fmt.Sprintf("%04d-%02d-%02d", year, month, day)
		body := fmt.Sprintf(`{"transacoes":[{"transactionId":"tx-%s","valor":"1.23","tipo":"D","dataLote":"%s","data":"2026-07-01T23:00:00-03:00","descricao":"Pagamento"}]}`, date, date)
		return response(200, body), nil
	})
}

func TestFetchMonthsAndUsePostingDate(t *testing.T) {
	s := sourceFixture(t)
	s.client.Transport = bankTransport(t)
	for range 2 {
		batch, err := s.Fetch(t.Context(), connection(s))
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Accounts) != 1 || batch.Accounts[0].Balance.AmountMinor != 10001 || len(batch.Transactions) != 4 {
			t.Fatalf("incorrect batch: %+v", batch)
		}
		for _, tx := range batch.Transactions {
			if tx.Amount.AmountMinor != -123 {
				t.Fatal("debit sign lost")
			}
		}
	}
}

func TestLaterMonthFailureReturnsNoPartialBatch(t *testing.T) {
	for _, bad := range []string{`{}`, `{"transacoes":null}`, `{"resultado":null}`, `{"erro":{"mensagem":"secret"}}`, `{"transacoes":[{"transactionId":"tx","valor":"0.001","tipo":"C","dataLote":"2026-11-01"}]}`, `{"transacoes":[{"transactionId":"tx","valor":"1.00","tipo":"C","dataLote":"2026-12-01"}]}`, `{"transacoes":[{"valor":"1.00","tipo":"C","dataLote":"2026-11-01"}]}`, `{"transacoes":[{"transactionId":"tx","valor":"-1.00","tipo":"D","dataLote":"2026-11-01"}]}`} {
		t.Run(bad, func(t *testing.T) {
			s := sourceFixture(t)
			normal := bankTransport(t)
			s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/conta-corrente/v4/extrato/11/2026" {
					return response(200, bad), nil
				}
				return normal.RoundTrip(r)
			})
			batch, err := s.Fetch(t.Context(), connection(s))
			if err == nil || len(batch.Accounts) != 0 || len(batch.Transactions) != 0 {
				t.Fatal("partial or malformed batch accepted")
			}
		})
	}
}

func TestSandboxUsesStaticTokenAndBindsAccount(t *testing.T) {
	cfg := configFixture(t)
	cfg.Environment = "sandbox"
	cfg.CertificateFile = ""
	cfg.PrivateKeyFile = ""
	cfg.AccessTokenFile = filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(cfg.AccessTokenFile, []byte("sandbox-token"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "sandbox.sicoob.com.br" || r.URL.Path != "/sicoob/sandbox/conta-corrente/v4/saldo" || r.Header.Get("Authorization") != "Bearer sandbox-token" {
			t.Error("sandbox attempted production or OAuth")
		}
		return response(200, `{"saldo":0}`), nil
	})
	if err := s.Verify(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg.Account = "654321"
	other, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if other.CredentialID() == s.CredentialID() {
		t.Fatal("account change kept credential binding")
	}
	cfg.Environment = "production"
	if _, err := New(cfg); err == nil {
		t.Fatal("production accepted sandbox token")
	}
}

func TestPacingRespectsCancellation(t *testing.T) {
	s := sourceFixture(t)
	s.requestInterval = time.Second
	s.lastRequest = time.Now()
	s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("contacted bank after cancellation")
		return response(500, ``), nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Verify(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestNoBankCallForWrongBindingOrRevocation(t *testing.T) {
	s := sourceFixture(t)
	s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected bank request"); return nil, nil })
	c := connection(s)
	c.CredentialID = "another-app"
	if _, err := s.Fetch(t.Context(), c); !errors.Is(err, ranjit.ErrProviderCredentials) {
		t.Fatal(err)
	}
	c = connection(s)
	c.Status = "revoked"
	if _, err := s.Fetch(t.Context(), c); !errors.Is(err, ranjit.ErrConflict) {
		t.Fatal(err)
	}
}

func TestRejectUnsafeResponses(t *testing.T) {
	for _, status := range []int{200, 307, 401, 403, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			s := sourceFixture(t)
			calls := 0
			s.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				r := response(status, strings.Repeat(" ", maxResponseBytes+1))
				r.Header.Set("Location", "https://untrusted.example/")
				return r, nil
			})
			if err := s.Verify(t.Context()); err == nil || calls != 1 {
				t.Fatalf("unsafe response accepted: %v (%d calls)", err, calls)
			}
		})
	}
}
