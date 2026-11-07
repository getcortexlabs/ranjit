package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDatabasePathWithSpacesUnicodeAndURIEscapes(t *testing.T) {
	name := "conta mês #%.db"
	if runtime.GOOS != "windows" {
		name = "conta mês ?#%.db"
	}
	path := filepath.Join(t.TempDir(), name)
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() == 0 {
		t.Fatalf("database written to a different path: %v", err)
	}
}

func TestRestoreCannotBypassLockThroughSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires a developer-mode or elevated token on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "data.db")
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	copy := filepath.Join(dir, "copy.db")
	if err = db.Backup(t.Context(), copy); err != nil {
		t.Fatal(err)
	}
	db.Close()
	alias := filepath.Join(dir, "alias.db")
	if err = os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	throughAlias, err := Open(t.Context(), alias)
	if err != nil {
		t.Fatal(err)
	}
	defer throughAlias.Close()
	if _, err = Restore(t.Context(), copy, path, true); err == nil {
		t.Fatal("alias bypassed maintenance lock")
	}
}
