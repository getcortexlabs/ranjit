package main

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/getcortexlabs/ranjit/internal/config"
)

func TestFirstUseImportDemoAndProfiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RANJIT_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("RANJIT_API_KEY", "")
	t.Setenv("RANJIT_DATABASE", "")
	t.Setenv("RANJIT_ADDR", "")
	t.Setenv("RANJIT_BASE_URL", "")
	out := &bytes.Buffer{}
	c := cli{in: strings.NewReader(""), out: out, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	file := filepath.Join(dir, "extrato.csv")
	if err := os.WriteFile(file, []byte("date,description,amount\n2026-11-07,Compra,-12.34\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"import", "--account", "pessoal", file}, {"import", "--account", "pessoal", file}, {"doctor"}, {"accounts", "--json"}, {"transactions"}, {"demo"}, {"configure", "--bank", "sandbox", "--name", "segundo"}, {"sync", "segundo"}, {"status", "--json"}} {
		out.Reset()
		if err := c.command(t.Context(), args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 {
			t.Fatalf("no output: %v", args)
		}
	}
	p, err := config.ReadPersonal(os.Getenv("RANJIT_CONFIG"))
	if err != nil || len(p.Profiles) != 2 {
		t.Fatalf("profiles: %+v %v", p.Profiles, err)
	}
	info, err := os.Stat(os.Getenv("RANJIT_CONFIG"))
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("config permissions: %v %v", info, err)
	}
	if strings.Contains(out.String(), p.APIKey) {
		t.Fatal("secret leaked in status")
	}
}

func TestWizardDefaultsToFilesAndKeepsExistingConfig(t *testing.T) {
	t.Setenv("RANJIT_DATABASE", "")
	t.Setenv("RANJIT_ADDR", "")
	t.Setenv("RANJIT_BASE_URL", "")
	t.Setenv("RANJIT_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	out := &bytes.Buffer{}
	c := cli{in: strings.NewReader("\n"), out: out, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := c.command(t.Context(), []string{"configure"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(os.Getenv("RANJIT_CONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	c.in = strings.NewReader("nonsense\nprofile\nsandbox\nclient\nsecret\ncert\nkey\n")
	if err = c.command(t.Context(), []string{"configure", "--bank", "inter", "--name", "broken"}); err == nil {
		t.Fatal("saved missing credentials")
	}
	after, err := os.ReadFile(os.Getenv("RANJIT_CONFIG"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed configure changed configuration", err)
	}
}
