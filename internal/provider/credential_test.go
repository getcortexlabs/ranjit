package provider

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCredentialReferences(t *testing.T) {
	t.Setenv("BANK_SECRET", "secret-that-must-not-appear\n")
	secret, err := ReadSecretFile("env:BANK_SECRET")
	if err != nil || secret != "secret-that-must-not-appear" {
		t.Fatal("valid secret reference failed", err)
	}
	for _, value := range []string{"", "secret-that-must-not-appear\nsecond-line", strings.Repeat("x", 4097)} {
		t.Setenv("BANK_SECRET", value)
		if _, err := ReadSecretFile("env:BANK_SECRET"); err == nil || strings.Contains(err.Error(), "secret-that-must-not-appear") {
			t.Fatal("invalid credential accepted or exposed")
		}
	}
	for _, source := range []string{"env:", "env:invalid-name", "env: RANJIT_KEY", filepath.Join(t.TempDir(), "missing")} {
		if _, err := ReadSecretFile(source); err == nil {
			t.Fatal("missing source accepted")
		}
	}
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("from-file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if secret, err = ReadSecretFile(path); err != nil || secret != "from-file" {
		t.Fatal("file source broken", err)
	}
}

func TestMTLSFromEnvironment(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BANK_CERT", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	t.Setenv("BANK_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private})))
	client, err := NewMTLSClient("env:BANK_CERT", "env:BANK_KEY")
	if err != nil {
		t.Fatal(err)
	}
	client.CloseIdleConnections()
	t.Setenv("BANK_KEY", "invalid-private-key-do-not-log")
	if _, err = NewMTLSClient("env:BANK_CERT", "env:BANK_KEY"); err == nil || strings.Contains(err.Error(), "do-not-log") {
		t.Fatal("invalid key accepted or exposed")
	}
}
