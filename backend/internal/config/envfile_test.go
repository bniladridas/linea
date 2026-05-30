package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileSetsMissingValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "linea.env")
	if err := os.WriteFile(path, []byte(`
# comment
LINEA_TEST_ONE=one
LINEA_TEST_TWO="two words"
LINEA_TEST_EXISTING=file
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("LINEA_ENV_FILE", path)
	t.Setenv("LINEA_TEST_EXISTING", "env")

	if err := LoadEnvFile(); err != nil {
		t.Fatalf("LoadEnvFile() error = %v", err)
	}
	if got := os.Getenv("LINEA_TEST_ONE"); got != "one" {
		t.Fatalf("LINEA_TEST_ONE = %q, want one", got)
	}
	if got := os.Getenv("LINEA_TEST_TWO"); got != "two words" {
		t.Fatalf("LINEA_TEST_TWO = %q, want two words", got)
	}
	if got := os.Getenv("LINEA_TEST_EXISTING"); got != "env" {
		t.Fatalf("LINEA_TEST_EXISTING = %q, want env", got)
	}
}

func TestLoadEnvFileIgnoresMissingDefaultFile(t *testing.T) {
	t.Setenv("LINEA_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))

	if err := LoadEnvFile(); err != nil {
		t.Fatalf("LoadEnvFile() error = %v", err)
	}
}
