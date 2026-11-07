// Package statement reads local bank exports. Parsing is bounded and atomic;
// malformed rows never become zero-value transactions.
package statement

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/getcortexlabs/ranjit"
	"github.com/getcortexlabs/ranjit/internal/provider"
)

const MaxBytes = 8 << 20
const MaxTransactions = 10000

type Statement struct {
	Snapshot    provider.Snapshot
	Fingerprint string
	FileHash    string
	From        string
	Through     string
	StableIDs   bool
}

type Options struct {
	Columns   map[string]string
	Delimiter string
	Format    string
	Account   string
	// Decimal is "." or ","; grouping separators are never guessed.
	Decimal    string
	DateFormat string
}

func Parse(r io.Reader, opts Options) (Statement, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Statement{}, fmt.Errorf("%w: cannot read statement", ranjit.ErrInvalid)
	}
	if len(data) == 0 || len(data) > MaxBytes {
		return Statement{}, fmt.Errorf("%w: statement must contain 1 byte to 8 MiB", ranjit.ErrInvalid)
	}
	var s Statement
	switch strings.ToLower(opts.Format) {
	case "ofx", "qfx":
		s, err = parseOFX(data)
	case "csv":
		s, err = parseCSV(data, opts)
	default:
		err = fmt.Errorf("%w: choose ofx or csv", ranjit.ErrInvalid)
	}
	if err != nil {
		return s, err
	}
	s.FileHash = digest(data)
	if len(s.Snapshot.Transactions) > MaxTransactions {
		return s, fmt.Errorf("%w: maximum 10000 transactions per file", ranjit.ErrInvalid)
	}
	if err = s.Snapshot.Validate(); err != nil {
		return s, err
	}
	if s.From == "" {
		s.From = "9999-12-31"
		s.Through = "0001-01-01"
	}
	for _, t := range s.Snapshot.Transactions {
		s.From = min(s.From, t.PostedOn)
		s.Through = max(s.Through, t.PostedOn)
	}
	if s.From > s.Through {
		s.From, s.Through = "", ""
	}
	return s, nil
}

func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

func invalid(message string) error { return fmt.Errorf("%w: %s", ranjit.ErrInvalid, message) }

func date(raw, format string) (string, error) {
	layouts := []string{time.DateOnly, "02/01/2006"}
	if format != "" {
		layouts = []string{format}
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.Format(time.DateOnly), nil
		}
	}
	return "", invalid("invalid date; use YYYY-MM-DD, DD/MM/YYYY or an explicit date format")
}

func fingerprint(accounts []provider.Account) string {
	ids := make([]string, len(accounts))
	for i, a := range accounts {
		ids[i] = a.ExternalID
	}
	sort.Strings(ids)
	return digest([]byte(strings.Join(ids, "\x00")))
}
