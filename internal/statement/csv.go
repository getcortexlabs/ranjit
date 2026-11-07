package statement

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/getcortexlabs/ranjit/internal/provider"
)

func parseCSV(data []byte, opts Options) (Statement, error) {
	var s Statement
	if opts.Account == "" || len(opts.Account) > 64 {
		return s, invalid("CSV needs an account name: ranjit import --account pessoal extrato.csv")
	}
	if !utf8.Valid(data) {
		return s, invalid("save the CSV using UTF-8 encoding")
	}
	decimal := opts.Decimal
	if decimal == "" {
		decimal = "."
	}
	if decimal != "." && decimal != "," {
		return s, invalid("decimal must be . or ,")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	first, _, _ := bytes.Cut(data, []byte{'\n'})
	r := csv.NewReader(bytes.NewReader(data))
	if bytes.Count(first, []byte{';'}) > bytes.Count(first, []byte{','}) {
		r.Comma = ';'
	}
	if bytes.Count(first, []byte{'\t'}) > bytes.Count(first, []byte{','}) && r.Comma == ',' {
		r.Comma = '\t'
	}
	header, err := r.Read()
	if err != nil {
		return s, invalid("cannot read CSV header")
	}
	columns := map[string]int{}
	for i, value := range header {
		key := strings.ToLower(strings.TrimSpace(value))
		switch key {
		case "data", "date", "posted_on":
			key = "date"
		case "descrição", "descricao", "description", "histórico", "historico", "title":
			key = "description"
		case "amount", "valor":
			key = "amount"
		case "id", "fitid", "transaction_id":
			key = "id"
		default:
			continue
		}
		if _, exists := columns[key]; exists {
			return s, invalid("duplicate CSV column")
		}
		columns[key] = i
	}
	for _, key := range []string{"date", "description", "amount"} {
		if _, ok := columns[key]; !ok {
			return s, invalid("CSV header needs date, description, amount (or data, descrição, valor); id is optional")
		}
	}
	_, s.StableIDs = columns["id"]
	accountID := "csv:" + opts.Account
	s.Snapshot.Accounts = []provider.Account{{ExternalID: accountID, Name: opts.Account, Type: "checking", BalanceUnknown: true}}
	s.Fingerprint = fingerprint(s.Snapshot.Accounts)
	hash := digest(data)
	for row := 2; ; row++ {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return s, invalid(fmt.Sprintf("invalid CSV row %d", row))
		}
		if len(s.Snapshot.Transactions) >= MaxTransactions {
			return s, invalid("maximum 10000 transactions per file")
		}
		value := func(key string) string { return strings.TrimSpace(record[columns[key]]) }
		posted, err := date(value("date"), opts.DateFormat)
		if err != nil {
			return s, invalid(fmt.Sprintf("invalid date at CSV row %d", row))
		}
		rawAmount := value("amount")
		if decimal == "," {
			if strings.Contains(rawAmount, ".") {
				return s, invalid(fmt.Sprintf("remove grouping separators at CSV row %d", row))
			}
			rawAmount = strings.ReplaceAll(rawAmount, ",", ".")
		}
		encoded, _ := json.Marshal(rawAmount)
		amount, err := provider.ParseBRL(encoded)
		if err != nil {
			return s, invalid(fmt.Sprintf("invalid amount at CSV row %d; use --decimal , for decimal commas, without grouping separators", row))
		}
		id := fmt.Sprintf("file:%s:%d", hash, row)
		if s.StableIDs {
			id = "id:" + value("id")
			if value("id") == "" {
				return s, invalid(fmt.Sprintf("empty transaction id at CSV row %d", row))
			}
		}
		s.Snapshot.Transactions = append(s.Snapshot.Transactions, provider.Transaction{ExternalID: id, AccountExternalID: accountID, Description: value("description"), Amount: amount, PostedOn: posted, Status: "booked"})
	}
	return s, nil
}
