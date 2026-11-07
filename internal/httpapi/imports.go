package httpapi

import (
	"net/http"

	"github.com/getcortexlabs/ranjit/internal/statement"
)

func (h *Server) importStatement(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	input, err := statement.Parse(r.Body, statement.Options{Format: q.Get("format"), Account: q.Get("account"), Decimal: q.Get("decimal"), DateFormat: q.Get("date_format")})
	if err != nil {
		h.problem(w, err)
		return
	}
	result, err := h.service.Store.Import(r.Context(), q.Get("customer_id"), q.Get("account"), input, h.service.Now())
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, result)
}

func (h *Server) status(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Store.Status(r.Context(), r.PathValue("id"), h.service.Now())
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, result)
}
