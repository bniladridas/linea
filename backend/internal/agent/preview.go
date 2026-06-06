package agent

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxAgentPreviewItems = 25
	maxAppSessionItems   = 25
)

func (r *Runtime) registerAgentPreview(loopID string, sessionID string, root string, entry string) AgentPreview {
	preview := AgentPreview{
		ID:        newTraceID(),
		LoopID:    loopID,
		SessionID: strings.TrimSpace(sessionID),
		Root:      root,
		Entry:     entry,
		URL:       "/api/agent/previews/",
		CreatedAt: time.Now().UTC(),
	}
	preview.URL += preview.ID + "/"

	r.mu.Lock()
	r.agentPreviews = append([]AgentPreview{preview}, r.agentPreviews...)
	evicted := []AgentPreview{}
	if len(r.agentPreviews) > maxAgentPreviewItems {
		evicted = append(evicted, r.agentPreviews[maxAgentPreviewItems:]...)
		r.agentPreviews = r.agentPreviews[:maxAgentPreviewItems]
	}
	r.mu.Unlock()
	for _, item := range evicted {
		if strings.TrimSpace(item.Root) != "" {
			_ = os.RemoveAll(item.Root)
		}
	}
	return preview
}

func (r *Runtime) HasAppSession(sessionID string) bool {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, session := range r.appSessions {
		if session.ID == sessionID {
			return true
		}
	}
	return false
}

func (r *Runtime) RecoverAppSession(sessionID string, root string) bool {
	sessionID = strings.TrimSpace(sessionID)
	root = strings.TrimSpace(root)
	if sessionID == "" || root == "" {
		return false
	}
	if !isRecoverableAppSessionRoot(root) {
		return false
	}
	r.saveAppSession(AppSession{ID: sessionID, Root: root})
	return true
}

func isRecoverableAppSessionRoot(root string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	realTemp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(realTemp, realRoot)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." || filepath.IsAbs(relative) {
		return false
	}
	if !strings.HasPrefix(filepath.Base(realRoot), "linea-app-") {
		return false
	}
	for _, name := range []string{"package.json", "index.html", filepath.Join("src", "App.jsx")} {
		info, err := os.Stat(filepath.Join(realRoot, name))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

func (r *Runtime) appSession(sessionID string) (AppSession, bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return AppSession{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, session := range r.appSessions {
		if session.ID == sessionID {
			return session, true
		}
	}
	return AppSession{}, false
}

func (r *Runtime) saveAppSession(session AppSession) {
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now
	replacedRoot := ""
	r.mu.Lock()
	for index, current := range r.appSessions {
		if current.ID == session.ID {
			if current.Root != session.Root {
				replacedRoot = current.Root
			}
			r.appSessions[index] = session
			r.mu.Unlock()
			if strings.TrimSpace(replacedRoot) != "" {
				_ = os.RemoveAll(replacedRoot)
			}
			return
		}
	}
	r.appSessions = append([]AppSession{session}, r.appSessions...)
	evicted := []AppSession{}
	if len(r.appSessions) > maxAppSessionItems {
		evicted = append(evicted, r.appSessions[maxAppSessionItems:]...)
		r.appSessions = r.appSessions[:maxAppSessionItems]
	}
	r.mu.Unlock()
	for _, item := range evicted {
		if strings.TrimSpace(item.Root) != "" {
			_ = os.RemoveAll(item.Root)
		}
	}
}

func (r *Runtime) PreviewFile(ctx context.Context, id string, name string) (PreviewFile, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return PreviewFile{}, errors.New("Preview ID is required.")
	}

	r.mu.RLock()
	var preview AgentPreview
	for _, item := range r.agentPreviews {
		if item.ID == id {
			preview = item
			break
		}
	}
	r.mu.RUnlock()
	if preview.ID == "" {
		return PreviewFile{}, errors.New("Preview was not found.")
	}

	select {
	case <-ctx.Done():
		return PreviewFile{}, ctx.Err()
	default:
	}

	entry := strings.TrimSpace(name)
	if entry == "" {
		entry = preview.Entry
	}
	entry = filepath.Clean(entry)
	if entry == "." || filepath.IsAbs(entry) || strings.HasPrefix(entry, ".."+string(filepath.Separator)) || entry == ".." {
		return PreviewFile{}, errors.New("Preview path is invalid.")
	}
	root, err := filepath.EvalSymlinks(preview.Root)
	if err != nil {
		return PreviewFile{}, err
	}
	path := filepath.Join(root, entry)
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return PreviewFile{}, err
	}
	relative, err := filepath.Rel(root, realPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return PreviewFile{}, errors.New("Preview path is outside the workspace.")
	}
	content, err := os.ReadFile(realPath)
	if err != nil {
		return PreviewFile{}, err
	}
	contentType := mime.TypeByExtension(filepath.Ext(realPath))
	switch filepath.Ext(realPath) {
	case ".js", ".jsx", ".mjs":
		contentType = "text/javascript; charset=utf-8"
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".html":
		contentType = "text/html; charset=utf-8"
	}
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	return PreviewFile{
		Path:        filepath.ToSlash(relative),
		ContentType: contentType,
		Content:     content,
	}, nil
}
