package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/store"
)

func (c cli) inspect(ctx context.Context, command string, args []string) error {
	f := c.flags(command)
	asJSON := f.Bool("json", false, "imprimir JSON")
	if err := f.Parse(args); err != nil {
		return err
	}
	if command == "revoke" {
		if f.NArg() != 1 {
			return errors.New("use ranjit revoke ID; os dados dessa conexão serão removidos")
		}
	} else if f.NArg() != 0 {
		return fmt.Errorf("use ranjit %s [--json]", command)
	}
	cfg, err := localConfig(false)
	if err != nil {
		return err
	}
	if command == "key" {
		fmt.Fprintln(c.out, cfg.APIKey)
		return nil
	}
	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	db.EnableWebhooks(cfg.WebhookURL)
	if command == "revoke" {
		if err = db.Revoke(ctx, f.Arg(0), time.Now()); err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Conexão revogada. Contas e lançamentos removidos.")
		return nil
	}
	connections, err := allConnections(ctx, db)
	if err != nil {
		return err
	}
	statuses := []ranjit.ConnectionStatus{}
	accounts := []ranjit.Account{}
	transactions := []ranjit.Transaction{}
	for _, connection := range connections {
		if command == "status" {
			status, err := db.Status(ctx, connection.ID, time.Now())
			if err != nil {
				return err
			}
			statuses = append(statuses, status)
			continue
		}
		if !connection.HistoryAvailable() {
			continue
		}
		cursor := ""
		for {
			page, err := db.Accounts(ctx, connection.ID, cursor, 100, time.Now())
			if err != nil {
				return err
			}
			accounts = append(accounts, page.Data...)
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
	}
	if command == "transactions" {
		for _, a := range accounts {
			cursor := ""
			for {
				page, err := db.Transactions(ctx, a.ID, cursor, 100, time.Now())
				if err != nil {
					return err
				}
				transactions = append(transactions, page.Data...)
				if page.NextCursor == "" {
					break
				}
				cursor = page.NextCursor
			}
		}
	}
	if *asJSON {
		switch command {
		case "status":
			return json.NewEncoder(c.out).Encode(statuses)
		case "accounts":
			return json.NewEncoder(c.out).Encode(accounts)
		default:
			return json.NewEncoder(c.out).Encode(transactions)
		}
	}
	w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	switch command {
	case "status":
		fmt.Fprintln(w, "CONEXÃO\tORIGEM\tESTADO\tÚLTIMA SINCRONIZAÇÃO\tLANÇAMENTOS\tJOB")
		for _, s := range statuses {
			synced := "nunca"
			if s.Connection.LastSyncedAt != nil {
				synced = s.Connection.LastSyncedAt.Local().Format("2006-01-02 15:04")
			}
			job := "—"
			if s.LatestJob != nil {
				job = fmt.Sprintf("%s (%d respostas)", s.LatestJob.Status, s.LatestJob.RequestsCompleted)
				if s.LatestJob.ErrorCode != "" {
					job += " " + s.LatestJob.ErrorCode
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\n", s.Connection.ID, terminal(s.Connection.InstitutionID), s.Connection.Status, synced, s.Transactions, job)
		}
	case "accounts":
		fmt.Fprintln(w, "CONTA\tNOME\tSALDO\tDATA DO SALDO")
		for _, a := range accounts {
			amount, at := "desconhecido", "—"
			if a.Balance != nil {
				amount = money(*a.Balance)
			}
			if a.BalanceAt != nil {
				at = a.BalanceAt.Format(time.DateOnly)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, terminal(a.Name), amount, at)
		}
	case "transactions":
		fmt.Fprintln(w, "DATA\tDESCRIÇÃO\tVALOR\tCONTA")
		for _, t := range transactions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.PostedOn, terminal(t.Description), money(t.Amount), t.AccountID)
		}
	}
	if err = w.Flush(); err != nil {
		return err
	}
	if len(connections) == 0 {
		fmt.Fprintln(c.out, "Nenhuma conta ainda. Use ranjit import extrato.ofx ou ranjit demo.")
	}
	return nil
}
