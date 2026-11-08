package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/store"
)

type Worker struct {
	Store  *store.Store
	URL    string
	Secret string
	Client *http.Client
	Now    func() time.Time
	Logger *slog.Logger
}

func New(db *store.Store, destination, secret string, logger *slog.Logger) *Worker {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Worker{Store: db, URL: destination, Secret: secret, Logger: logger, Now: time.Now, Client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (w *Worker) Run(ctx context.Context) {
	defer w.Client.CloseIdleConnections()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := w.Step(ctx); err != nil && ctx.Err() == nil {
			w.Logger.Error("webhook delivery could not be recorded")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) Step(ctx context.Context) error {
	attempt, err := w.Store.ClaimWebhook(ctx, w.Now())
	if errors.Is(err, ranjit.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(attempt.Payload))
	if err != nil {
		return err
	}
	timestamp := strconv.FormatInt(w.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(w.Secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(attempt.Payload)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Ranjit-Webhook/1")
	req.Header.Set("Ranjit-Event-ID", attempt.ID)
	req.Header.Set("Ranjit-Signature", "t="+timestamp+",v1="+hex.EncodeToString(mac.Sum(nil)))
	status := 0
	resp, err := w.Client.Do(req)
	if err == nil {
		status = resp.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
	}
	// A shutdown leaves the lease recoverable instead of consuming another retry.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	err = w.Store.FinishWebhook(ctx, attempt, status, w.Now())
	if errors.Is(err, ranjit.ErrLease) {
		return nil
	}
	return err
}
