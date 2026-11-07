package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/getcortexlabs/ranjit/internal/app"
	"github.com/getcortexlabs/ranjit/internal/service"
	"github.com/getcortexlabs/ranjit/internal/store"
	"time"
)

func (c cli) maintenance(ctx context.Context, command string, args []string) error {
	f := c.flags(command)
	var replace bool
	if command == "restore" {
		f.BoolVar(&replace, "replace", false, "substituir banco local, guardando cópia anterior")
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 1 {
		return fmt.Errorf("use ranjit %s [opções] arquivo-ou-ID", command)
	}
	cfg, err := localConfig(command == "restore")
	if err != nil {
		return err
	}
	if command == "restore" {
		rollback, err := store.Restore(ctx, f.Arg(0), cfg.Database, replace)
		if err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Dados restaurados. Conexões bancárias ficam pausadas; use ranjit renew ID para retomá-las.")
		if rollback != "" {
			fmt.Fprintln(c.out, "Cópia anterior:", rollback)
		}
		return nil
	}
	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	switch command {
	case "backup":
		if err = db.Backup(ctx, f.Arg(0)); err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Cópia salva:", f.Arg(0), "— contém dados financeiros; não inclui configuração nem credenciais.")
	case "pause":
		if err = db.Pause(ctx, f.Arg(0), time.Now()); err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Coleta pausada. Histórico preservado.")
	case "renew":
		source, closeSource, err := app.Source(cfg)
		if err != nil {
			return err
		}
		defer closeSource()
		if _, err = service.New(db, source, cfg.BaseURL).Renew(ctx, f.Arg(0)); err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Acesso renovado. Sincronização enfileirada; rode ranjit serve ou ranjit sync.")
	default:
		return errors.New("unknown maintenance command")
	}
	return nil
}
