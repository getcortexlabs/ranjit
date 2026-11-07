package provider

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/getcortexlabs/ranjit"
)

// ParseBRL reads decimal JSON numbers or strings without rounding through float64.
func ParseBRL(raw json.RawMessage) (ranjit.Money, error) {
	invalid := fmt.Errorf("%w: invalid BRL amount", ranjit.ErrInvalid)
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return ranjit.Money{}, invalid
		}
	}
	negative := strings.HasPrefix(value, "-")
	digits := strings.TrimPrefix(value, "-")
	whole, cents, dot := strings.Cut(digits, ".")
	if whole == "" || len(whole) > 14 || len(cents) > 2 || dot && cents == "" {
		return ranjit.Money{}, invalid
	}
	for _, c := range whole + cents {
		if c < '0' || c > '9' {
			return ranjit.Money{}, invalid
		}
	}
	cents += strings.Repeat("0", 2-len(cents))
	minor, err := strconv.ParseInt(whole+cents, 10, 64)
	if err != nil {
		return ranjit.Money{}, invalid
	}
	if negative {
		minor = -minor
	}
	result := ranjit.Money{AmountMinor: minor, Currency: "BRL"}
	if err := result.Validate(); err != nil {
		return ranjit.Money{}, invalid
	}
	return result, nil
}
