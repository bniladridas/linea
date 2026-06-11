package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	pidFileName   = "daemon.pid"
	logFileName   = "linea-daemon.log"
	plistLabel    = "com.bniladridas.linea"
	plistFileName = "com.bniladridas.linea.plist"
)

func DataDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "linea")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func LogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "Library", "Logs", "Linea")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func pidPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pidFileName), nil
}

func logPath() (string, error) {
	dir, err := LogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, logFileName), nil
}

func ReadPID() (int, error) {
	p, err := pidPath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func WritePID(pid int) error {
	p, err := pidPath()
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

func RemovePID() error {
	p, err := pidPath()
	if err != nil {
		return err
	}
	os.Remove(p)
	return nil
}

func IsRunning() bool {
	pid, err := ReadPID()
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

type Status struct {
	Running  bool   `json:"running"`
	PID      int    `json:"pid,omitempty"`
	LogFile  string `json:"log_file,omitempty"`
	DataDir  string `json:"data_dir,omitempty"`
	PlistDir string `json:"plist_dir,omitempty"`
}

func CurrentStatus() (Status, error) {
	s := Status{}
	pid, err := ReadPID()
	if err == nil {
		s.PID = pid
		s.Running = IsRunning()
	}
	s.DataDir, _ = DataDir()
	s.LogFile, _ = logPath()
	s.PlistDir, _ = PlistDir()
	return s, nil
}

func PrintStatus(w io.Writer) error {
	s, err := CurrentStatus()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

func PlistDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

func plistPath() (string, error) {
	dir, err := PlistDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, plistFileName), nil
}

func Install() error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	log, err := logPath()
	if err != nil {
		return err
	}
	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, plistLabel, execPath, log, log)

	p, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(plistContent), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Load with launchctl
	cmd := exec.Command("launchctl", "load", p)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	slog.Info("LaunchAgent installed and loaded", "plist", p)
	return nil
}

func Uninstall() error {
	p, err := plistPath()
	if err != nil {
		return err
	}

	// Unload from launchctl first
	if _, err := os.Stat(p); err == nil {
		cmd := exec.Command("launchctl", "unload", p)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			slog.Warn("launchctl unload", "error", err)
		}
		os.Remove(p)
	}

	RemovePID()
	slog.Info("LaunchAgent uninstalled")
	return nil
}

func StartBackground(serverAddr string) error {
	if IsRunning() {
		pid, _ := ReadPID()
		return fmt.Errorf("daemon is already running (pid %d)", pid)
	}

	log, err := logPath()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer logFile.Close()

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	cmd := exec.Command(execPath)
	cmd.Env = append(os.Environ(), "API_ADDR="+serverAddr)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	if err := WritePID(cmd.Process.Pid); err != nil {
		return fmt.Errorf("write pid: %w", err)
	}

	slog.Info("daemon started", "pid", cmd.Process.Pid, "addr", serverAddr, "log", log)
	return nil
}

func WaitForHealth(url string, timeout time.Duration) bool {
	healthURL := strings.TrimRight(url, "/") + "/healthz"
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}
