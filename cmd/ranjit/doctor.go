package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/getcortexlabs/ranjit/internal/app"
	"github.com/getcortexlabs/ranjit/internal/config"
	"github.com/getcortexlabs/ranjit/internal/provider"
	"github.com/getcortexlabs/ranjit/internal/store"
	"time"
)

func (c cli) doctor(ctx context.Context, args []string) error {
	f := c.flags("doctor")
	live := f.Bool("live", false, "consultar saldo usando as credenciais configuradas")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("use ranjit doctor [--live]")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("não foi possível abrir o banco local: %w", err)
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "OK  banco local e configuração")
	for _, cert := range cfg.Certificates(time.Now()) {
		if cert.Level != "ok" {
			fmt.Fprintf(c.out, "AVISO  %s: certificado %s (%d dias restantes)\n", cert.Profile, cert.Level, cert.DaysRemaining)
		}
	}
	profiles := cfg.Profiles
	if cfg.Provider != "profiles" {
		source, closeSource, err := app.Source(cfg)
		if err != nil {
			return err
		}
		defer closeSource()
		return c.checkSource(ctx, cfg.Provider, source, *live)
	}
	failed := false
	for _, p := range profiles {
		s, err := app.Profile(p)
		if err == nil {
			err = c.checkSource(ctx, p.Name, s, *live)
			if closer, ok := s.(interface{ Close() }); ok {
				closer.Close()
			}
		}
		if err != nil {
			failed = true
			fmt.Fprintf(c.out, "ERRO  %s: %v\n", p.Name, err)
		}
	}
	if len(profiles) == 0 {
		fmt.Fprintln(c.out, "OK  importação OFX/CSV pronta; conexão bancária é opcional")
	}
	if failed {
		return errors.New("corrija os perfis indicados e rode ranjit doctor novamente")
	}
	return nil
}

func (c cli) checkSource(ctx context.Context, name string, s provider.Source, live bool) error {
	if bank, ok := s.(provider.CredentialSource); ok {
		fmt.Fprintf(c.out, "OK  %s: arquivos e certificado; conector experimental\n", name)
		if live {
			checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			if err := bank.Verify(checkCtx); err != nil {
				return err
			}
			fmt.Fprintf(c.out, "OK  %s: saldo acessível; a conferência do extrato ainda precisa ser feita\n", name)
		}
	} else {
		fmt.Fprintf(c.out, "OK  %s: dados simulados\n", name)
	}
	return nil
}
