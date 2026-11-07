package httpapi

import (
	"github.com/getcortexlabs/ranjit"
	"net/http"

	"github.com/getcortexlabs/ranjit/internal/statement"
)

func (h *Server) importStatement(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	input, err := statement.Parse(r.Body, importOptions(r))
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

func importOptions(r *http.Request) statement.Options {
	q := r.URL.Query()
	return statement.Options{Format: q.Get("format"), Account: q.Get("account"), Decimal: q.Get("decimal"), DateFormat: q.Get("date_format"), Delimiter: q.Get("delimiter"), Columns: map[string]string{"date": q.Get("date_column"), "amount": q.Get("amount_column"), "description": q.Get("description_column"), "id": q.Get("id_column")}}
}
func (h *Server) previewImport(w http.ResponseWriter, r *http.Request) {
	input, err := statement.Parse(r.Body, importOptions(r))
	if err != nil {
		h.problem(w, err)
		return
	}
	result, err := h.service.Store.PreviewImport(r.Context(), r.URL.Query().Get("customer_id"), r.URL.Query().Get("account"), input, h.service.Now())
	if err != nil {
		h.problem(w, err)
		return
	}
	sample := input.Snapshot.Transactions
	if len(sample) > 20 {
		sample = sample[:20]
	}
	out := ranjit.ImportPreview{Result: result, From: input.From, Through: input.Through, Sample: []ranjit.ImportSample{}}
	for _, t := range sample {
		out.Sample = append(out.Sample, ranjit.ImportSample{PostedOn: t.PostedOn, Description: t.Description, Amount: t.Amount})
	}
	write(w, http.StatusOK, out)
}
func (h *Server) csvHeaders(w http.ResponseWriter, r *http.Request) {
	headers, err := statement.CSVHeaders(r.Body, r.URL.Query().Get("delimiter"))
	if err != nil {
		h.problem(w, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"columns": headers})
}
