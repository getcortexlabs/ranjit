package httpapi_test

import (
	"encoding/json"
	"github.com/getcortexlabs/ranjit"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDashboardSessionOriginReplayAndLogout(t *testing.T) {
	client, _, _, logs := setup(t)
	request := func(method, path, body, origin string, cookie *http.Cookie, bearer bool) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, "http://localhost:8080"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if bearer {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := client.HTTPClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	link := request("POST", "/ui/link", "", "", nil, true)
	var out struct {
		Path string `json:"path"`
	}
	json.NewDecoder(link.Body).Decode(&out)
	link.Body.Close()
	token := strings.TrimPrefix(out.Path, "/app/#")
	if token == out.Path || len(token) < 20 {
		t.Fatal("missing link")
	}
	body := `{"token":"` + token + `"}`
	for _, origin := range []string{"", "https://evil.example", "http://localhost:8080?oops"} {
		resp := request("POST", "/ui/session", body, origin, nil, false)
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Fatal("accepted origin", origin, resp.StatusCode)
		}
	}
	login := request("POST", "/ui/session", body, "http://localhost:8080", nil, false)
	login.Body.Close()
	if login.StatusCode != 204 || len(login.Cookies()) != 1 {
		t.Fatal("login", login.StatusCode)
	}
	cookie := login.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	replay := request("POST", "/ui/session", body, "http://localhost:8080", nil, false)
	replay.Body.Close()
	if replay.StatusCode != 401 {
		t.Fatal("replayed link")
	}
	for _, tc := range []struct {
		method, path, origin string
		status               int
	}{{"GET", "/v1/institutions", "", 200}, {"GET", "/v1/institutions", "https://evil.example", 401}, {"POST", "/v1/connections", "", 401}, {"POST", "/v1/connections", "https://evil.example", 401}} {
		resp := request(tc.method, tc.path, `{}`, tc.origin, cookie, false)
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatal(tc, resp.StatusCode)
		}
	}
	page := request("GET", "/app/", "", "", nil, false)
	html, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if !strings.Contains(page.Header.Get("Content-Security-Policy"), "script-src 'self'") || strings.Contains(string(html), key) {
		t.Fatal("dashboard CSP or secret")
	}
	hostile, _ := http.NewRequest("GET", "http://evil.example/app/", nil)
	resp, err := client.HTTPClient.Do(hostile)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("DNS rebinding host accepted")
	}
	logout := request("POST", "/ui/logout", "", "http://localhost:8080", cookie, false)
	logout.Body.Close()
	gone := request("GET", "/v1/institutions", "", "", cookie, false)
	gone.Body.Close()
	if gone.StatusCode != 401 {
		t.Fatal("logout retained session")
	}
	for _, secret := range []string{key, token, cookie.Value} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("secret in logs")
		}
	}
}

func TestSDKPreviewMappingAndSearch(t *testing.T) {
	client, _, _, _ := setup(t)
	ctx := t.Context()
	csv := "Quando;Texto;Quantia;Código\n07/11/2026;Mercado;-12,34;tx1\n"
	opts := ranjit.ImportOptions{CustomerID: "personal", Account: "pessoal", Format: "csv", Decimal: ",", Delimiter: ";", Columns: map[string]string{"date": "Quando", "description": "Texto", "amount": "Quantia", "id": "Código"}}
	preview, err := client.PreviewImport(ctx, strings.NewReader(csv), opts)
	if err != nil || preview.Result.New != 1 || preview.Sample[0].Amount.AmountMinor != -1234 {
		t.Fatal(preview, err)
	}
	page, err := client.Connections(ctx, "personal", "", 100)
	if err != nil || len(page.Data) != 0 {
		t.Fatal("preview wrote", page, err)
	}
	if _, err = client.Import(ctx, strings.NewReader(csv), opts); err != nil {
		t.Fatal(err)
	}
	txs, err := client.SearchTransactions(ctx, ranjit.TransactionFilter{CustomerID: "personal", Query: "mercado", From: "2026-11-07", Limit: 20})
	if err != nil || len(txs.Data) != 1 {
		t.Fatal(txs, err)
	}
}
