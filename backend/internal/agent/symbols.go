package agent

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxSymbolFiles = 200
	maxSymbols     = 100
)

func (r *Runtime) ListSymbols(ctx context.Context, query string) ([]WorkspaceSymbol, error) {
	root, err := r.workspaceRootPath()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	fileSet := token.NewFileSet()
	symbols := []WorkspaceSymbol{}
	filesSeen := 0
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
		if filesSeen > maxSymbolFiles || len(symbols) >= maxSymbols {
			return filepath.SkipAll
		}
		file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		displayPath := filepath.ToSlash(relative)
		ast.Inspect(file, func(node ast.Node) bool {
			if len(symbols) >= maxSymbols {
				return false
			}
			for _, symbol := range symbolsFromNode(fileSet, displayPath, node) {
				if !matchesSymbolQuery(symbol, query) {
					continue
				}
				symbols = append(symbols, symbol)
				if len(symbols) >= maxSymbols {
					return false
				}
			}
			return true
		})
		if len(symbols) >= maxSymbols {
			return filepath.SkipAll
		}
		return nil
	})
	if errors.Is(err, filepath.SkipAll) {
		err = nil
	}
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].Path == symbols[j].Path {
			if symbols[i].Line == symbols[j].Line {
				return symbols[i].Name < symbols[j].Name
			}
			return symbols[i].Line < symbols[j].Line
		}
		return symbols[i].Path < symbols[j].Path
	})
	return symbols, err
}

func symbolsFromNode(fileSet *token.FileSet, path string, node ast.Node) []WorkspaceSymbol {
	switch item := node.(type) {
	case *ast.FuncDecl:
		kind := "func"
		name := item.Name.Name
		if item.Recv != nil {
			kind = "method"
		}
		return []WorkspaceSymbol{{Name: name, Kind: kind, Path: path, Line: fileSet.Position(item.Pos()).Line}}
	case *ast.TypeSpec:
		return []WorkspaceSymbol{{Name: item.Name.Name, Kind: "type", Path: path, Line: fileSet.Position(item.Pos()).Line}}
	case *ast.ValueSpec:
		kind := "var"
		out := make([]WorkspaceSymbol, 0, len(item.Names))
		for _, name := range item.Names {
			out = append(out, WorkspaceSymbol{Name: name.Name, Kind: kind, Path: path, Line: fileSet.Position(name.Pos()).Line})
		}
		return out
	default:
		return nil
	}
}

func matchesSymbolQuery(symbol WorkspaceSymbol, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(symbol.Name), query) ||
		strings.Contains(strings.ToLower(symbol.Path), query) ||
		strings.Contains(strings.ToLower(symbol.Kind), query)
}
