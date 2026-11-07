package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getcortexlabs/ranjit/internal/statement"
	"github.com/getcortexlabs/ranjit/internal/store"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (c cli) importFile(ctx context.Context, args []string) error {
	f := c.flags("import")
	account := f.String("account", "", "nome da conta; obrigatório em CSV")
	format := f.String("format", "", "ofx ou csv; padrão: extensão do arquivo")
	decimal := f.String("decimal", ".", "separador decimal: . ou , (sem separador de milhares)")
	dateFormat := f.String("date-format", "", "layout Go opcional; ex.: 01/02/2006 para mês/dia/ano")
	jsonOutput := f.Bool("json", false, "imprimir resultado em JSON")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 1 {
		return errors.New("use ranjit import [--account pessoal] extrato.ofx (opções antes do arquivo)")
	}
	if *format == "" {
		*format = strings.TrimPrefix(strings.ToLower(filepath.Ext(f.Arg(0))), ".")
	}
	file, err := os.Open(f.Arg(0))
	if err != nil {
		return err
	}
	defer file.Close()
	input, err := statement.Parse(file, statement.Options{Format: *format, Account: *account, Decimal: *decimal, DateFormat: *dateFormat})
	if err != nil {
		return err
	}
	cfg, err := localConfig(true)
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := db.Import(ctx, "personal", *account, input, time.Now())
	if err != nil {
		return err
	}
	if *jsonOutput {
		return json.NewEncoder(c.out).Encode(result)
	}
	if result.Duplicate {
		fmt.Fprintln(c.out, "Este arquivo já foi importado. Nenhum lançamento foi duplicado.")
	} else {
		fmt.Fprintf(c.out, "Importado: %d conta(s), %d lançamento(s).\n", result.Accounts, result.Transactions)
	}
	if result.Deduplication == "exact_file" {
		fmt.Fprintln(c.out, "CSV sem IDs: repetir este arquivo é seguro; novos arquivos precisam cobrir períodos separados.")
	}
	fmt.Fprintln(c.out, "Use ranjit accounts ou ranjit transactions para consultar.")
	return nil
}
