package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWritePIDAndReadPID(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	if err := WritePID(42); err != nil {
		t.Fatalf("WritePID() error = %v", err)
	}
	pid, err := ReadPID()
	if err != nil {
		t.Fatalf("ReadPID() error = %v", err)
	}
	if pid != 42 {
		t.Fatalf("ReadPID() = %d, want 42", pid)
	}
}

func TestWritePIDOverwritesExistingFile(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	if err := WritePID(1); err != nil {
		t.Fatalf("WritePID(1) error = %v", err)
	}
	if err := WritePID(2); err != nil {
		t.Fatalf("WritePID(2) error = %v", err)
	}
	pid, _ := ReadPID()
	if pid != 2 {
		t.Fatalf("ReadPID() = %d, want 2", pid)
	}
}

func TestReadPIDReturnsErrorForMissingFile(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	if _, err := ReadPID(); err == nil {
		t.Fatal("ReadPID() error = nil, want error")
	}
}

func TestReadPIDReturnsErrorForInvalidContent(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	p, _ := pidPath()
	os.WriteFile(p, []byte("not-a-number\n"), 0o644)

	if _, err := ReadPID(); err == nil {
		t.Fatal("ReadPID() error = nil, want error")
	}
}

func TestRemovePID(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	WritePID(42)
	if err := RemovePID(); err != nil {
		t.Fatalf("RemovePID() error = %v", err)
	}
	if _, err := ReadPID(); err == nil {
		t.Fatal("ReadPID() after RemovePID should fail")
	}
}

func TestRemovePIDIsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	if err := RemovePID(); err != nil {
		t.Fatalf("RemovePID() error = %v", err)
	}
}

func TestIsRunningReturnsTrueForCurrentProcess(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	WritePID(os.Getpid())
	if !IsRunning() {
		t.Fatal("IsRunning() = false, want true for current process")
	}
}

func TestIsRunningReturnsFalseForMissingPIDFile(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	if IsRunning() {
		t.Fatal("IsRunning() = true, want false when no PID file")
	}
}

func TestCurrentStatusPopulatesFields(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	WritePID(os.Getpid())
	s, err := CurrentStatus()
	if err != nil {
		t.Fatalf("CurrentStatus() error = %v", err)
	}
	if !s.Running {
		t.Fatal("CurrentStatus().Running = false, want true")
	}
	if s.PID == 0 {
		t.Fatal("CurrentStatus().PID = 0")
	}
	if s.DataDir == "" {
		t.Fatal("CurrentStatus().DataDir is empty")
	}
	if s.LogFile == "" {
		t.Fatal("CurrentStatus().LogFile is empty")
	}
	if s.PlistDir == "" {
		t.Fatal("CurrentStatus().PlistDir is empty")
	}
}

func TestPrintStatusProducesValidJSON(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("HOME", dataDir)
	t.Setenv("CACHE_DIR", dataDir)

	var buf captureWriter
	if err := PrintStatus(&buf); err != nil {
		t.Fatalf("PrintStatus() error = %v", err)
	}
	if len(buf.data) == 0 {
		t.Fatal("PrintStatus() wrote nothing")
	}
}

func TestWaitForHealthReturnsTrueOn200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if !WaitForHealth(server.URL, time.Second) {
		t.Fatal("WaitForHealth() = false, want true")
	}
}

func TestWaitForHealthReturnsFalseOnNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if WaitForHealth(server.URL, 0) {
		t.Fatal("WaitForHealth() = true, want false")
	}
}

func TestDataDirCreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CACHE_DIR", tmp)

	dir, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	if dir == "" {
		t.Fatal("DataDir() returned empty")
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("DataDir() did not create directory")
	}
}

func TestLogDirCreatesDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir, err := LogDir()
	if err != nil {
		t.Fatalf("LogDir() error = %v", err)
	}
	if dir == "" {
		t.Fatal("LogDir() returned empty")
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("LogDir() did not create directory")
	}
}

func TestPlistDirUsesHomeLibrary(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir, err := PlistDir()
	if err != nil {
		t.Fatalf("PlistDir() error = %v", err)
	}
	if dir != filepath.Join(tmp, "Library", "LaunchAgents") {
		t.Fatalf("PlistDir() = %q", dir)
	}
}

func TestReadPIDRespectsDataDir(t *testing.T) {
	dataDir, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dataDir, "daemon.pid")
	os.WriteFile(pidFile, []byte("99\n"), 0o644)

	pid, err := ReadPID()
	if err != nil {
		t.Fatalf("ReadPID() error = %v", err)
	}
	if pid != 99 {
		t.Fatalf("ReadPID() = %d, want 99", pid)
	}
	os.Remove(pidFile)
}

type captureWriter struct {
	data []byte
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.data = append(w.data, p...)
	return len(p), nil
}
