package provider

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"
)

// ReadSecretFile accepts a file path or an explicit env:VARIABLE reference.
// Errors never include credential contents.
func ReadSecretFile(path string) (string, error) {
	secret, err := readCredential(path, 4096)
	if err != nil {
		return "", errors.New("cannot read bank client secret file")
	}
	value := strings.TrimSpace(string(secret))
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", errors.New("bank client secret file is empty or invalid")
	}
	return value, nil
}

// NewMTLSClient uses system roots, a client certificate and bounded network timeouts.
// Redirects are refused so bank credentials never follow a different destination.
func NewMTLSClient(certificateFile, keyFile string) (*http.Client, error) {
	certificate, err := ReadPEM(certificateFile)
	if err != nil {
		return nil, err
	}
	key, err := ReadPEM(keyFile)
	if err != nil {
		return nil, err
	}
	cert, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		return nil, errors.New("cannot load bank PEM certificate and private key")
	}
	if cert.Leaf == nil {
		cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return nil, errors.New("cannot parse bank certificate")
		}
	}
	now := time.Now()
	if now.Before(cert.Leaf.NotBefore) || !now.Before(cert.Leaf.NotAfter) {
		return nil, errors.New("bank certificate is not currently valid")
	}
	client := NewTLSClient()
	client.Transport.(*http.Transport).TLSClientConfig.Certificates = []tls.Certificate{cert}
	return client, nil
}

// NewTLSClient is used by bank sandboxes that issue a static access token.
// It has the same timeouts and redirect policy as the mTLS transport.
func NewTLSClient() *http.Client {
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 60 * time.Second,
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
