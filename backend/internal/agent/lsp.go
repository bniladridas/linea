package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
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
	if !r.lspConfigured() {
		return nil, false
	}
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
	if !r.lspConfigured() {
		return nil, false
	}
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

func (r *Runtime) listReferencesWithLSP(ctx context.Context, root string, query string, fallback []WorkspaceReference) ([]WorkspaceReference, bool) {
	if !r.lspConfigured() {
		return nil, false
	}
	positions, err := identifierPositions(ctx, root, query, maxReferences)
	if err != nil || len(positions) == 0 {
		return nil, false
	}
	refCtx, cancel := context.WithTimeout(ctx, lspTimeout)
	defer cancel()
	seen := map[string]bool{}
	references := []WorkspaceReference{}
	for _, position := range positions {
		output, err := r.runLSP(refCtx, root, "references", "-d", position)
		if err != nil {
			return nil, false
		}
		for _, reference := range parseLSPReferences(root, output, query) {
			key := reference.Path + ":" + strconv.Itoa(reference.Line) + ":" + reference.Text
			if seen[key] {
				continue
			}
			seen[key] = true
			references = append(references, reference)
			if len(references) >= maxReferences {
				break
			}
		}
		if len(references) >= maxReferences {
			break
		}
	}
	if len(references) < len(fallback) {
		return nil, false
	}
	return references, true
}

func (r *Runtime) runLSP(ctx context.Context, root string, args ...string) ([]byte, error) {
	command := strings.TrimSpace(r.lspCommand)
	if !r.lspConfigured() {
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

func (r *Runtime) lspConfigured() bool {
	command := strings.TrimSpace(r.lspCommand)
	return command != "" && !strings.EqualFold(command, "off") && !strings.EqualFold(command, "none")
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

func parseLSPReferences(root string, output []byte, query string) []WorkspaceReference {
	references := []WorkspaceReference{}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		location, ok := parseLSPLocation(root, scanner.Text())
		if !ok {
			continue
		}
		text, ok := workspaceSourceLine(root, location.Path, location.Line)
		if !ok {
			continue
		}
		references = append(references, WorkspaceReference{Name: query, Path: location.Path, Line: location.Line, Text: text})
	}
	return references
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
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return ""
		}
		path = relative
	}
	path = filepath.Clean(path)
	if path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) || filepath.IsAbs(path) {
		return ""
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

func workspaceSourceLine(root string, displayPath string, line int) (string, bool) {
	path, ok := resolvedWorkspacePath(root, displayPath)
	if !ok {
		return "", false
	}
	text, ok := sourceLine(path, line)
	if !ok {
		return "", false
	}
	return text, true
}

func resolvedWorkspacePath(root string, displayPath string) (string, bool) {
	cleanPath := filepath.Clean(filepath.FromSlash(displayPath))
	if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanPath) {
		return "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	fullPath, err := filepath.EvalSymlinks(filepath.Join(resolvedRoot, cleanPath))
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(resolvedRoot, fullPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", false
	}
	return fullPath, true
}

func sourceLine(path string, line int) (string, bool) {
	if line <= 0 {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxReadBytes || !utf8.Valid(data) {
		return "", false
	}
	lines := strings.Split(string(data), "\n")
	if line > len(lines) {
		return "", false
	}
	return strings.TrimSpace(lines[line-1]), true
}
