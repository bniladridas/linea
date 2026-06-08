package agent

import (
	"path/filepath"
	"regexp"
	"strings"
)

var secretValuePattern = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|credential|authorization)(\s*[:=]\s*)([^\s"']+)`)

func isSecretPath(path string) bool {
	path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	base := filepath.Base(path)
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	switch base {
	case "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519", "known_hosts", "credentials", "credentials.json":
		return true
	}
	return strings.Contains(path, "/.ssh/") ||
		strings.Contains(path, "/.aws/") ||
		strings.Contains(path, "/.config/gcloud/")
}

func redactSecrets(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return secretValuePattern.ReplaceAllString(value, "$1$2[redacted]")
}
