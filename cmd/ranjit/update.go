package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/getcortexlabs/ranjit/internal/store"
	"github.com/getcortexlabs/ranjit/internal/updater"
	"os"
	"path/filepath"
	"time"
)

func (c cli) update(ctx context.Context, args []string) error {
	f := c.flags("update")
	check := f.Bool("check", false, "somente consultar a versão estável")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("use ranjit update [--check]")
	}
	client := updater.New()
	latest, err := client.Latest(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Instalada: %s. Estável: %s.\n", version, latest)
	if *check || version == latest {
		return nil
	}
	if version == "dev" {
		return errors.New("build de desenvolvimento; instale um release para usar atualização automática")
	}
	if !updater.Newer(latest, version) {
		return errors.New("atualização recusada: versão atual não é uma estável anterior")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	staged, err := client.Stage(ctx, latest, exe)
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	cfg, err := localConfig(true)
	if err != nil {
		return err
	}
	copy := cfg.Database + ".before-update-" + time.Now().UTC().Format("20060102T150405.000000000") + ".db"
	release, err := store.BackupStopped(ctx, cfg.Database, copy)
	if err != nil {
		return err
	}
	defer release()
	previous, err := updater.Install(staged, exe)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Atualizado para %s. Binário anterior: %s\n", latest, previous)
	if _, err = os.Stat(copy); err == nil {
		fmt.Fprintln(c.out, "Backup anterior à atualização:", copy)
	}
	fmt.Fprintln(c.out, "Rode ranjit ui ou ranjit service start para abrir a nova versão.")
	return nil
}
