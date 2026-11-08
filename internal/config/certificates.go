package config

import (
	"crypto/x509"
	"encoding/pem"
	"math"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

// CertificateStatus deliberately omits certificate subjects, serials and paths.
type CertificateStatus = ranjit.CertificateStatus

func (c Config) Certificates(now time.Time) []CertificateStatus {
	profiles := c.Profiles
	if c.Provider != "profiles" {
		path := ""
		switch c.Provider {
		case "efi":
			path = c.EFI.CertificateFile
		case "inter":
			path = c.Inter.CertificateFile
		case "sicoob":
			path = c.Sicoob.CertificateFile
		case "c6":
			path = c.C6.CertificateFile
		case "santander":
			path = c.Santander.CertificateFile
		}
		profiles = []Profile{{Name: c.Provider, CertificateFile: path}}
	}
	out := []CertificateStatus{}
	for _, p := range profiles {
		if p.CertificateFile == "" {
			continue
		}
		status := CertificateStatus{Profile: p.Name, Level: "unreadable"}
		if cert := readCertificate(p.CertificateFile); cert != nil {
			end := cert.NotAfter
			status.ExpiresAt = &end
			status.DaysRemaining = int(math.Ceil(end.Sub(now).Hours() / 24))
			switch {
			case now.Before(cert.NotBefore):
				status.Level = "not_yet_valid"
			case !now.Before(end):
				status.Level = "expired"
			case status.DaysRemaining <= 7:
				status.Level = "urgent"
			case status.DaysRemaining <= 30:
				status.Level = "warning"
			default:
				status.Level = "ok"
			}
		}
		out = append(out, status)
	}
	return out
}
func readCertificate(path string) *x509.Certificate {
	data, err := provider.ReadPEM(path)
	if err != nil {
		return nil
	}
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			return nil
		}
		data = rest
		if block.Type == "CERTIFICATE" {
			cert, _ := x509.ParseCertificate(block.Bytes)
			return cert
		}
	}
	return nil
}
