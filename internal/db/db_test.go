package db

import (
	"path/filepath"
	"testing"
)

func TestOpenMigrates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	d, err = Open(p) // idempotent
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO users(sub) VALUES('x')`); err != nil {
		t.Fatal(err)
	}
}
