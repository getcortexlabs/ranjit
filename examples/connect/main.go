// Run with make example. Open the printed URL to approve the sandbox consent.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/getcortexlabs/ranjit"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	base := os.Getenv("RANJIT_BASE_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	client, err := ranjit.NewClient(base, os.Getenv("RANJIT_API_KEY"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	institutions, err := client.Institutions(ctx)
	if err != nil {
		return err
	}
	if len(institutions.Data) == 0 {
		return fmt.Errorf("no configured institution")
	}
	result, err := client.Connect(ctx, ranjit.ConnectRequest{CustomerID: "local-user", InstitutionID: institutions.Data[0].ID})
	if err != nil {
		return err
	}
	if result.AuthorizationURL != "" {
		fmt.Printf("Autorize os dados de teste no navegador:\n%s\n", result.AuthorizationURL)
	} else {
		fmt.Printf("Conexão com %s criada. Aguardando sincronização.\n", institutions.Data[0].Name)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		connection, err := client.Connection(ctx, result.Connection.ID)
		if err != nil {
			return err
		}
		if connection.Status == "denied" || connection.Status == "revoked" || connection.Status == "expired" {
			return fmt.Errorf("connection is %s", connection.Status)
		}
		if connection.LastSyncedAt == nil {
			continue
		}
		accounts, err := client.Accounts(ctx, connection.ID, "", 100)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err = enc.Encode(accounts); err != nil {
			return err
		}
		for _, account := range accounts.Data {
			transactions, err := client.Transactions(ctx, account.ID, "", 100)
			if err != nil {
				return err
			}
			if err = enc.Encode(transactions); err != nil {
				return err
			}
		}
		return nil
	}
}
