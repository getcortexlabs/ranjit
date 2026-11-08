package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getcortexlabs/ranjit/internal/config"
)

func TestRotatePersonalKeyAndEndGrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("RANJIT_CONFIG", path)
	t.Setenv("RANJIT_API_KEY", "")
	t.Setenv("RANJIT_DATABASE_URL", "")
	t.Setenv("RANJIT_PROFILES_JSON", "")
	// Personal mode bypasses environment loading, including profile overrides.
	p := config.NewPersonal()
	if err := config.SavePersonal(path, p); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	c := cli{out: &out}
	if err := c.command(context.Background(), []string{"key", "rotate", "--grace", "1h"}); err != nil {
		t.Fatal(err)
	}
	rotated, err := config.ReadPersonal(path)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.APIKey == p.APIKey || rotated.PreviousAPIKey != p.APIKey || !rotated.PreviousAPIKeyExpiresAt.After(time.Now()) || strings.TrimSpace(out.String()) != rotated.APIKey {
		t.Fatal("bad rotation")
	}
	if err = c.command(t.Context(), []string{"key", "rotate"}); err == nil {
		t.Fatal("in-flight grace replaced")
	}
	if err = c.command(t.Context(), []string{"key", "rotate", "--grace", "0"}); err != nil {
		t.Fatal(err)
	}
	rotated, err = config.ReadPersonal(path)
	if err != nil || rotated.PreviousAPIKey != "" || !rotated.PreviousAPIKeyExpiresAt.IsZero() {
		t.Fatal("old key retained", err)
	}
}
