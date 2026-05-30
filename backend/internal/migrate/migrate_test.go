package migrate

import "testing"

func TestNames(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}
	if len(names) == 0 {
		t.Fatal("Names() returned no migrations")
	}
	if names[0] != "001_init.sql" {
		t.Fatalf("first migration = %q, want 001_init.sql", names[0])
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	if _, err := Run(t.Context(), ""); err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}
