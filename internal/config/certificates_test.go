package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCertificateWarningsDoNotDiscloseIdentity(t *testing.T) {
	now := time.Now()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "private-account-identity"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(5 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "secret-location.pem")
	os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
	cfg := Config{Provider: "profiles", Profiles: []Profile{{Name: "bank", CertificateFile: path}}}
	alerts := cfg.Certificates(now)
	if len(alerts) != 1 || alerts[0].Level != "urgent" || alerts[0].DaysRemaining != 5 {
		t.Fatal(alerts)
	}
	if got := cfg.Certificates(now.Add(6 * 24 * time.Hour)); got[0].Level != "expired" {
		t.Fatal(got)
	}
	data, _ := json.Marshal(alerts)
	if strings.Contains(string(data), "private-account") || strings.Contains(string(data), "secret-location") {
		t.Fatal("certificate identity disclosed")
	}
	os.WriteFile(path, []byte("invalid"), 0600)
	if got := cfg.Certificates(now); got[0].Level != "unreadable" {
		t.Fatal(got)
	}
}
