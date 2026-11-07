package statement

import (
	"strings"
	"testing"
)

const ofxFixture = `OFXHEADER:100
DATA:OFXSGML
VERSION:102
ENCODING:USASCII
CHARSET:1252

<OFX><BANKMSGSRSV1><STMTTRNRS><TRNUID>0
<STATUS><CODE>0<SEVERITY>INFO</STATUS>
<STMTRS><CURDEF>BRL
<BANKACCTFROM><BANKID>999<BRANCHID>0001<ACCTID>1234<ACCTTYPE>CHECKING</BANKACCTFROM>
<BANKTRANLIST><DTSTART>20261101000000[-3:BRT]<DTEND>20261107000000[-3:BRT]
<STMTTRN><TRNTYPE>DEBIT<DTPOSTED>20261106120000[-3:BRT]<TRNAMT>-12.34<FITID>a<NAME>Café<MEMO>Compra &amp; almoço</STMTTRN>
<STMTTRN><TRNTYPE>CREDIT<DTPOSTED>20261107<TRNAMT>90071992547409.91<FITID>b<NAME>Entrada</STMTTRN>
</BANKTRANLIST><LEDGERBAL><BALAMT>100.01<DTASOF>20261107</LEDGERBAL>
</STMTRS></STMTTRNRS></BANKMSGSRSV1></OFX>`

func TestOFXSGMLAndExactMoney(t *testing.T) {
	s, err := Parse(strings.NewReader(ofxFixture), Options{Format: "ofx"})
	if err != nil {
		t.Fatal(err)
	}
	if !s.StableIDs || len(s.Snapshot.Transactions) != 2 || s.Snapshot.Accounts[0].Balance.AmountMinor != 10001 || s.Snapshot.Transactions[0].Amount.AmountMinor != -1234 || s.Snapshot.Transactions[1].Amount.AmountMinor != 9007199254740991 || s.From != "2026-11-06" || s.Through != "2026-11-07" {
		t.Fatalf("unexpected statement: %+v", s)
	}
	if !strings.Contains(s.Snapshot.Transactions[0].Description, "& almoço") {
		t.Fatal("entity decoding")
	}
	encoded := strings.ReplaceAll(ofxFixture, "Café", "Caf\xe9")
	s, err = Parse(strings.NewReader(encoded), Options{Format: "ofx"})
	if err != nil || !strings.Contains(s.Snapshot.Transactions[0].Description, "Café") {
		t.Fatalf("Windows-1252: %v", err)
	}
}

func TestOFXXMLCreditCard(t *testing.T) {
	xml := `<?xml version="1.0"?><OFX><CREDITCARDMSGSRSV1><CCSTMTTRNRS><CCSTMTRS><CURDEF>BRL</CURDEF><CCACCTFROM><ACCTID>123</ACCTID></CCACCTFROM><BANKTRANLIST><STMTTRN><DTPOSTED>20261107</DTPOSTED><TRNAMT>-1.00</TRNAMT><FITID>x</FITID><NAME>Compra</NAME></STMTTRN></BANKTRANLIST></CCSTMTRS></CCSTMTTRNRS></CREDITCARDMSGSRSV1></OFX>`
	s, err := Parse(strings.NewReader(xml), Options{Format: "ofx"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Snapshot.Accounts[0].Type != "credit_card" || !s.Snapshot.Accounts[0].BalanceUnknown {
		t.Fatal("unknown balance or account type lost")
	}
}

func TestOFXRejectsUnsafeOrPartialStatements(t *testing.T) {
	for name, data := range map[string]string{
		"truncated":      strings.TrimSuffix(ofxFixture, "</OFX>"),
		"duplicate ID":   strings.Replace(ofxFixture, "<FITID>b", "<FITID>a", 1),
		"missing amount": strings.Replace(ofxFixture, "<TRNAMT>-12.34", "", 1),
		"correction":     strings.Replace(ofxFixture, "<FITID>a", "<FITID>a<CORRECTFITID>old", 1),
		"currency":       strings.Replace(ofxFixture, "<CURDEF>BRL", "<CURDEF>USD", 1),
		"entities":       "<!DOCTYPE OFX [<!ENTITY x SYSTEM 'file:///secret'>]>" + ofxFixture,
		"overflow":       strings.Replace(ofxFixture, "90071992547409.91", "90071992547409.92", 1),
		"date":           strings.Replace(ofxFixture, "20261106120000", "20260230120000", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(data), Options{Format: "ofx"}); err == nil {
				t.Fatal("accepted invalid OFX")
			}
		})
	}
}

func TestCSVPreservesIdenticalRowsAndSupportsDecimalComma(t *testing.T) {
	data := "data;descrição;valor\n07/11/2026;Café;-12,34\n07/11/2026;Café;-12,34\n"
	s, err := Parse(strings.NewReader(data), Options{Format: "csv", Account: "pessoal", Decimal: ","})
	if err != nil {
		t.Fatal(err)
	}
	if s.StableIDs || !s.Snapshot.Accounts[0].BalanceUnknown || len(s.Snapshot.Transactions) != 2 || s.Snapshot.Transactions[0].ExternalID == s.Snapshot.Transactions[1].ExternalID || s.Snapshot.Transactions[1].Amount.AmountMinor != -1234 {
		t.Fatalf("bad normalization: %+v", s)
	}
	for _, bad := range []string{"2026-11-07,Café,1.001", "2026-11-07,Café,1e2", "2026-11-31,Café,1", "2026-11-07,Café,", "2026-11-07,Café,NaN"} {
		if _, err := Parse(strings.NewReader("date,description,amount\n"+bad), Options{Format: "csv", Account: "pessoal"}); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func FuzzStatementNeverPanics(f *testing.F) {
	f.Add(ofxFixture, "ofx")
	f.Add("date,description,amount\n2026-11-07,test,1.20", "csv")
	f.Fuzz(func(t *testing.T, input, format string) {
		if len(input) > MaxBytes {
			t.Skip()
		}
		_, _ = Parse(strings.NewReader(input), Options{Format: format, Account: "test"})
	})
}
