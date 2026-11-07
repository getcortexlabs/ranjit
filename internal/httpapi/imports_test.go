package httpapi_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/getcortexlabs/ranjit"
)

func TestImportSDKAcceptsLargeFilesAndKeepsUnknownBalance(t *testing.T) {
	client, _, _, logs := setup(t)
	var body strings.Builder
	body.WriteString("id,date,description,amount\n")
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&body, "%d,2026-11-07,private-description-%d,-12.34\n", i, i)
	}
	result, err := client.Import(t.Context(), strings.NewReader(body.String()), ranjit.ImportOptions{CustomerID: "personal", Account: "pessoal", Format: "csv"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Transactions != 600 || result.Duplicate {
		t.Fatalf("import: %+v", result)
	}
	accounts, err := client.Accounts(t.Context(), result.ConnectionID, "", 100)
	if err != nil || len(accounts.Data) != 1 || accounts.Data[0].Balance != nil {
		t.Fatalf("unknown balance: %+v %v", accounts, err)
	}
	status, err := client.Status(t.Context(), result.ConnectionID)
	if err != nil || status.Transactions != 600 || status.LatestJob != nil || !status.Connection.ConsentExpiresAt.IsZero() {
		t.Fatalf("file status: %+v %v", status, err)
	}
	_, err = client.Sync(t.Context(), result.ConnectionID)
	var apiError *ranjit.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusConflict {
		t.Fatalf("scheduled file connection: %v", err)
	}
	if strings.Contains(logs.String(), "private-description") {
		t.Fatal("statement leaked into logs")
	}
	if err = client.Revoke(t.Context(), result.ConnectionID); err != nil {
		t.Fatal(err)
	}
	status, err = client.Status(t.Context(), result.ConnectionID)
	if err != nil || status.Transactions != 0 {
		t.Fatalf("data retained: %+v %v", status, err)
	}
}

func TestImportRequiresAuthenticationBeforeReadingFile(t *testing.T) {
	client, _, _, _ := setup(t)
	req, err := http.NewRequest(http.MethodPost, "http://localhost:8080/v1/imports?customer_id=personal&format=csv&account=pessoal", strings.NewReader("invalid"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}
