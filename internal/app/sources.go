// Package app assembles configured providers without exposing credentials to the API.
package app

import (
	"fmt"

	"github.com/getcortexlabs/ranjit/internal/config"
	"github.com/getcortexlabs/ranjit/internal/provider"
	"github.com/getcortexlabs/ranjit/internal/provider/c6"
	"github.com/getcortexlabs/ranjit/internal/provider/efi"
	"github.com/getcortexlabs/ranjit/internal/provider/inter"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
	"github.com/getcortexlabs/ranjit/internal/provider/santander"
	"github.com/getcortexlabs/ranjit/internal/provider/sicoob"
)

func Source(c config.Config) (provider.Source, func(), error) {
	if c.Provider == "profiles" {
		r := provider.NewRegistry()
		for _, p := range c.Profiles {
			s, err := Profile(p)
			if err != nil {
				r.Close()
				return nil, nil, fmt.Errorf("profile %s: %w", p.Name, err)
			}
			if err = r.Add(p.Name, s); err != nil {
				r.Close()
				return nil, nil, err
			}
		}
		return r, r.Close, nil
	}
	var s provider.Source
	var err error
	switch c.Provider {
	case "sandbox":
		s = sandbox.Source{}
	case "efi":
		s, err = efi.New(c.EFI)
	case "inter":
		s, err = inter.New(c.Inter)
	case "sicoob":
		s, err = sicoob.New(c.Sicoob)
	case "c6":
		s, err = c6.New(c.C6)
	case "santander":
		s, err = santander.New(c.Santander)
	default:
		err = fmt.Errorf("unknown provider")
	}
	return s, func() {
		if c, ok := s.(interface{ Close() }); ok {
			c.Close()
		}
	}, err
}

func Profile(p config.Profile) (provider.Source, error) {
	switch p.Provider {
	case "sandbox":
		return sandbox.Source{}, nil
	case "efi":
		return efi.New(efi.Config{Environment: p.Environment, ClientID: p.ClientID, ClientSecretFile: p.ClientSecretFile, CertificateFile: p.CertificateFile, PrivateKeyFile: p.PrivateKeyFile, LookbackDays: p.LookbackDays})
	case "inter":
		return inter.New(inter.Config{Environment: p.Environment, ClientID: p.ClientID, ClientSecretFile: p.ClientSecretFile, CertificateFile: p.CertificateFile, PrivateKeyFile: p.PrivateKeyFile, Account: p.Account, LookbackDays: p.LookbackDays})
	case "sicoob":
		return sicoob.New(sicoob.Config{Environment: p.Environment, ClientID: p.ClientID, AccessTokenFile: p.AccessTokenFile, CertificateFile: p.CertificateFile, PrivateKeyFile: p.PrivateKeyFile, Account: p.Account, LookbackDays: p.LookbackDays})
	case "c6":
		return c6.New(c6.Config{Environment: p.Environment, ClientID: p.ClientID, ClientSecretFile: p.ClientSecretFile, CertificateFile: p.CertificateFile, PrivateKeyFile: p.PrivateKeyFile, LookbackDays: p.LookbackDays})
	case "santander":
		return santander.New(santander.Config{Environment: p.Environment, ClientID: p.ClientID, ClientSecretFile: p.ClientSecretFile, CertificateFile: p.CertificateFile, PrivateKeyFile: p.PrivateKeyFile, Account: p.Account, LookbackDays: p.LookbackDays})
	default:
		return nil, fmt.Errorf("unknown provider")
	}
}
