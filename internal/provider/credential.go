package provider

import (
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ReadPEM resolves certificate/key files or secret environment references. PEM
// values use actual newlines, as provided by the hosting platform's secret store.
func ReadPEM(source string) ([]byte, error) { return readCredential(source, 1<<20) }

func readCredential(source string, limit int) ([]byte, error) {
	if name, ok := strings.CutPrefix(source, "env:"); ok {
		if !environmentName.MatchString(name) {
			return nil, errors.New("invalid credential environment reference")
		}
		value, exists := os.LookupEnv(name)
		if !exists || len(value) == 0 || len(value) > limit {
			return nil, errors.New("credential environment value is missing or too large")
		}
		return []byte(value), nil
	}
	f, err := os.Open(source)
	if err != nil {
		return nil, errors.New("cannot open credential file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(data) == 0 || len(data) > limit {
		return nil, errors.New("credential file is empty, unreadable or too large")
	}
	return data, nil
}
