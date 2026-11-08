// Local demo tooling. No bank credentials or financial data leave this stack.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
	_ "modernc.org/sqlite"
)

var httpClient = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("expected init, health or verify")
	}
	switch args[0] {
	case "init":
		file, err := os.OpenFile("/config/.env", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(file, "RANJIT_API_KEY=%s\nAPP_TOKEN=%s\nRANJIT_WEBHOOK_SECRET=%s\n", rand.Text()+rand.Text(), rand.Text()+rand.Text(), rand.Text()+rand.Text())
		return errors.Join(err, file.Close())
	case "health":
		if len(args) != 2 {
			return errors.New("health requires a URL")
		}
		resp, err := httpClient.Get(args[1])
		if err != nil {
			return errors.New("service not reachable")
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("service returned %d", resp.StatusCode)
		}
		return nil
	case "verify":
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		return verify(ctx)
	default:
		return errors.New("unknown command")
	}
}

func verify(ctx context.Context) error {
	client, err := ranjit.NewClientFromEnv()
	if err != nil {
		return err
	}
	client.ReadRetries = ranjit.RetryPolicy{MaxAttempts: 3, MaxElapsedTime: 5 * time.Second}
	// Stable external IDs make rerunning the demo safe, including after restart.
	statement := "date,description,amount,id\n2026-11-01,Demo deposit,100.00,quickstart-deposit\n2026-11-02,Demo purchase,-12.50,quickstart-purchase\n"
	var imported ranjit.ImportResult
	if err := backend(ctx, "POST", "/statements", statement, &imported); err != nil {
		return err
	}
	var page ranjit.Page[ranjit.Transaction]
	if err := backend(ctx, "GET", "/transactions", "", &page); err != nil {
		return err
	}
	if imported.Transactions != 2 || len(page.Data) != 2 {
		return errors.New("CSV import did not produce the two expected transactions")
	}
	fmt.Println("CSV imported through the Go backend; repeated imports keep the same transactions.")
	// Reuse the demo connection beyond the idempotency window, too.
	var connection ranjit.Connection
	for c, err := range client.IterConnections(ctx, "quickstart-sandbox", 20) {
		if err != nil {
			return err
		}
		if c.Status == "active" {
			connection = c
			break
		}
	}
	if connection.ID == "" {
		result, err := client.ConnectWithKey(ctx, ranjit.ConnectRequest{CustomerID: "quickstart-sandbox", InstitutionID: "sandbox-bank"}, "quickstart-sandbox-v1")
		if err != nil {
			return err
		}
		connection = result.Connection
		if result.AuthorizationURL != "" {
			u, err := url.Parse(result.AuthorizationURL)
			if err != nil || u.Host != "localhost:8080" || u.Scheme != "http" || u.Path != "/sandbox/authorize" {
				return errors.New("unexpected sandbox authorization URL")
			}
			form := url.Values{"token": {u.Query().Get("token")}, "decision": {"approve"}}
			req, err := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:8080/sandbox/authorize", strings.NewReader(form.Encode()))
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := httpClient.Do(req)
			if err != nil {
				return errors.New("sandbox authorization unreachable")
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				return fmt.Errorf("sandbox authorization returned %d", resp.StatusCode)
			}
		}
	}
	if err := wait(ctx, func() (bool, error) {
		status, err := client.Status(ctx, connection.ID)
		return status.Accounts == 2 && status.Transactions == 4, err
	}); err != nil {
		return err
	}
	count := 0
	for _, err := range client.IterSearchTransactions(ctx, ranjit.TransactionFilter{CustomerID: "quickstart-sandbox", Limit: 1}) {
		if err != nil {
			return err
		}
		count++
	}
	if count != 4 {
		return fmt.Errorf("sandbox pagination returned %d transactions, want 4", count)
	}
	fmt.Println("Sandbox authorized and synced; the SDK read all four transactions across pages.")
	db, err := sql.Open("sqlite", "file:/inbox/webhook-inbox.db?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	if err := wait(ctx, func() (bool, error) {
		rows, err := db.QueryContext(ctx, `SELECT payload FROM webhook_inbox`)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		found := map[string]bool{}
		for rows.Next() {
			var raw string
			var event ranjit.WebhookEvent
			if err := rows.Scan(&raw); err != nil {
				return false, err
			}
			if err := json.Unmarshal([]byte(raw), &event); err != nil {
				return false, err
			}
			if event.Data.ConnectionID == connection.ID {
				found[event.Type] = true
			}
		}
		return found["connection.activated"] && found["sync.succeeded"], rows.Err()
	}); err != nil {
		return err
	}
	fmt.Println("Signed lifecycle and sync webhooks committed to the receiver inbox.")
	fmt.Println("Ready: backend http://localhost:3000 · Ranjit http://localhost:8080")
	return nil
}

func backend(ctx context.Context, method, path, body string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:3000"+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("APP_TOKEN"))
	req.Header.Set("Content-Type", "text/csv")
	resp, err := httpClient.Do(req)
	if err != nil {
		return errors.New("demo backend unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("demo backend returned %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func wait(ctx context.Context, check func() (bool, error)) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		ok, err := check()
		if err != nil || ok {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("demo verification timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
