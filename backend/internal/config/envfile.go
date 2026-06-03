package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func LoadEnvFile() error {
	for _, path := range envFileCandidates() {
		if err := loadEnvFile(path); err != nil {
			return err
		}
	}
	return nil
}

func envFileCandidates() []string {
	if path := os.Getenv("LINEA_ENV_FILE"); path != "" {
		return []string{path}
	}

	paths := []string{".env", "../.env"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "linea", "linea.env"))
	}
	return paths
}

func loadEnvFile(path string) error {
	if path == "" {
		return nil
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func DefaultEnvFilePath() string {
	if path := os.Getenv("LINEA_ENV_FILE"); path != "" {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "linea", "linea.env")
	}
	return ""
}

func DefaultSettingsFilePath() string {
	if path := os.Getenv("LINEA_SETTINGS_FILE"); path != "" {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "linea", "settings.json")
	}
	return ""
}
