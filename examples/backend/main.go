// A single-customer backend example. Its token belongs to this demo, and is
// deliberately different from the Ranjit service key. See README.md.
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/getcortexlabs/ranjit"
)

func main() {
	client, err := ranjit.NewClientFromEnv()
	if err != nil {
		log.Fatal(err)
	}
	client.ReadRetries = ranjit.RetryPolicy{MaxAttempts: 3, MaxElapsedTime: 10 * time.Second}
	token := os.Getenv("APP_TOKEN")
	if len(token) < 32 || token == os.Getenv("RANJIT_API_KEY") {
		log.Fatal("APP_TOKEN must be a separate secret of at least 32 characters")
	}
	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:3000"
	}
	server := &http.Server{Addr: addr, Handler: routes(client, token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: time.Minute}
	log.Print("example backend listening on ", addr)
	log.Fatal(server.ListenAndServe())
}

func routes(client *ranjit.Client, token string) http.Handler {
	// Replace this fixed identity with your authenticated application's user ID.
	// Never accept a customer ID from a query string or an unverified header.
	const customer = "example-customer"
	mux := http.NewServeMux()
	mux.HandleFunc("POST /statements", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		defer r.Body.Close()
		result, err := client.Import(r.Context(), r.Body, ranjit.ImportOptions{CustomerID: customer, Account: "personal", Format: "csv"})
		respond(w, result, err)
	})
	mux.HandleFunc("GET /transactions", func(w http.ResponseWriter, r *http.Request) {
		result, err := client.SearchTransactions(r.Context(), ranjit.TransactionFilter{CustomerID: customer, Cursor: r.URL.Query().Get("cursor"), Limit: 100})
		respond(w, result, err)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := client.Ready(ctx); err != nil {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func respond(w http.ResponseWriter, data any, err error) {
	if err != nil {
		var apiError *ranjit.APIError
		if errors.As(err, &apiError) && apiError.StatusCode == http.StatusBadRequest {
			http.Error(w, "invalid statement or pagination cursor", http.StatusBadRequest)
			return
		}
		http.Error(w, "financial service unavailable", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}
