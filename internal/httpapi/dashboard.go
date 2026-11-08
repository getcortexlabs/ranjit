package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/getcortexlabs/ranjit"
)

//go:embed web/*
var dashboardFiles embed.FS

type sessions struct {
	mu      sync.Mutex
	links   map[[32]byte]time.Time
	cookies map[[32]byte]time.Time
}

func (s *sessions) issue(link bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.links == nil {
		s.links = map[[32]byte]time.Time{}
		s.cookies = map[[32]byte]time.Time{}
	}
	target, ttl := s.cookies, 8*time.Hour
	if link {
		target, ttl = s.links, time.Minute
	}
	for key, until := range target {
		if time.Now().After(until) {
			delete(target, key)
		}
	}
	if len(target) >= 64 {
		for key := range target {
			delete(target, key)
			break
		}
	}
	token := rand.Text() + rand.Text()
	target[sha256.Sum256([]byte(token))] = time.Now().Add(ttl)
	return token
}
func (s *sessions) valid(token string, link bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	target := s.cookies
	if link {
		target = s.links
	}
	key := sha256.Sum256([]byte(token))
	until, ok := target[key]
	if link {
		delete(target, key)
	}
	return ok && time.Now().Before(until)
}
func localBrowser(r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback()
}
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Method == http.MethodGet || r.Method == http.MethodHead
	}
	u, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && u.Scheme == scheme && u.Host == r.Host && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
func (h *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	if !localBrowser(r) {
		http.Error(w, "O painel só está disponível em localhost.", http.StatusForbidden)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/app/")
	if r.URL.Path == "/app" || path == "" {
		path = "index.html"
	}
	if path != "index.html" && path != "app.js" && path != "app.css" {
		h.problem(w, ranjit.ErrNotFound)
		return
	}
	body, err := fs.ReadFile(dashboardFiles, "web/"+path)
	if err != nil {
		h.problem(w, err)
		return
	}
	types := map[string]string{"index.html": "text/html; charset=utf-8", "app.js": "text/javascript; charset=utf-8", "app.css": "text/css; charset=utf-8"}
	w.Header().Set("Content-Type", types[path])
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Write(body)
}
func (h *Server) uiLink(w http.ResponseWriter, r *http.Request) {
	if !localBrowser(r) {
		http.Error(w, "localhost required", 403)
		return
	}
	write(w, 200, map[string]string{"path": "/app/#" + h.sessions.issue(true)})
}
func (h *Server) uiSession(w http.ResponseWriter, r *http.Request) {
	if !localBrowser(r) || !sameOrigin(r) {
		http.Error(w, "origin rejected", 403)
		return
	}
	var input struct {
		Token string `json:"token"`
		Key   string `json:"key"`
	}
	if err := decode(r, &input); err != nil {
		h.problem(w, err)
		return
	}
	valid := input.Token != "" && h.sessions.valid(input.Token, true)
	if input.Key != "" {
		valid = h.validKey(input.Key)
	}
	if !valid {
		write(w, 401, map[string]any{"error": map[string]string{"message": "Acesso inválido ou expirado. Abra novamente com ranjit ui."}})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "ranjit_session", Value: h.sessions.issue(false), Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 8 * 3600})
	w.WriteHeader(204)
}
func (h *Server) uiLogout(w http.ResponseWriter, r *http.Request) {
	if !localBrowser(r) || !sameOrigin(r) {
		http.Error(w, "origin rejected", 403)
		return
	}
	if cookie, err := r.Cookie("ranjit_session"); err == nil {
		h.sessions.mu.Lock()
		delete(h.sessions.cookies, sha256.Sum256([]byte(cookie.Value)))
		h.sessions.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "ranjit_session", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(204)
}
