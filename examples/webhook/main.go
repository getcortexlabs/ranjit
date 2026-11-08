// This receiver commits authenticated events to an inbox before acknowledging
// delivery. Process the inbox in your backend, using event.id as a unique key.
package main

import (
	"context"
	"database/sql"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/getcortexlabs/ranjit"
	_ "modernc.org/sqlite"
)

func main() {
	secret := os.Getenv("RANJIT_WEBHOOK_SECRET")
	if len(secret) < 32 {
		log.Fatal("RANJIT_WEBHOOK_SECRET must contain at least 32 characters")
	}
	db, err := openInbox("webhook-inbox.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	server := &http.Server{Addr: "127.0.0.1:8081", Handler: receiver(db, secret), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: time.Minute}
	log.Print("webhook receiver listening on http://127.0.0.1:8081/webhooks/ranjit")
	log.Fatal(server.ListenAndServe())
}

func openInbox(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
        CREATE TABLE IF NOT EXISTS webhook_inbox(id TEXT PRIMARY KEY,payload TEXT NOT NULL,received_at INTEGER NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func receiver(db *sql.DB, secret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/ranjit", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		defer r.Body.Close()
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large or unreadable", http.StatusBadRequest)
			return
		}
		signatures := r.Header.Values("Ranjit-Signature")
		if len(signatures) != 1 {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		event, err := ranjit.VerifyWebhook(body, signatures[0], secret, time.Now())
		if err != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
		// The primary key makes redelivery safe, including after a restart.
		// Acknowledge only after commit; business work can consume this inbox.
		_, err = db.ExecContext(r.Context(), `INSERT INTO webhook_inbox(id,payload,received_at) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, event.ID, string(body), time.Now().UnixMilli())
		if err != nil {
			http.Error(w, "inbox unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
