package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"linea/backend/internal/agent"
	"linea/backend/internal/llm"
	"linea/backend/internal/search"
	"linea/backend/internal/store"
)

func TestCreateMessageStreamsAndPersistsAssistant(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	server := NewServer(appStore, fakeAssistant{chunks: []string{"hello ", "world"}}, nil, testFiles(), "", Status{}).Handler()

	res := postMessage(t, server, conversation.ID, "hello")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "event: user") || !strings.Contains(body, "event: chunk") || !strings.Contains(body, "event: done") {
		t.Fatalf("stream body missing expected events:\n%s", body)
	}
	if !strings.Contains(body, "hello ") || !strings.Contains(body, "world") {
		t.Fatalf("stream body missing chunks:\n%s", body)
	}

	messages, err := appStore.ListMessages(context.Background(), conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if messages[1].Role != "assistant" || messages[1].Content != "hello world" {
		t.Fatalf("assistant message = %#v", messages[1])
	}
}

func TestCreateMessageStreamsProviderWhenAvailable(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	server := NewServer(appStore, providerAssistant{chunks: []string{"ok"}}, nil, testFiles(), "", Status{}).Handler()

	res := postMessage(t, server, conversation.ID, "hello")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "event: provider") || !strings.Contains(res.Body.String(), "TestModel") {
		t.Fatalf("stream body missing provider event:\n%s", res.Body.String())
	}
}

func TestCreateMessageFallsBackThroughStream(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	assistant := llm.NewFallbackClient(
		fakeAssistant{err: llm.ErrQuotaExceeded},
		providerAssistant{name: "Ollama", model: "local", chunks: []string{"local ok"}},
	)
	server := NewServer(appStore, assistant, nil, testFiles(), "", Status{}).Handler()

	res := postMessage(t, server, conversation.ID, "hello")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "event: provider") || !strings.Contains(body, "Ollama") {
		t.Fatalf("stream body missing fallback provider event:\n%s", body)
	}
	if !strings.Contains(body, "local ok") || !strings.Contains(body, "event: done") {
		t.Fatalf("stream body missing fallback response:\n%s", body)
	}

	messages, err := appStore.ListMessages(context.Background(), conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[1].Role != "assistant" || messages[1].Content != "local ok" {
		t.Fatalf("persisted messages = %#v", messages)
	}
}

func TestCreateMessagePersistsMissingKeyFallback(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	server := NewServer(appStore, fakeAssistant{err: llm.ErrMissingAPIKey}, nil, testFiles(), "", Status{}).Handler()

	res := postMessage(t, server, conversation.ID, "hello")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	messages, err := appStore.ListMessages(context.Background(), conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if messages[1].Content != "Linea is not configured with GEMINI_API_KEY yet." {
		t.Fatalf("fallback message = %q", messages[1].Content)
	}
}

func TestCreateMessageAddsSearchResultsWhenNeeded(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	assistant := &capturingAssistant{chunks: []string{"ok"}}
	searcher := fakeSearcher{results: []search.Result{{
		Title:   "Result",
		URL:     "https://example.com",
		Snippet: "Snippet",
	}}}
	server := NewServer(appStore, assistant, searcher, testFiles(), "", Status{}).Handler()

	res := postMessage(t, server, conversation.ID, "what is latest go")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	if len(assistant.searchResults) != 1 {
		t.Fatalf("search results passed to assistant = %d, want 1", len(assistant.searchResults))
	}
	if !strings.Contains(res.Body.String(), "event: search") {
		t.Fatalf("stream body missing search event:\n%s", res.Body.String())
	}
}

func TestListMessagesReturnsEmptyArray(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Empty")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	server := NewServer(appStore, fakeAssistant{}, nil, testFiles(), "", Status{}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/conversations/"+conversation.ID+"/messages", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if strings.TrimSpace(res.Body.String()) != "[]" {
		t.Fatalf("body = %q, want []", strings.TrimSpace(res.Body.String()))
	}
}

func TestUpdateConversationRenamesTitle(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Old")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	server := NewServer(appStore, fakeAssistant{}, nil, testFiles(), "", Status{}).Handler()

	body, err := json.Marshal(map[string]string{"title": "New title"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/conversations/"+conversation.ID, bytes.NewReader(body))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if got := conversations[0].Title; got != "New title" {
		t.Fatalf("title = %q, want New title", got)
	}
}

func TestDeleteConversationRemovesMessages(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Delete me")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if _, err := appStore.AddMessage(context.Background(), conversation.ID, "user", "hello"); err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}
	server := NewServer(appStore, fakeAssistant{}, nil, testFiles(), "", Status{}).Handler()

	req := httptest.NewRequest(http.MethodDelete, "/api/conversations/"+conversation.ID, nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusNoContent, res.Body.String())
	}
	if _, err := appStore.ListMessages(context.Background(), conversation.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ListMessages() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteConversationReturnsNotFound(t *testing.T) {
	server := NewServer(store.NewMemoryStore(), fakeAssistant{}, nil, testFiles(), "", Status{}).Handler()

	req := httptest.NewRequest(http.MethodDelete, "/api/conversations/missing", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestGetStatusReturnsSafeSystemState(t *testing.T) {
	status := Status{
		Storage: "PostgreSQL",
		Search:  "DuckDuckGo",
		Providers: []ProviderStatus{{
			Name:    "Cerebras",
			Model:   "gpt-oss-120b",
			Enabled: true,
			Role:    "fallback",
		}},
	}
	server := NewServer(store.NewMemoryStore(), fakeAssistant{}, nil, testFiles(), "", status).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var got Status
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Storage != "PostgreSQL" || got.Search != "DuckDuckGo" || len(got.Providers) != 1 {
		t.Fatalf("status body = %#v", got)
	}
}

func TestGetAgentStatusReturnsLocalContract(t *testing.T) {
	appStore := store.NewMemoryStore()
	agentStatus := agent.Status{
		Mode: "local",
		Rules: agent.RuleSet{
			Source:  "built-in",
			Loaded:  true,
			Summary: []string{"Local-first"},
		},
		Tools: []agent.Tool{{
			ID:       "read_file",
			Name:     "Read files",
			Access:   "workspace",
			Approval: "not required",
		}},
	}
	server := NewServerWithAgentStatus(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, func(context.Context) agent.Status {
		return agentStatus
	}).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var got agent.Status
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Mode != "local" || !got.Rules.Loaded || len(got.Tools) != 1 {
		t.Fatalf("agent status = %#v", got)
	}
}

func TestAgentTraceEndpointsPersistRuntimeEvents(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("")
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	body := strings.NewReader(`{"event":"before tool","state":"recorded","detail":"read-only"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/traces", body)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created agent.Trace
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.ID == "" || created.Event != "before tool" || created.State != "recorded" {
		t.Fatalf("created trace = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agent/traces", nil)
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var traces []agent.Trace
	if err := json.NewDecoder(res.Body).Decode(&traces); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(traces) != 1 || traces[0].ID != created.ID {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestCreateAgentTraceRejectsEmptyEvent(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("")
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/traces", strings.NewReader(`{"event":"","state":"ready"}`))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
}

func TestAgentWorkspaceReadFileEndpoint(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "notes.md"), "agent notes")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/workspace/file?path=notes.md", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var got agent.FileResult
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Path != "notes.md" || got.Content != "agent notes" {
		t.Fatalf("file result = %#v", got)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "read file" || traces[0].Detail != "notes.md" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentWorkspaceSearchEndpoint(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "notes.md"), "agent notes\nother")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/workspace/search?q=agent", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var got []agent.SearchResult
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(got) != 1 || got[0].Path != "notes.md" {
		t.Fatalf("search result = %#v", got)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "search files" || traces[0].Detail != "agent" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentWorkspaceDisabledReturnsNotFound(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("")
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/workspace/search?q=agent", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusNotFound, res.Body.String())
	}
}

func TestReadAttachmentsPreservesImageBytes(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "sample.png")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte("png-bytes")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(8 << 20); err != nil {
		t.Fatalf("ParseMultipartForm() error = %v", err)
	}

	attachments, err := readAttachments(req)
	if err != nil {
		t.Fatalf("readAttachments() error = %v", err)
	}
	if len(attachments) != 1 {
		t.Fatalf("attachment count = %d, want 1", len(attachments))
	}
	if !attachments[0].IsImage() || attachments[0].MIMEType != "image/png" || string(attachments[0].Data) != "png-bytes" {
		t.Fatalf("attachment = %#v", attachments[0])
	}
}

func TestCreateMessageRejectsInvalidAttachmentBeforePersisting(t *testing.T) {
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	server := NewServer(appStore, fakeAssistant{chunks: []string{"ok"}}, nil, testFiles(), "", Status{}).Handler()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("content", "hello"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	part, err := writer.CreateFormFile("files", "sample.gif")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte("gif-bytes")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conversation.ID+"/messages", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
	messages, err := appStore.ListMessages(context.Background(), conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("message count = %d, want 0", len(messages))
	}
}

func postMessage(t *testing.T, handler http.Handler, conversationID, content string) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("content", content); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/conversations/"+conversationID+"/messages", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func testFiles() http.FileSystem {
	return http.FS(fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><html><body>Linea</body></html>")},
	})
}

func writeAPITestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}
}

type fakeAssistant struct {
	chunks []string
	err    error
}

type providerAssistant struct {
	chunks []string
	name   string
	model  string
}

func (f providerAssistant) GenerateStream(ctx context.Context, _ []store.Message, _ []llm.Attachment, _ []llm.SearchResult, onChunk func(string) error) error {
	name := f.name
	if name == "" {
		name = "Test"
	}
	model := f.model
	if model == "" {
		model = "TestModel"
	}
	llm.NotifyProvider(ctx, llm.ProviderInfo{Name: name, Model: model})
	for _, chunk := range f.chunks {
		if err := onChunk(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (f fakeAssistant) GenerateStream(_ context.Context, _ []store.Message, _ []llm.Attachment, _ []llm.SearchResult, onChunk func(string) error) error {
	if f.err != nil {
		return f.err
	}
	for _, chunk := range f.chunks {
		if err := onChunk(chunk); err != nil {
			return err
		}
	}
	return nil
}

type capturingAssistant struct {
	chunks        []string
	searchResults []llm.SearchResult
}

func (f *capturingAssistant) GenerateStream(_ context.Context, _ []store.Message, _ []llm.Attachment, searchResults []llm.SearchResult, onChunk func(string) error) error {
	f.searchResults = searchResults
	for _, chunk := range f.chunks {
		if err := onChunk(chunk); err != nil {
			return err
		}
	}
	return nil
}

type fakeSearcher struct {
	results []search.Result
	err     error
}

func (f fakeSearcher) Search(context.Context, string) ([]search.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.results, nil
}
