package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"linea/backend/internal/agent"
	"linea/backend/internal/llm"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
)

type Server struct {
	store          store.Store
	llmClient      Assistant
	searchClient   WebSearcher
	staticFiles    http.FileSystem
	origin         string
	statusProvider StatusProvider
	agentProvider  AgentStatusProvider
	agentRuntime   AgentRuntime
	settingsStore  SettingsStore
}

type Status struct {
	Storage   string           `json:"storage"`
	Search    string           `json:"search"`
	Providers []ProviderStatus `json:"providers"`
}

type ProviderStatus struct {
	Name    string `json:"name"`
	Model   string `json:"model,omitempty"`
	Enabled bool   `json:"enabled"`
	Role    string `json:"role"`
	State   string `json:"state,omitempty"`
	Message string `json:"message,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type StatusProvider func(context.Context) Status

type AgentStatusProvider func(context.Context) agent.Status

type AgentRuntime interface {
	Status(context.Context) agent.Status
	ListTraces(context.Context) []agent.Trace
	AddTrace(context.Context, agent.TraceInput) (agent.Trace, error)
	ReadFile(context.Context, string) (agent.FileResult, error)
	SearchFiles(context.Context, string) ([]agent.SearchResult, error)
}

type Settings struct {
	Providers []ProviderSetting `json:"providers"`
}

type ProviderSetting struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Model      string `json:"model,omitempty"`
	Role       string `json:"role"`
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
}

type SettingsStore interface {
	GetSettings() Settings
	UpdateSettings(Settings) (Settings, error)
}

type Assistant interface {
	GenerateStream(ctx context.Context, messages []store.Message, attachments []llm.Attachment, searchResults []llm.SearchResult, onChunk func(string) error) error
}

type WebSearcher interface {
	Search(ctx context.Context, query string) ([]search.Result, error)
}

func NewServer(store store.Store, llmClient Assistant, searchClient WebSearcher, staticFiles http.FileSystem, origin string, status Status) *Server {
	return NewServerWithStatus(store, llmClient, searchClient, staticFiles, origin, func(context.Context) Status {
		return status
	})
}

func NewServerWithStatus(store store.Store, llmClient Assistant, searchClient WebSearcher, staticFiles http.FileSystem, origin string, statusProvider StatusProvider) *Server {
	return NewServerWithStatusAndSettings(store, llmClient, searchClient, staticFiles, origin, statusProvider, nil)
}

func NewServerWithStatusAndSettings(
	store store.Store,
	llmClient Assistant,
	searchClient WebSearcher,
	staticFiles http.FileSystem,
	origin string,
	statusProvider StatusProvider,
	settingsStore SettingsStore,
) *Server {
	return NewServerWithAgentStatus(store, llmClient, searchClient, staticFiles, origin, statusProvider, settingsStore, nil)
}

func NewServerWithAgentStatus(
	store store.Store,
	llmClient Assistant,
	searchClient WebSearcher,
	staticFiles http.FileSystem,
	origin string,
	statusProvider StatusProvider,
	settingsStore SettingsStore,
	agentProvider AgentStatusProvider,
) *Server {
	return &Server{
		store:          store,
		llmClient:      llmClient,
		searchClient:   searchClient,
		staticFiles:    staticFiles,
		origin:         origin,
		statusProvider: statusProvider,
		agentProvider:  agentProvider,
		settingsStore:  settingsStore,
	}
}

func NewServerWithAgentRuntime(
	store store.Store,
	llmClient Assistant,
	searchClient WebSearcher,
	staticFiles http.FileSystem,
	origin string,
	statusProvider StatusProvider,
	settingsStore SettingsStore,
	agentRuntime AgentRuntime,
) *Server {
	return &Server{
		store:          store,
		llmClient:      llmClient,
		searchClient:   searchClient,
		staticFiles:    staticFiles,
		origin:         origin,
		statusProvider: statusProvider,
		agentRuntime:   agentRuntime,
		settingsStore:  settingsStore,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /api/status", s.getStatus)
	mux.HandleFunc("GET /api/agent", s.getAgentStatus)
	mux.HandleFunc("GET /api/agent/traces", s.listAgentTraces)
	mux.HandleFunc("POST /api/agent/traces", s.createAgentTrace)
	mux.HandleFunc("GET /api/agent/workspace/file", s.readAgentWorkspaceFile)
	mux.HandleFunc("GET /api/agent/workspace/search", s.searchAgentWorkspace)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PATCH /api/settings", s.updateSettings)
	mux.HandleFunc("GET /api/conversations", s.listConversations)
	mux.HandleFunc("POST /api/conversations", s.createConversation)
	mux.HandleFunc("PATCH /api/conversations/{id}", s.updateConversation)
	mux.HandleFunc("DELETE /api/conversations/{id}", s.deleteConversation)
	mux.HandleFunc("GET /api/conversations/{id}/messages", s.listMessages)
	mux.HandleFunc("POST /api/conversations/{id}/messages", s.createMessage)
	mux.HandleFunc("GET /", s.serveWebApp)
	return s.cors(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.statusProvider(r.Context()))
}

func (s *Server) getAgentStatus(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime != nil {
		writeJSON(w, http.StatusOK, s.agentRuntime.Status(r.Context()))
		return
	}
	if s.agentProvider == nil {
		writeJSON(w, http.StatusOK, agent.NewRuntime("").Status(r.Context()))
		return
	}
	writeJSON(w, http.StatusOK, s.agentProvider(r.Context()))
}

func (s *Server) listAgentTraces(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeJSON(w, http.StatusOK, []agent.Trace{})
		return
	}
	writeJSON(w, http.StatusOK, s.agentRuntime.ListTraces(r.Context()))
}

func (s *Server) createAgentTrace(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent traces are not available.")
		return
	}
	var input agent.TraceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	trace, err := s.agentRuntime.AddTrace(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, trace)
}

func (s *Server) readAgentWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent workspace is not available.")
		return
	}
	result, err := s.agentRuntime.ReadFile(r.Context(), r.URL.Query().Get("path"))
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "read file", "completed", result.Path)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) searchAgentWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil {
		writeError(w, http.StatusNotFound, "Agent workspace is not available.")
		return
	}
	results, err := s.agentRuntime.SearchFiles(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		writeAgentToolError(w, err)
		return
	}
	s.recordAgentTrace(r.Context(), "search files", "completed", r.URL.Query().Get("q"))
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) recordAgentTrace(ctx context.Context, event string, state string, detail string) {
	if s.agentRuntime == nil {
		return
	}
	if _, err := s.agentRuntime.AddTrace(ctx, agent.TraceInput{Event: event, State: state, Detail: detail}); err != nil {
		slog.Warn("record agent trace", "error", err)
	}
}

func writeAgentToolError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrWorkspaceDisabled):
		writeError(w, http.StatusNotFound, "Workspace tools are off.")
	case errors.Is(err, agent.ErrPathOutsideRoot):
		writeError(w, http.StatusBadRequest, "Path must stay inside the workspace.")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request) {
	if s.settingsStore == nil {
		writeJSON(w, http.StatusOK, Settings{})
		return
	}
	writeJSON(w, http.StatusOK, s.settingsStore.GetSettings())
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	if s.settingsStore == nil {
		writeError(w, http.StatusNotFound, "Settings are not available.")
		return
	}
	var next Settings
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	updated, err := s.settingsStore.UpdateSettings(next)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) listConversations(w http.ResponseWriter, r *http.Request) {
	conversations, err := s.store.ListConversations(r.Context())
	if err != nil {
		slog.Error("list conversations", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not list conversations.")
		return
	}
	writeJSON(w, http.StatusOK, conversations)
}

func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "Untitled"
	}
	conversation, err := s.store.CreateConversation(r.Context(), req.Title)
	if err != nil {
		slog.Error("create conversation", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not create conversation.")
		return
	}
	writeJSON(w, http.StatusCreated, conversation)
}

func (s *Server) updateConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "Title is required.")
		return
	}
	conversation, err := s.store.UpdateConversationTitle(r.Context(), r.PathValue("id"), title)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	if err != nil {
		slog.Error("update conversation", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not update conversation.")
		return
	}
	writeJSON(w, http.StatusOK, conversation)
}

func (s *Server) deleteConversation(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteConversation(r.Context(), r.PathValue("id")); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	} else if err != nil {
		slog.Error("delete conversation", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not delete conversation.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	messages, err := s.store.ListMessages(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	if err != nil {
		slog.Error("list messages", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not list messages.")
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Message must be multipart form data.")
		return
	}
	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		writeError(w, http.StatusBadRequest, "Message content is required.")
		return
	}
	attachments, err := readAttachments(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	conversationID := r.PathValue("id")
	userMessage, err := s.store.AddMessage(r.Context(), conversationID, "user", content)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Conversation not found.")
		return
	}
	if err != nil {
		slog.Error("add user message", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not save message.")
		return
	}

	messages, err := s.store.ListMessages(r.Context(), conversationID)
	if err != nil {
		slog.Error("load message history", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not load message history.")
		return
	}
	searchResults, err := s.searchIfNeeded(r.Context(), content)
	if err != nil {
		slog.Warn("web search failed", "error", err)
	}
	s.streamAssistantResponse(w, r, conversationID, userMessage, messages, attachments, searchResults)
}

func (s *Server) streamAssistantResponse(w http.ResponseWriter, r *http.Request, conversationID string, userMessage store.Message, messages []store.Message, attachments []llm.Attachment, searchResults []llm.SearchResult) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming is not supported.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusCreated)

	writeEvent(w, "user", userMessage)
	for _, result := range searchResults {
		writeEvent(w, "search", result)
	}
	flusher.Flush()

	var reply strings.Builder
	var currentProvider *llm.ProviderInfo
	generateCtx := llm.WithProviderCallback(r.Context(), func(info llm.ProviderInfo) {
		currentProvider = &info
		writeEvent(w, "provider", info)
		flusher.Flush()
	})
	err := s.llmClient.GenerateStream(generateCtx, messages, attachments, searchResults, func(chunk string) error {
		reply.WriteString(chunk)
		payload := map[string]any{"content": chunk}
		if currentProvider != nil {
			payload["provider"] = currentProvider
		}
		writeEvent(w, "chunk", payload)
		flusher.Flush()
		return nil
	})
	if errors.Is(err, llm.ErrMissingAPIKey) {
		content := "Linea is not configured with GEMINI_API_KEY yet."
		assistantMessage, saveErr := s.store.AddMessage(r.Context(), conversationID, "assistant", content)
		if saveErr != nil {
			slog.Error("add configuration assistant message", "error", saveErr)
			writeEvent(w, "error", map[string]string{"error": "Could not save assistant response."})
			flusher.Flush()
			return
		}
		writeEvent(w, "chunk", map[string]string{"content": content})
		writeEvent(w, "done", assistantMessage)
		flusher.Flush()
		return
	}
	if err != nil {
		slog.Error("generate assistant response", "error", err)
		if llm.HasImageAttachments(attachments) {
			writeEvent(w, "error", map[string]string{"error": "Image input uses Gemini, but Gemini could not respond right now."})
			flusher.Flush()
			return
		}
		writeEvent(w, "error", map[string]string{"error": "Could not generate a response."})
		flusher.Flush()
		return
	}
	assistantMessage, err := s.store.AddMessage(r.Context(), conversationID, "assistant", strings.TrimSpace(reply.String()))
	if err != nil {
		slog.Error("add assistant message", "error", err)
		writeEvent(w, "error", map[string]string{"error": "Could not save assistant response."})
		flusher.Flush()
		return
	}
	writeEvent(w, "done", messageWithProvider{Message: assistantMessage, Provider: currentProvider})
	flusher.Flush()
}

type messageWithProvider struct {
	store.Message
	Provider *llm.ProviderInfo `json:"provider,omitempty"`
}

func (s *Server) searchIfNeeded(ctx context.Context, content string) ([]llm.SearchResult, error) {
	if s.searchClient == nil || !search.ShouldSearch(content) {
		return nil, nil
	}
	results, err := s.searchClient.Search(ctx, search.QueryFromMessage(content))
	if err != nil {
		return nil, err
	}
	searchResults := make([]llm.SearchResult, 0, len(results))
	for _, result := range results {
		searchResults = append(searchResults, llm.SearchResult{
			Title:   result.Title,
			URL:     result.URL,
			Snippet: result.Snippet,
		})
	}
	return searchResults, nil
}

func readAttachments(r *http.Request) ([]llm.Attachment, error) {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, nil
	}
	files := r.MultipartForm.File["files"]
	attachments := make([]llm.Attachment, 0, len(files))
	for _, header := range files {
		mimeType := attachmentMIMEType(header.Filename, header.Header.Get("Content-Type"))
		limit := int64(512 * 1024)
		if strings.HasPrefix(mimeType, "image/") {
			if !isSupportedImageMIME(mimeType) {
				return nil, errors.New("Images must be PNG, JPEG, or WebP.")
			}
			limit = 2 * 1024 * 1024
		}
		if header.Size > limit {
			return nil, errors.New("Attached files must be 512 KB or smaller. Images must be 2 MB or smaller.")
		}
		file, err := header.Open()
		if err != nil {
			return nil, errors.New("Could not read an attached file.")
		}
		content, readErr := io.ReadAll(io.LimitReader(file, limit))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil {
			return nil, errors.New("Could not read an attached file.")
		}
		if strings.HasPrefix(mimeType, "image/") {
			attachments = append(attachments, llm.Attachment{Name: header.Filename, MIMEType: mimeType, Data: content})
			continue
		}
		attachments = append(attachments, llm.Attachment{Name: header.Filename, Content: string(content), MIMEType: mimeType})
	}
	return attachments, nil
}

func attachmentMIMEType(name string, contentType string) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if contentType != "" && contentType != "application/octet-stream" {
		return contentType
	}
	if extType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); extType != "" {
		return strings.Split(extType, ";")[0]
	}
	return "text/plain"
}

func isSupportedImageMIME(mimeType string) bool {
	switch mimeType {
	case "image/png", "image/jpeg", "image/webp":
		return true
	default:
		return false
	}
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", s.origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) serveWebApp(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	filePath := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, "/")), "/")
	if filePath == "" || filePath == "." {
		filePath = "index.html"
	}
	file, err := s.staticFiles.Open(filePath)
	if err == nil {
		defer file.Close()
		info, statErr := file.Stat()
		if statErr == nil && !info.IsDir() {
			http.ServeContent(w, r, filePath, info.ModTime(), file)
			return
		}
	}
	index, err := s.staticFiles.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer index.Close()
	info, err := index.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, "index.html", info.ModTime(), index)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write json", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeEvent(w io.Writer, event string, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		payload = []byte(`{"error":"Could not encode event."}`)
	}
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", payload)
}
