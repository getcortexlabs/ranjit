package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestUpgradePreservesDataAndRestoresForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	body, err := migrations.ReadFile("migrations/001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(`
		CREATE TABLE schema_migrations (name TEXT PRIMARY KEY);
		INSERT INTO schema_migrations VALUES ('001_init.sql');
		INSERT INTO connections VALUES ('con-old','customer','sandbox-bank','sandbox','active',1,9999999999999,NULL,NULL,9999999999999);
		INSERT INTO accounts VALUES ('acc-old','con-old','external','Account','checking',100,'BRL',1);
		INSERT INTO transactions VALUES ('txn-old','acc-old','external','Test',100,'BRL','2026-11-01','booked');
		INSERT INTO jobs(id,connection_id,status,created_at,updated_at,available_at) VALUES ('job-old','con-old','queued',1,1,1);
	`); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"connections", "accounts", "transactions", "jobs"} {
		var count int
		if err = db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("migration lost %s: %d %v", table, count, err)
		}
	}
	c, err := db.Connection(t.Context(), "con-old", time.Now())
	if err != nil || c.AuthorizationMode != "sandbox_consent" {
		t.Fatalf("old connection = %+v %v", c, err)
	}
	var enabled int
	if err = db.db.QueryRow(`PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("foreign key enforcement = %d %v", enabled, err)
	}
	if err = db.Revoke(t.Context(), "con-old", time.Now()); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = db.db.QueryRow(`SELECT count(*) FROM transactions`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("cascade broken after rebuild: %d %v", remaining, err)
	}
}

func TestInterMigrationPreservesEFICredentialBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "efi.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err = old.Exec(`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_bank_credentials.sql"} {
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = old.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err = old.Exec(`INSERT INTO schema_migrations VALUES (?)`, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = old.Exec(`
		INSERT INTO connections VALUES ('con-efi','customer','efi','efi','active',1,9999999999999,NULL,NULL,0,'api_credentials','existing-binding');
		INSERT INTO accounts VALUES ('acc-efi','con-efi','own-account','Account','payment',100,'BRL',1);
		INSERT INTO transactions VALUES ('txn-efi','acc-efi','external','Test',100,'BRL','2026-11-01','booked');
		INSERT INTO jobs(id,connection_id,status,created_at,updated_at,available_at) VALUES ('job-efi','con-efi','queued',1,1,1);
	`); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, err := db.Connection(t.Context(), "con-efi", time.Now())
	if err != nil || c.CredentialID != "existing-binding" || c.AuthorizationMode != "api_credentials" {
		t.Fatalf("lost Efí binding: %+v %v", c, err)
	}
	for _, table := range []string{"accounts", "transactions", "jobs"} {
		var count int
		if err = db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("migration lost %s: %v", table, err)
		}
	}
}

func TestBankMigrationPreservesInterCredentialBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inter.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err = old.Exec(`CREATE TABLE schema_migrations (name TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_init.sql", "002_bank_credentials.sql", "003_inter.sql"} {
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = old.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
		if _, err = old.Exec(`INSERT INTO schema_migrations VALUES (?)`, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = old.Exec(`
		INSERT INTO connections VALUES ('con-inter','customer','inter','inter','active',1,9999999999999,NULL,NULL,0,'api_credentials','existing-binding');
		INSERT INTO accounts VALUES ('acc-inter','con-inter','own-account','Account','payment',100,'BRL',1);
		INSERT INTO transactions VALUES ('txn-inter','acc-inter','external','Test',100,'BRL','2026-11-01','booked');
		INSERT INTO jobs(id,connection_id,status,created_at,updated_at,available_at) VALUES ('job-inter','con-inter','queued',1,1,1);
	`); err != nil {
		t.Fatal(err)
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c, err := db.Connection(t.Context(), "con-inter", time.Now())
	if err != nil || c.CredentialID != "existing-binding" || c.AuthorizationMode != "api_credentials" {
		t.Fatalf("lost Inter binding: %+v %v", c, err)
	}
	for _, table := range []string{"accounts", "transactions", "jobs"} {
		var count int
		if err = db.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("migration lost %s: %v", table, err)
		}
	}
}
