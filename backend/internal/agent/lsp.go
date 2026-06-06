package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const lspTimeout = 10 * time.Second

var (
	errLSPUnavailable = errors.New("lsp unavailable")
	lspLocationRE     = regexp.MustCompile(`^(.+?):(\d+):(\d+)(?:[-:]\d+(?::\d+)?)?:?\s*(.*)$`)
)

func WithLSPCommand(command string) func(*Runtime) {
	return func(r *Runtime) {
		r.lspCommand = strings.TrimSpace(command)
	}
}

func (r *Runtime) listDiagnosticsWithLSP(ctx context.Context, root string) ([]Diagnostic, bool) {
	files, err := goFiles(ctx, root, maxDiagnosticFiles)
	if err != nil || len(files) == 0 {
		return nil, false
	}
	output, err := r.runLSP(ctx, root, append([]string{"check"}, files...)...)
	diagnostics := parseLSPDiagnostics(root, output)
	if len(diagnostics) > 0 {
		return diagnostics, true
	}
	return diagnostics, err == nil
}

func (r *Runtime) listSymbolsWithLSP(ctx context.Context, root string, query string) ([]WorkspaceSymbol, bool) {
	output, err := r.runLSP(ctx, root, "workspace_symbol", strings.TrimSpace(query))
	if err != nil {
		return nil, false
	}
	symbols := parseLSPSymbols(root, output, strings.ToLower(strings.TrimSpace(query)))
	if len(symbols) > maxSymbols {
		symbols = symbols[:maxSymbols]
	}
	return symbols, true
}

func (r *Runtime) runLSP(ctx context.Context, root string, args ...string) ([]byte, error) {
	command := strings.TrimSpace(r.lspCommand)
	if command == "" {
		return nil, errLSPUnavailable
	}
	if strings.EqualFold(command, "off") || strings.EqualFold(command, "none") {
		return nil, errLSPUnavailable
	}
	if _, err := exec.LookPath(command); err != nil && !filepath.IsAbs(command) {
		return nil, errLSPUnavailable
	}
	runCtx, cancel := context.WithTimeout(ctx, lspTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, command, args...)
	cmd.Dir = root
	return cmd.CombinedOutput()
}

func parseLSPDiagnostics(root string, output []byte) []Diagnostic {
	diagnostics := []Diagnostic{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		location, ok := parseLSPLocation(root, scanner.Text())
		if !ok || location.Detail == "" {
			continue
		}
		diagnostics = append(diagnostics, Diagnostic{
			Path:     location.Path,
			Line:     location.Line,
			Column:   location.Column,
			Severity: "error",
			Message:  location.Detail,
		})
		if len(diagnostics) >= maxDiagnostics {
			break
		}
	}
	return diagnostics
}

func parseLSPSymbols(root string, output []byte, query string) []WorkspaceSymbol {
	symbols := []WorkspaceSymbol{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		location, ok := parseLSPLocation(root, scanner.Text())
		if !ok {
			continue
		}
		name, kind := parseLSPSymbolDetail(location.Detail)
		if name == "" {
			continue
		}
		symbol := WorkspaceSymbol{Name: name, Kind: kind, Path: location.Path, Line: location.Line}
		if matchesSymbolQuery(symbol, query) {
			symbols = append(symbols, symbol)
		}
	}
	return symbols
}

type lspLocation struct {
	Path   string
	Line   int
	Column int
	Detail string
}

func parseLSPLocation(root string, line string) (lspLocation, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "Log:") || strings.HasPrefix(line, "Info:") {
		return lspLocation{}, false
	}
	match := lspLocationRE.FindStringSubmatch(line)
	if match == nil {
		return lspLocation{}, false
	}
	lineNumber, err := strconv.Atoi(match[2])
	if err != nil {
		return lspLocation{}, false
	}
	column, err := strconv.Atoi(match[3])
	if err != nil {
		return lspLocation{}, false
	}
	path := displayLSPPath(root, match[1])
	if path == "" {
		return lspLocation{}, false
	}
	return lspLocation{Path: path, Line: lineNumber, Column: column, Detail: strings.TrimSpace(match[4])}, true
}

func displayLSPPath(root string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		relative, err := filepath.Rel(root, path)
		if err == nil && !strings.HasPrefix(relative, "..") {
			path = relative
		}
	}
	return filepath.ToSlash(path)
}

func parseLSPSymbolDetail(detail string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(detail))
	if len(fields) == 0 {
		return "", "symbol"
	}
	if len(fields) == 1 {
		return cleanSymbolName(fields[0]), "symbol"
	}
	firstKind := normalizeSymbolKind(fields[0])
	if firstKind != "" {
		return cleanSymbolName(fields[1]), firstKind
	}
	lastKind := normalizeSymbolKind(fields[len(fields)-1])
	if lastKind != "" {
		return cleanSymbolName(fields[0]), lastKind
	}
	return cleanSymbolName(fields[0]), "symbol"
}

func normalizeSymbolKind(value string) string {
	switch strings.ToLower(strings.Trim(value, "():")) {
	case "func", "function":
		return "func"
	case "method":
		return "method"
	case "type", "class", "struct", "interface":
		return "type"
	case "var", "variable":
		return "var"
	case "const", "constant":
		return "const"
	default:
		return ""
	}
}

func cleanSymbolName(value string) string {
	value = strings.Trim(value, "`\"'(),")
	if index := strings.LastIndex(value, "."); index >= 0 && index < len(value)-1 {
		value = value[index+1:]
	}
	return value
}
