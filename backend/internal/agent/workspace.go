package agent

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	ErrWorkspaceDisabled = errors.New("workspace tools are disabled")
	ErrPathOutsideRoot   = errors.New("path is outside workspace")
)

const (
	maxReadBytes    = 128 * 1024
	maxSearchFiles  = 200
	maxSearchResult = 50
)

type FileResult struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated"`
}

type SearchResult struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func WithWorkspaceRoot(root string) func(*Runtime) {
	return func(r *Runtime) {
		r.workspaceRoot = strings.TrimSpace(root)
	}
}

func (r *Runtime) WorkspaceEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return strings.TrimSpace(r.workspaceRoot) != ""
}

func (r *Runtime) WorkspaceRoot() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	root := strings.TrimSpace(r.workspaceRoot)
	if root == "" {
		return ""
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return absolute
	}
	return resolved
}

func (r *Runtime) SetWorkspaceRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		r.mu.Lock()
		r.workspaceRoot = ""
		r.editProposals = nil
		r.mu.Unlock()
		return "", nil
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("workspace root is not a directory")
	}
	r.mu.Lock()
	r.workspaceRoot = resolved
	r.editProposals = nil
	r.mu.Unlock()
	return resolved, nil
}

func (r *Runtime) ReadFile(ctx context.Context, name string) (FileResult, error) {
	fullPath, displayPath, err := r.workspacePath(name)
	if err != nil {
		return FileResult{}, err
	}
	select {
	case <-ctx.Done():
		return FileResult{}, ctx.Err()
	default:
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return FileResult{}, err
	}
	if info.IsDir() {
		return FileResult{}, errors.New("path is a directory")
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return FileResult{}, err
	}
	truncated := false
	if len(data) > maxReadBytes {
		data = data[:maxReadBytes]
		truncated = true
	}
	if !utf8.Valid(data) {
		return FileResult{}, errors.New("file is not text")
	}
	return FileResult{Path: displayPath, Content: string(data), Size: info.Size(), Truncated: truncated}, nil
}

func (r *Runtime) SearchFiles(ctx context.Context, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, errors.New("search query is too short")
	}
	root, err := r.workspaceRootPath()
	if err != nil {
		return nil, err
	}
	lowerQuery := strings.ToLower(query)
	results := []SearchResult{}
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
		if shouldSkipFile(entry.Name()) {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		filesSeen++
		if filesSeen > maxSearchFiles || len(results) >= maxSearchResult {
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
		lines := strings.Split(string(data), "\n")
		for index, line := range lines {
			if strings.Contains(strings.ToLower(line), lowerQuery) {
				results = append(results, SearchResult{
					Path: filepath.ToSlash(relative),
					Line: index + 1,
					Text: strings.TrimSpace(line),
				})
				if len(results) >= maxSearchResult {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if errors.Is(err, filepath.SkipAll) {
		err = nil
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Path == results[j].Path {
			return results[i].Line < results[j].Line
		}
		return results[i].Path < results[j].Path
	})
	return results, err
}

func (r *Runtime) workspacePath(name string) (string, string, error) {
	root, err := r.workspaceRootPath()
	if err != nil {
		return "", "", err
	}
	name = strings.TrimSpace(name)
	if name == "" || filepath.IsAbs(name) {
		return "", "", ErrPathOutsideRoot
	}
	cleanName := filepath.Clean(name)
	if cleanName == "." || strings.HasPrefix(cleanName, "..") {
		return "", "", ErrPathOutsideRoot
	}
	fullPath := filepath.Join(root, cleanName)
	fullPath, err = filepath.EvalSymlinks(fullPath)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(root, fullPath)
	if err != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return "", "", ErrPathOutsideRoot
	}
	return fullPath, filepath.ToSlash(relative), nil
}

func (r *Runtime) workspaceRootPath() (string, error) {
	r.mu.RLock()
	rootValue := strings.TrimSpace(r.workspaceRoot)
	r.mu.RUnlock()
	if rootValue == "" {
		return "", ErrWorkspaceDisabled
	}
	root, err := filepath.Abs(rootValue)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("workspace root is not a directory")
	}
	return root, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", ".next", ".cache":
		return true
	default:
		return false
	}
}

func shouldSkipFile(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".lock")
}
