package httpapi

import (
	"fmt"
	"net/http"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/store"
)

func (h *Server) connect(w http.ResponseWriter, r *http.Request) {
	var input ranjit.ConnectRequest
	if err := decode(r, &input); err != nil {
		h.problem(w, err)
		return
	}
	result, err := h.service.Connect(r.Context(), input)
	if err != nil {
		h.problem(w, err)
		return
	}
	w.Header().Set("Location", "/v1/connections/"+result.Connection.ID)
	write(w, http.StatusCreated, result)
}

func (h *Server) connections(w http.ResponseWriter, r *http.Request) {
	cursor, limit, err := pagination(r)
	if err != nil {
		h.problem(w, err)
		return
	}
	customer := r.URL.Query().Get("customer_id")
	if customer == "" || len(customer) > 128 {
		h.problem(w, fmt.Errorf("%w: customer_id is required (maximum 128 bytes)", ranjit.ErrInvalid))
		return
	}
	result, err := h.service.Store.Connections(r.Context(), customer, cursor, limit, h.service.Now())
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, result)
}

func (h *Server) connection(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Store.Connection(r.Context(), r.PathValue("id"), h.service.Now())
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, result)
}

func (h *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Store.Revoke(r.Context(), r.PathValue("id"), h.service.Now()); err != nil {
		h.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Server) sync(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Store.Enqueue(r.Context(), r.PathValue("id"), h.service.Now())
	if err != nil {
		h.problem(w, err)
		return
	}
	w.Header().Set("Location", "/v1/jobs/"+result.ID)
	write(w, http.StatusAccepted, result)
}

func (h *Server) pause(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Store.Pause(r.Context(), r.PathValue("id"), h.service.Now()); err != nil {
		h.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Server) renew(w http.ResponseWriter, r *http.Request) {
	c, err := h.service.Renew(r.Context(), r.PathValue("id"))
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, c)
}

func (h *Server) accounts(w http.ResponseWriter, r *http.Request) {
	cursor, limit, err := pagination(r)
	if err != nil {
		h.problem(w, err)
		return
	}
	result, err := h.service.Store.Accounts(r.Context(), r.PathValue("id"), cursor, limit, h.service.Now())
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, result)
}

func (h *Server) transactions(w http.ResponseWriter, r *http.Request) {
	cursor, limit, err := pagination(r)
	if err != nil {
		h.problem(w, err)
		return
	}
	result, err := h.service.Store.Transactions(r.Context(), r.PathValue("id"), cursor, limit, h.service.Now())
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, result)
}

func (h *Server) job(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Store.Job(r.Context(), r.PathValue("id"))
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, result)
}

func (h *Server) searchTransactions(w http.ResponseWriter, r *http.Request) {
	cursor, limit, err := pagination(r)
	if err != nil {
		h.problem(w, err)
		return
	}
	q := r.URL.Query()
	result, err := h.service.Store.SearchTransactions(r.Context(), store.Search{Customer: q.Get("customer_id"), Account: q.Get("account_id"), Query: q.Get("q"), From: q.Get("from"), Through: q.Get("through"), Cursor: cursor, Limit: limit}, h.service.Now())
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, 200, result)
}
