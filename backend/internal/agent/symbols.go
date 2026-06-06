package agent

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxSymbolFiles = 200
	maxSymbols     = 100
	maxReferences  = 100
)

func (r *Runtime) ListSymbols(ctx context.Context, query string) ([]WorkspaceSymbol, error) {
	root, err := r.workspaceRootPath()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if symbols, ok := r.listSymbolsWithLSP(ctx, root, query); ok {
		sortSymbols(symbols)
		return symbols, nil
	}
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
	sortSymbols(symbols)
	return symbols, err
}

func (r *Runtime) ListReferences(ctx context.Context, query string) ([]WorkspaceReference, error) {
	query = strings.TrimSpace(query)
	if !isGoIdentifier(query) {
		return nil, errors.New("reference query must be a Go identifier")
	}
	root, err := r.workspaceRootPath()
	if err != nil {
		return nil, err
	}
	references, err := scanReferences(ctx, root, query)
	if err != nil {
		return nil, err
	}
	if lspReferences, ok := r.listReferencesWithLSP(ctx, root, query, references); ok {
		sortReferences(lspReferences)
		return lspReferences, nil
	}
	return references, nil
}

func scanReferences(ctx context.Context, root string, query string) ([]WorkspaceReference, error) {
	fileSet := token.NewFileSet()
	references := []WorkspaceReference{}
	filesSeen := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
		if filesSeen > maxSymbolFiles || len(references) >= maxReferences {
			return filepath.SkipAll
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || len(data) > maxReadBytes || !utf8.Valid(data) {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		displayPath := filepath.ToSlash(relative)
		lines := strings.Split(string(data), "\n")
		file := fileSet.AddFile(path, fileSet.Base(), len(data))
		var scanned scanner.Scanner
		scanned.Init(file, data, nil, 0)
		for {
			pos, tokenType, literal := scanned.Scan()
			if tokenType == token.EOF {
				break
			}
			if tokenType != token.IDENT || literal != query {
				continue
			}
			position := fileSet.Position(pos)
			text := ""
			if position.Line > 0 && position.Line <= len(lines) {
				text = strings.TrimSpace(lines[position.Line-1])
			}
			references = append(references, WorkspaceReference{Name: query, Path: displayPath, Line: position.Line, Text: text})
			if len(references) >= maxReferences {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if errors.Is(err, filepath.SkipAll) {
		err = nil
	}
	sortReferences(references)
	return references, err
}

func sortSymbols(symbols []WorkspaceSymbol) {
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].Path == symbols[j].Path {
			if symbols[i].Line == symbols[j].Line {
				return symbols[i].Name < symbols[j].Name
			}
			return symbols[i].Line < symbols[j].Line
		}
		return symbols[i].Path < symbols[j].Path
	})
}

func sortReferences(references []WorkspaceReference) {
	sort.Slice(references, func(i, j int) bool {
		if references[i].Path == references[j].Path {
			return references[i].Line < references[j].Line
		}
		return references[i].Path < references[j].Path
	})
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

func goFiles(ctx context.Context, root string, limit int) ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
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
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(relative))
		if limit > 0 && len(files) >= limit {
			return filepath.SkipAll
		}
		return nil
	})
	if errors.Is(err, filepath.SkipAll) {
		err = nil
	}
	sort.Strings(files)
	return files, err
}

func identifierPositions(ctx context.Context, root string, query string, limit int) ([]string, error) {
	positions := []string{}
	files, err := goFiles(ctx, root, maxSymbolFiles)
	if err != nil {
		return nil, err
	}
	fileSet := token.NewFileSet()
	for _, displayPath := range files {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		path := filepath.Join(root, filepath.FromSlash(displayPath))
		data, readErr := os.ReadFile(path)
		if readErr != nil || len(data) > maxReadBytes || !utf8.Valid(data) {
			continue
		}
		file := fileSet.AddFile(path, fileSet.Base(), len(data))
		var scanned scanner.Scanner
		scanned.Init(file, data, nil, 0)
		for {
			pos, tokenType, literal := scanned.Scan()
			if tokenType == token.EOF {
				break
			}
			if tokenType != token.IDENT || literal != query {
				continue
			}
			position := fileSet.Position(pos)
			positions = append(positions, displayPath+":"+strconv.Itoa(position.Line)+":"+strconv.Itoa(position.Column))
			if limit > 0 && len(positions) >= limit {
				return positions, nil
			}
		}
	}
	return positions, nil
}

func isGoIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if index == 0 {
			if char != '_' && !unicode.IsLetter(char) {
				return false
			}
			continue
		}
		if char != '_' && !unicode.IsLetter(char) && !unicode.IsDigit(char) {
			return false
		}
	}
	return true
}

func matchesSymbolQuery(symbol WorkspaceSymbol, query string) bool {
	if query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(symbol.Name), query) ||
		strings.Contains(strings.ToLower(symbol.Path), query) ||
		strings.Contains(strings.ToLower(symbol.Kind), query)
}
