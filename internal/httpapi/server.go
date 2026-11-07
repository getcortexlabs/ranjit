package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/service"
	"github.com/getcortexlabs/ranjit/internal/statement"
)

type Server struct {
	sessions sessions
	service  *service.Service
	key      [32]byte
	logger   *slog.Logger
}

// Option adds authenticated operational information without exposing credentials.
type Option func(*http.ServeMux)

func Diagnostics(fn func() any) Option {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /v1/diagnostics", func(w http.ResponseWriter, r *http.Request) { write(w, 200, fn()) })
	}
}

func New(s *service.Service, apiKey string, logger *slog.Logger, options ...Option) http.Handler {
	h := &Server{service: s, key: sha256.Sum256([]byte(apiKey)), logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /app", h.dashboard)
	mux.HandleFunc("GET /app/", h.dashboard)
	mux.HandleFunc("POST /ui/session", h.uiSession)
	mux.HandleFunc("POST /ui/logout", h.uiLogout)
	mux.Handle("POST /ui/link", h.authenticate(http.HandlerFunc(h.uiLink)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Store.Ping(r.Context()); err != nil {
			write(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		write(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /sandbox/authorize", h.consentPage)
	mux.HandleFunc("POST /sandbox/authorize", h.decide)
	api := http.NewServeMux()
	for _, option := range options {
		option(api)
	}
	api.HandleFunc("GET /v1/institutions", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, ranjit.Page[ranjit.Institution]{Data: s.Source.Institutions()})
	})
	api.HandleFunc("POST /v1/connections", h.connect)
	api.HandleFunc("GET /v1/connections", h.connections)
	api.HandleFunc("GET /v1/connections/{id}", h.connection)
	api.HandleFunc("DELETE /v1/connections/{id}", h.revoke)
	api.HandleFunc("POST /v1/connections/{id}/sync", h.sync)
	api.HandleFunc("POST /v1/connections/{id}/pause", h.pause)
	api.HandleFunc("POST /v1/connections/{id}/renew", h.renew)
	api.HandleFunc("GET /v1/connections/{id}/accounts", h.accounts)
	api.HandleFunc("GET /v1/accounts/{id}/transactions", h.transactions)
	api.HandleFunc("GET /v1/jobs/{id}", h.job)
	api.HandleFunc("GET /v1/transactions", h.searchTransactions)
	api.HandleFunc("POST /v1/imports", h.importStatement)
	api.HandleFunc("POST /v1/imports/preview", h.previewImport)
	api.HandleFunc("POST /v1/imports/columns", h.csvHeaders)
	api.HandleFunc("GET /v1/connections/{id}/status", h.status)
	mux.Handle("/v1/", h.authenticate(api))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { h.problem(w, ranjit.ErrNotFound) })
	return h.middleware(mux)
}

func (h *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" && localBrowser(r) && sameOrigin(r) {
			if cookie, err := r.Cookie("ranjit_session"); err == nil && h.sessions.valid(cookie.Value, false) {
				next.ServeHTTP(w, r)
				return
			}
		}
		prefix, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		got := sha256.Sum256([]byte(token))
		if !ok || !strings.EqualFold(prefix, "Bearer") || token == "" || subtle.ConstantTimeCompare(got[:], h.key[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			write(w, http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "unauthorized", "message": "a valid bearer token is required"}})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
		r.ResponseWriter.WriteHeader(status)
	}
}
func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(p)
}

func (h *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := ranjit.NewID("req")
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		rw := &recorder{ResponseWriter: w}
		defer func() {
			if recover() != nil {
				h.logger.Error("request panic", "request_id", id)
				h.problem(rw, errors.New("internal error"))
			}
			// No URL, query, headers, body or customer identifiers in request logs.
			h.logger.Info("request", "request_id", id, "method", r.Method, "status", rw.status, "duration_ms", time.Since(start).Milliseconds())
		}()
		limit := int64(16 << 10)
		if r.Method == http.MethodPost && (r.URL.Path == "/v1/imports" || r.URL.Path == "/v1/imports/preview" || r.URL.Path == "/v1/imports/columns") {
			limit = statement.MaxBytes
		}
		r.Body = http.MaxBytesReader(rw, r.Body, limit)
		next.ServeHTTP(rw, r)
	})
}

func decode(r *http.Request, dst any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return fmt.Errorf("%w: Content-Type must be application/json", ranjit.ErrInvalid)
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err = dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: malformed JSON body", ranjit.ErrInvalid)
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: expected one JSON value", ranjit.ErrInvalid)
	}
	return nil
}

func pagination(r *http.Request) (string, int, error) {
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 128 {
		return "", 0, fmt.Errorf("%w: cursor too long", ranjit.ErrInvalid)
	}
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			return "", 0, fmt.Errorf("%w: limit must be between 1 and 100", ranjit.ErrInvalid)
		}
	}
	return cursor, limit, nil
}

func write(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Server) problem(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "request could not be completed"
	switch {
	case errors.Is(err, ranjit.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "resource not found"
	case errors.Is(err, ranjit.ErrConsent):
		status, code, message = http.StatusGone, "invalid_authorization", ranjit.ErrConsent.Error()
	case errors.Is(err, ranjit.ErrConflict):
		status, code, message = http.StatusConflict, "inactive_connection", ranjit.ErrConflict.Error()
	case errors.Is(err, ranjit.ErrInvalid):
		status, code, message = http.StatusBadRequest, "invalid_request", err.Error()
	case errors.Is(err, ranjit.ErrProviderCredentials):
		status, code, message = http.StatusBadGateway, "provider_credentials", ranjit.ErrProviderCredentials.Error()
	case errors.Is(err, ranjit.ErrProviderUnavailable):
		status, code, message = http.StatusBadGateway, "provider_unavailable", ranjit.ErrProviderUnavailable.Error()
	default:
		h.logger.Error("request failed", "request_id", w.Header().Get("X-Request-ID"))
	}
	write(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
