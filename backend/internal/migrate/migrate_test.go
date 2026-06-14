package migrate

import (
	"strings"
	"testing"
)

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

func TestNamesReturnsSorted(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("migrations not sorted: %s >= %s", names[i-1], names[i])
		}
	}
}

func TestNamesAllHaveSQLExtension(t *testing.T) {
	names, err := Names()
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}
	for _, name := range names {
		if !strings.HasSuffix(name, ".sql") {
			t.Fatalf("migration %q does not end with .sql", name)
		}
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	if _, err := Run(t.Context(), ""); err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}

func TestRunRequiresNonBlankDatabaseURL(t *testing.T) {
	if _, err := Run(t.Context(), "   "); err == nil {
		t.Fatal("Run() with spaces error = nil, want error")
	}
}
