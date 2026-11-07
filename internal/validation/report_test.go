package validation

import (
	"encoding/json"
	"github.com/getcortexlabs/ranjit/internal/provider/sandbox"
	"strings"
	"testing"
)

func TestSimulationNeverClaimsLiveValidation(t *testing.T) {
	report, err := Run(t.Context(), sandbox.Source{}, "dev", "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Result != "simulated" || report.Checks["manual_statement_review"] != "pending" || report.Checks["repeat_import"] != "passed" {
		t.Fatal(report)
	}
	b, _ := json.Marshal(report)
	for _, private := range []string{"checking-1", "482135", "Salário", "credential_id", "account_id"} {
		if strings.Contains(string(b), private) {
			t.Fatal("private data in report", string(b))
		}
	}
}
