package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestLaunchProfilesMigratePersistAndDeleteIndependently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(earlySchema + `INSERT INTO settings VALUES('agents','retained');`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := db.SaveLaunchProfile(&LaunchProfile{Name: "Work", Agent: "codex", EnvJSON: `{"CODEX_HOME":"/private/home"}`})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.LaunchProfile(p.ID)
	if err != nil || got.EnvJSON != p.EnvJSON || db.Setting("agents") != "retained" {
		t.Fatal("migration or reopen lost configuration")
	}
	p.Name = "Personal"
	if _, err := db.SaveLaunchProfile(p); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteLaunchProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveLaunchProfile(p); !errors.Is(err, ErrNotFound) {
		t.Fatal("updating a deleted profile recreated it", err)
	}
	if _, err := db.LaunchProfile(p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	replacement, err := db.SaveLaunchProfile(&LaunchProfile{Name: "Replacement", Agent: "codex", EnvJSON: `{}`})
	if err != nil || replacement.ID == p.ID {
		t.Fatal("deleted profile ID was reused; a stale selection could launch different settings", err)
	}
	if db.Setting("agents") != "retained" {
		t.Fatal("profile deletion removed other settings")
	}
}
