package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getcortexlabs/ranjit/internal/app"
	"github.com/getcortexlabs/ranjit/internal/config"
	"github.com/getcortexlabs/ranjit/internal/validation"
	"os"
	"time"
)

func (c cli) validate(ctx context.Context, args []string) error {
	f := c.flags("validate")
	path := f.String("report", "ranjit-validation.json", "relatório sem saldos, transações ou credenciais")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 1 {
		return errors.New("use ranjit validate --report relatorio.json perfil; exige perfil configurado e acesso habilitado para banco real")
	}
	cfg, err := localConfig(false)
	if err != nil {
		return err
	}
	var selected *config.Profile
	for _, p := range cfg.Profiles {
		if p.Name == f.Arg(0) {
			selected = &p
			break
		}
	}
	if selected == nil {
		return errors.New("perfil não encontrado; configure o acesso com ranjit configure (ou use o perfil demo)")
	}
	// Reserve the report before making requests; never overwrite an earlier result.
	file, err := os.OpenFile(*path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	source, err := app.Profile(*selected)
	if err != nil {
		_ = json.NewEncoder(file).Encode(map[string]string{"result": "not_run", "error_code": "configuration_invalid"})
		return errors.New("perfil inválido; use ranjit doctor")
	}
	if closer, ok := source.(interface{ Close() }); ok {
		defer closer.Close()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	report, checkErr := validation.Run(ctx, source, version, selected.Environment)
	if err = json.NewEncoder(file).Encode(report); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Relatório: %s. Resultado: %s.\n", *path, report.Result)
	fmt.Fprintln(c.out, "A conferência com o extrato oficial continua manual. Este teste não certifica o conector.")
	if checkErr != nil {
		return fmt.Errorf("validação incompleta (%s); use ranjit doctor --live", report.ErrorCode)
	}
	return nil
}
