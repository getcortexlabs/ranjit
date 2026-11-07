package httpapi

import (
	"html/template"
	"mime"
	"net/http"

	"github.com/getcortexlabs/ranjit"
)

var consentTemplate = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Ranjit · Sandbox</title></head>
<body><main><h1>Autorizar dados de teste</h1>
<p>Esta é uma simulação local do Ranjit. Nenhum banco será acessado.</p>
<p>Instituição: {{.Connection.InstitutionID}}<br>Cliente: {{.Connection.CustomerID}}</p>
<p>A aplicação que criou esta conexão poderá consultar contas, saldos e transações fictícias até {{.Connection.ConsentExpiresAt.Format "02/01/2006"}}.</p>
<form method="post" action="/sandbox/authorize"><input type="hidden" name="token" value="{{.Token}}">
<button type="submit" name="decision" value="approve">Autorizar simulação</button>
<button type="submit" name="decision" value="deny">Recusar</button></form>
</main></body></html>`))

func (h *Server) consentPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	c, err := h.service.Consent(r.Context(), token)
	if err != nil {
		h.problem(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err = consentTemplate.Execute(w, struct {
		Connection ranjit.Connection
		Token      string
	}{c, token}); err != nil {
		h.logger.Error("consent page write failed")
	}
}

func (h *Server) decide(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && origin != h.service.BaseURL {
		write(w, http.StatusForbidden, map[string]any{"error": map[string]string{"code": "invalid_origin", "message": "origin does not match this server"}})
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		h.problem(w, ranjit.ErrInvalid)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.problem(w, ranjit.ErrInvalid)
		return
	}
	c, err := h.service.Decide(r.Context(), r.PostForm.Get("token"), r.PostForm.Get("decision"))
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, c)
}
