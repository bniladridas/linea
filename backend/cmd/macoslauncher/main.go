package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	appRoot, err := appRoot()
	if err != nil {
		exit(err)
	}
	lineaBin := filepath.Join(appRoot, "Contents", "Resources", "linea")
	logFile, err := openLog()
	if err != nil {
		exit(err)
	}
	defer logFile.Close()

	apiAddr := os.Getenv("API_ADDR")
	if apiAddr == "" {
		apiAddr = "127.0.0.1:8080"
	}
	lineaURL := urlForAddr(apiAddr)

	cmd := exec.Command(lineaBin)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "API_ADDR="+apiAddr)
	if err := cmd.Start(); err != nil {
		exit(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stopProcess(cmd.Process)
	}()

	if err := waitForHealth(ctx, lineaURL); err != nil {
		stopProcess(cmd.Process)
		_ = exec.Command("open", logFile.Name()).Run()
		exit(err)
	}
	_ = exec.Command("open", lineaURL+"/").Run()

	err = cmd.Wait()
	if ctx.Err() != nil || errors.Is(err, os.ErrProcessDone) {
		return
	}
	if err != nil {
		exit(err)
	}
}

func appRoot() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(filepath.Dir(exe), "..", "..")), nil
}

func openLog() (*os.File, error) {
	logDir := filepath.Join(os.Getenv("HOME"), "Library", "Logs", "Linea")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(logDir, "linea.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
}

func urlForAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	if strings.Contains(addr, ":") {
		return "http://" + addr
	}
	return "http://127.0.0.1:" + addr
}

func waitForHealth(ctx context.Context, baseURL string) error {
	client := &http.Client{Timeout: time.Second}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(20 * time.Second)

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
		if err != nil {
			return err
		}
		res, err := client.Do(req)
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("linea did not start at %s", baseURL)
		case <-ticker.C:
		}
	}
}

func stopProcess(process *os.Process) {
	if process == nil {
		return
	}
	_ = process.Signal(syscall.SIGTERM)
	time.Sleep(300 * time.Millisecond)
	_ = process.Kill()
}

func exit(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
