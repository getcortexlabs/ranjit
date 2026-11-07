package santander

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
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
	cfg := Config{Environment: "sandbox", Account: "0001.000000123456", ClientID: "test-app", ClientSecretFile: filepath.Join(dir, "secret"), CertificateFile: filepath.Join(dir, "cert.pem"), PrivateKeyFile: filepath.Join(dir, "key.pem"), LookbackDays: 90}
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
	return ranjit.Connection{ID: "con-test", Provider: "santander", InstitutionID: "santander", Status: "active", AuthorizationMode: "api_credentials", CredentialID: s.CredentialID(), CreatedAt: now, ConsentExpiresAt: now.Add(time.Hour)}
}

const authorized = `{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`

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
		if r.URL.Path == "/auth/oauth/v2/token" {
			n := tokens.Add(1)
			return response(200, strings.Replace(authorized, "test-token", fmtToken(n), 1)), nil
		}
		if balances.Add(1) == 1 {
			return response(401, `{"message":"secret upstream data"}`), nil
		}
		return response(200, `{"availableAmount":"1.00","availableAmountCurrency":"BRL"}`), nil
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

func TestCancelledRequestReturnsContextError(t *testing.T) {
	s := sourceFixture(t)
	s.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Verify(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
