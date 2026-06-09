package agent

import (
	"bytes"
	"context"
	"errors"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const projectToolTimeout = 15 * time.Second

var (
	tscDiagnosticRE    = regexp.MustCompile(`^(.+)\((\d+),(\d+)\):\s+(error|warning)\s+(TS\d+):\s+(.+)$`)
	eslintDiagnosticRE = regexp.MustCompile(`^(.+):\s+line\s+(\d+),\s+col\s+(\d+),\s+(Error|Warning)\s+-\s+(.+)$`)
	eslintConfigExists = []string{".eslintrc", ".eslintrc.json", ".eslintrc.js", ".eslintrc.yaml", ".eslintrc.yml"}
)

const (
	maxDiagnosticFiles = 200
	maxDiagnostics     = 100
)

type Diagnostic struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

func (r *Runtime) ListDiagnostics(ctx context.Context) ([]Diagnostic, error) {
	root, err := r.workspaceRootPath()
	if err != nil {
		return nil, err
	}
	if diagnostics, ok := r.listDiagnosticsWithLSP(ctx, root); ok {
		sortDiagnostics(diagnostics)
		return diagnostics, nil
	}
	diagnostics := r.listDiagnosticsWithProjectTools(ctx, root)
	if len(diagnostics) > 0 {
		sortDiagnostics(diagnostics)
		return diagnostics, nil
	}
	filesSeen := 0
	fileSet := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(entry.Name()) || entry.Type()&fs.ModeSymlink != 0 || filepath.Ext(entry.Name()) != ".go" {
			return nil
		}
		filesSeen++
		if filesSeen > maxDiagnosticFiles || len(diagnostics) >= maxDiagnostics {
			return filepath.SkipAll
		}
		_, parseErr := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if parseErr == nil {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		for _, message := range diagnosticMessages(parseErr) {
			diagnostics = append(diagnostics, Diagnostic{
				Path:     filepath.ToSlash(relative),
				Line:     message.Line,
				Column:   message.Column,
				Severity: "error",
				Message:  strings.TrimSpace(message.Text),
			})
			if len(diagnostics) >= maxDiagnostics {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if errors.Is(err, filepath.SkipAll) {
		err = nil
	}
	sortDiagnostics(diagnostics)
	return diagnostics, err
}

func sortDiagnostics(diagnostics []Diagnostic) {
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path == diagnostics[j].Path {
			if diagnostics[i].Line == diagnostics[j].Line {
				return diagnostics[i].Column < diagnostics[j].Column
			}
			return diagnostics[i].Line < diagnostics[j].Line
		}
		return diagnostics[i].Path < diagnostics[j].Path
	})
}

type diagnosticMessage struct {
	Line   int
	Column int
	Text   string
}

func diagnosticMessages(err error) []diagnosticMessage {
	var list scanner.ErrorList
	if errors.As(err, &list) {
		out := make([]diagnosticMessage, 0, len(list))
		for _, item := range list {
			out = append(out, diagnosticMessage{Line: item.Pos.Line, Column: item.Pos.Column, Text: item.Msg})
		}
		return out
	}
	var scanErr *scanner.Error
	if errors.As(err, &scanErr) {
		return []diagnosticMessage{{Line: scanErr.Pos.Line, Column: scanErr.Pos.Column, Text: scanErr.Msg}}
	}
	return []diagnosticMessage{{Text: err.Error()}}
}

func (r *Runtime) listDiagnosticsWithProjectTools(ctx context.Context, root string) []Diagnostic {
	diagnostics := []Diagnostic{}
	hasPackageJSON := false
	if _, err := os.Stat(filepath.Join(root, "package.json")); err == nil {
		hasPackageJSON = true
	}
	if _, err := os.Stat(filepath.Join(root, "tsconfig.json")); err == nil && hasPackageJSON {
		output, err := r.runProjectTool(ctx, root, "npx", "--yes", "tsc", "--noEmit", "--pretty", "false")
		if err != nil || len(output) == 0 {
			if diags := parseTSCOutput(root, string(output)); len(diags) > 0 {
				diagnostics = append(diagnostics, diags...)
			}
		}
		if len(diagnostics) >= maxDiagnostics {
			return diagnostics[:maxDiagnostics]
		}
	}
	if hasPackageJSON {
		hasESLint := false
		for _, name := range eslintConfigExists {
			if _, err := os.Stat(filepath.Join(root, name)); err == nil {
				hasESLint = true
				break
			}
		}
		if !hasESLint {
			hasESLint = hasESLintConfigInPackageJSON(root)
		}
		if hasESLint {
			output, err := r.runProjectTool(ctx, root, "npx", "--yes", "eslint", ".", "--format", "compact")
			if err != nil || len(output) == 0 {
				if diags := parseESLintOutput(root, string(output)); len(diags) > 0 {
					diagnostics = append(diagnostics, diags...)
				}
			}
			if len(diagnostics) >= maxDiagnostics {
				return diagnostics[:maxDiagnostics]
			}
		}
	}
	return diagnostics
}

func (r *Runtime) runProjectTool(ctx context.Context, root string, name string, args ...string) ([]byte, error) {
	toolCtx, cancel := context.WithTimeout(ctx, projectToolTimeout)
	defer cancel()
	cmd := exec.CommandContext(toolCtx, name, args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) == 0 {
			return nil, err
		}
		return output, nil
	}
	return output, nil
}

func hasESLintConfigInPackageJSON(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte("eslintConfig")) || bytes.Contains(data, []byte(`"eslint"`))
}

func parseTSCOutput(root string, output string) []Diagnostic {
	diagnostics := []Diagnostic{}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		match := tscDiagnosticRE.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		path := displayLSPPath(root, match[1])
		if path == "" {
			continue
		}
		lineNum, _ := strconv.Atoi(match[2])
		column, _ := strconv.Atoi(match[3])
		severity := strings.ToLower(match[4])
		message := match[5] + ": " + match[6]
		diagnostics = append(diagnostics, Diagnostic{
			Path:     path,
			Line:     lineNum,
			Column:   column,
			Severity: severity,
			Message:  message,
		})
		if len(diagnostics) >= maxDiagnostics {
			break
		}
	}
	return diagnostics
}

func parseESLintOutput(root string, output string) []Diagnostic {
	diagnostics := []Diagnostic{}
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		match := eslintDiagnosticRE.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		path := displayLSPPath(root, match[1])
		if path == "" {
			continue
		}
		lineNum, _ := strconv.Atoi(match[2])
		column, _ := strconv.Atoi(match[3])
		severity := strings.ToLower(match[4])
		message := match[5]
		diagnostics = append(diagnostics, Diagnostic{
			Path:     path,
			Line:     lineNum,
			Column:   column,
			Severity: severity,
			Message:  message,
		})
		if len(diagnostics) >= maxDiagnostics {
			break
		}
	}
	return diagnostics
}
