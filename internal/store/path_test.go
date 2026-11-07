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
