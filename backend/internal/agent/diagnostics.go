package agent

import (
	"context"
	"errors"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
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
	diagnostics := []Diagnostic{}
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
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path == diagnostics[j].Path {
			if diagnostics[i].Line == diagnostics[j].Line {
				return diagnostics[i].Column < diagnostics[j].Column
			}
			return diagnostics[i].Line < diagnostics[j].Line
		}
		return diagnostics[i].Path < diagnostics[j].Path
	})
	return diagnostics, err
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
