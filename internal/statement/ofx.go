package statement

import (
	"bytes"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/getcortexlabs/ranjit/internal/provider"
)

type node struct {
	name, text string
	children   []*node
}

func (n *node) all(name string) []*node {
	var result []*node
	for _, c := range n.children {
		if c.name == name {
			result = append(result, c)
		}
		result = append(result, c.all(name)...)
	}
	return result
}
func (n *node) value(name string) (string, error) {
	items := n.all(name)
	if len(items) != 1 || len(items[0].children) != 0 {
		return "", invalid("missing or duplicate OFX field: " + name)
	}
	return strings.TrimSpace(items[0].text), nil
}
func (n *node) optional(name string) (string, error) {
	if len(n.all(name)) == 0 {
		return "", nil
	}
	return n.value(name)
}

var ofxDate = regexp.MustCompile(`^[0-9]{8}([0-9]{6}(\.[0-9]+)?(\[[+-]?[0-9]+(\.[0-9]+)?(:[A-Za-z]+)?\])?)?$`)

func parseOFX(data []byte) (Statement, error) {
	var s Statement
	root, err := ofxTree(data)
	if err != nil {
		return s, err
	}
	if len(root.all("CORRECTFITID")) != 0 || len(root.all("CORRECTACTION")) != 0 {
		return s, invalid("OFX correction records are not supported; export a consolidated statement")
	}
	statements := append(root.all("STMTRS"), root.all("CCSTMTRS")...)
	if len(statements) == 0 {
		return s, invalid("OFX must contain a bank or credit card statement")
	}
	if len(statements) > 20 {
		return s, invalid("maximum 20 accounts per OFX file")
	}
	for _, st := range statements {
		currency, err := st.value("CURDEF")
		if err != nil || currency != "BRL" {
			return s, invalid("only BRL statements are supported")
		}
		accounts := append(st.all("BANKACCTFROM"), st.all("CCACCTFROM")...)
		if len(accounts) != 1 {
			return s, invalid("OFX statement needs one account")
		}
		a := accounts[0]
		id, err := a.value("ACCTID")
		if err != nil || id == "" {
			return s, invalid("OFX account id is required")
		}
		bank, err := a.optional("BANKID")
		if err != nil {
			return s, err
		}
		branch, err := a.optional("BRANCHID")
		if err != nil {
			return s, err
		}
		kind, err := a.optional("ACCTTYPE")
		if err != nil {
			return s, err
		}
		external := "ofx:" + digest([]byte(bank+"\x00"+branch+"\x00"+id+"\x00"+kind+"\x00"+a.name))
		typ := "checking"
		if kind == "SAVINGS" {
			typ = "savings"
		}
		if a.name == "CCACCTFROM" {
			typ = "credit_card"
		}
		account := provider.Account{ExternalID: external, Name: "Conta importada", Type: typ, BalanceUnknown: true}
		balances := st.all("LEDGERBAL")
		if len(balances) > 1 {
			return s, invalid("multiple ledger balances in one statement")
		}
		if len(balances) == 1 {
			amount, err := balances[0].value("BALAMT")
			if err != nil {
				return s, err
			}
			encoded, _ := json.Marshal(amount)
			minor, err := provider.ParseBRL(encoded)
			if err != nil {
				return s, invalid("invalid OFX balance")
			}
			asof, err := balances[0].value("DTASOF")
			if err != nil {
				return s, err
			}
			day, err := parseOFXDate(asof)
			if err != nil {
				return s, err
			}
			account.Balance = minor
			account.BalanceUnknown = false
			account.BalanceAt, _ = time.Parse(time.DateOnly, day)
		}
		s.Snapshot.Accounts = append(s.Snapshot.Accounts, account)
		for _, trn := range st.all("STMTTRN") {
			id, err := trn.value("FITID")
			if err != nil || id == "" {
				return s, invalid("each OFX transaction needs a FITID")
			}
			posted, err := trn.value("DTPOSTED")
			if err != nil {
				return s, err
			}
			posted, err = parseOFXDate(posted)
			if err != nil {
				return s, err
			}
			amount, err := trn.value("TRNAMT")
			if err != nil {
				return s, err
			}
			encoded, _ := json.Marshal(amount)
			minor, err := provider.ParseBRL(encoded)
			if err != nil {
				return s, invalid("invalid OFX transaction amount")
			}
			name, err := trn.optional("NAME")
			if err != nil {
				return s, err
			}
			memo, err := trn.optional("MEMO")
			if err != nil {
				return s, err
			}
			description := strings.TrimSpace(name + " " + memo)
			if description == "" {
				description = "Lançamento importado"
			}
			if len(trn.all("CURRENCY")) != 0 || len(trn.all("ORIGCURRENCY")) != 0 {
				return s, invalid("foreign currency transactions are not supported")
			}
			s.Snapshot.Transactions = append(s.Snapshot.Transactions, provider.Transaction{ExternalID: id, AccountExternalID: external, Description: description, Amount: minor, PostedOn: posted, Status: "booked"})
		}
	}
	s.Fingerprint = fingerprint(s.Snapshot.Accounts)
	s.StableIDs = true
	return s, nil
}

func parseOFXDate(value string) (string, error) {
	if !ofxDate.MatchString(value) {
		return "", invalid("invalid OFX date")
	}
	t, err := time.Parse("20060102", value[:8])
	if err != nil {
		return "", invalid("invalid OFX date")
	}
	return t.Format(time.DateOnly), nil
}

// OFX 1.x permits omitted leaf closing tags. Aggregates must still close in
// order. No DTD, entity declarations, attributes or external resources are read.
func ofxTree(data []byte) (*node, error) {
	start := bytes.Index(bytes.ToUpper(data), []byte("<OFX>"))
	if start < 0 {
		return nil, invalid("missing OFX root")
	}
	header := strings.ToUpper(string(data[:start]))
	if bytes.Contains(bytes.ToUpper(data), []byte("<!DOCTYPE")) || bytes.Contains(bytes.ToUpper(data), []byte("<!ENTITY")) {
		return nil, invalid("OFX declarations are not supported")
	}
	if !utf8.Valid(data) {
		if !strings.Contains(header, "1252") && !strings.Contains(header, "8859-1") {
			return nil, invalid("OFX encoding must be UTF-8, Windows-1252 or ISO-8859-1")
		}
		var b strings.Builder
		const cp1252 = "€\u0081‚ƒ„…†‡ˆ‰Š‹Œ\u008dŽ\u008f\u0090‘’“”•–—˜™š›œ\u009džŸ"
		chars := []rune(cp1252)
		for _, c := range data[start:] {
			r := rune(c)
			if strings.Contains(header, "1252") && c >= 0x80 && c <= 0x9f {
				r = chars[c-0x80]
			}
			b.WriteRune(r)
		}
		data = []byte(b.String())
		start = 0
	}
	text := string(data[start:])
	root := &node{name: "document"}
	stack := []*node{root}
	leaves := map[string]bool{}
	for _, name := range strings.Fields("CODE SEVERITY MESSAGE DTSERVER LANGUAGE DTPROFUP DTACCTUP FI ORG FID INTU.BID TRNUID STATUS CURDEF BANKID BRANCHID ACCTID ACCTTYPE ACCTKEY DTSTART DTEND TRNTYPE DTPOSTED DTUSER DTAVAIL TRNAMT FITID CORRECTFITID CORRECTACTION SRVRTID CHECKNUM REFNUM SIC PAYEEID NAME MEMO BALAMT DTASOF AVAILCASH") {
		leaves[name] = true
	}
	// FI and STATUS are aggregates, even when their children happen to be empty.
	delete(leaves, "FI")
	delete(leaves, "STATUS")
	count := 0
	for len(text) > 0 {
		if text[0] != '<' {
			idx := strings.IndexByte(text, '<')
			if idx < 0 {
				idx = len(text)
			}
			value := strings.TrimSpace(text[:idx])
			if value != "" {
				if len(stack) < 2 {
					return nil, invalid("content outside OFX root")
				}
				stack[len(stack)-1].text += html.UnescapeString(value)
			}
			text = text[idx:]
			continue
		}
		end := strings.IndexByte(text, '>')
		if end < 0 {
			return nil, invalid("truncated OFX tag")
		}
		tag := strings.ToUpper(strings.TrimSpace(text[1:end]))
		text = text[end+1:]
		closing := strings.HasPrefix(tag, "/")
		name := strings.TrimPrefix(tag, "/")
		if name == "" || strings.ContainsAny(name, " \t\r\n!?=\"'") {
			return nil, invalid("unsupported OFX tag")
		}
		if len(stack) > 1 && leaves[stack[len(stack)-1].name] && (!closing || stack[len(stack)-1].name != name) {
			stack = stack[:len(stack)-1]
		}
		if closing {
			if len(stack) < 2 || stack[len(stack)-1].name != name {
				return nil, invalid("mismatched OFX closing tag")
			}
			stack = stack[:len(stack)-1]
		} else {
			if len(stack) == 1 && (name != "OFX" || len(root.children) != 0) {
				return nil, invalid("unexpected OFX root")
			}
			if len(stack) > 32 || count > 100000 {
				return nil, invalid("OFX structure limit exceeded")
			}
			count++
			n := &node{name: name}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, n)
			stack = append(stack, n)
		}
	}
	if len(stack) != 1 {
		return nil, invalid("truncated OFX document")
	}
	return root, nil
}
