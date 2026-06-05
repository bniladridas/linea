package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestCreateTemporaryMessageStreamsWithoutPersistence(t *testing.T) {
	appStore := store.NewMemoryStore()
	server := NewServer(appStore, fakeAssistant{chunks: []string{"temp ", "ok"}}, nil, testFiles(), "", Status{}).Handler()

	res := postTemporaryMessage(t, server, "hello", nil)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "event: user") || !strings.Contains(body, "event: chunk") || !strings.Contains(body, "event: done") {
		t.Fatalf("stream body missing expected events:\n%s", body)
	}
	if !strings.Contains(body, "temp ") || !strings.Contains(body, "ok") {
		t.Fatalf("stream body missing chunks:\n%s", body)
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 0 {
		t.Fatalf("conversation count = %d, want 0", len(conversations))
	}
}

func TestCreateTemporaryMessageUsesHistory(t *testing.T) {
	appStore := store.NewMemoryStore()
	assistant := &capturingAssistant{chunks: []string{"ok"}}
	server := NewServer(appStore, assistant, nil, testFiles(), "", Status{}).Handler()
	history := []map[string]string{
		{"role": "user", "content": "first"},
		{"role": "assistant", "content": "second"},
	}

	res := postTemporaryMessage(t, server, "third", history)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	if len(assistant.messages) != 3 {
		t.Fatalf("message count passed to assistant = %d, want 3", len(assistant.messages))
	}
	if assistant.messages[0].Content != "first" || assistant.messages[2].Content != "third" {
		t.Fatalf("messages passed to assistant = %#v", assistant.messages)
	}
}

func TestCreateConversationImportsMessages(t *testing.T) {
	appStore := store.NewMemoryStore()
	server := NewServer(appStore, fakeAssistant{}, nil, testFiles(), "", Status{}).Handler()
	body := strings.NewReader(`{"title":"Saved temp","messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/conversations", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var conversation store.Conversation
	if err := json.Unmarshal(res.Body.Bytes(), &conversation); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	messages, err := appStore.ListMessages(context.Background(), conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].Role != "user" || messages[1].Content != "hi" {
		t.Fatalf("imported messages = %#v", messages)
	}
}

func TestCreateConversationRejectsInvalidImportBeforePersisting(t *testing.T) {
	appStore := store.NewMemoryStore()
	server := NewServer(appStore, fakeAssistant{}, nil, testFiles(), "", Status{}).Handler()
	body := strings.NewReader(`{"title":"Bad import","messages":[{"role":"system","content":"hidden"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/conversations", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
	conversations, err := appStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 0 {
		t.Fatalf("conversation count = %d, want 0", len(conversations))
	}
}

func TestCreateConversationRollsBackImportFailure(t *testing.T) {
	baseStore := store.NewMemoryStore()
	appStore := &failingImportStore{Store: baseStore, failOnCall: 2}
	server := NewServer(appStore, fakeAssistant{}, nil, testFiles(), "", Status{}).Handler()
	body := strings.NewReader(`{"title":"Partial","messages":[{"role":"user","content":"one"},{"role":"assistant","content":"two"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/conversations", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusInternalServerError, res.Body.String())
	}
	conversations, err := baseStore.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 0 {
		t.Fatalf("conversation count = %d, want 0", len(conversations))
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

func TestCreateMessageCanCreateEditProposalFromChat(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "README.md"), "# Linea\n")
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{chunks: []string{"should not stream"}}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	res := postMessage(t, server, conversation.ID, "propose edit README.md\n```md\n# Linea\n\nChanged.\n```")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "Created an edit proposal for `README.md`.") {
		t.Fatalf("stream body missing proposal confirmation:\n%s", body)
	}
	if strings.Contains(body, "should not stream") {
		t.Fatalf("edit proposal command called assistant:\n%s", body)
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "README.md" || proposals[0].Status != "pending" {
		t.Fatalf("proposals = %#v", proposals)
	}
	if !hasDiffLine(proposals[0].Diff, "add", "Changed.") {
		t.Fatalf("proposal diff missing changed line: %#v", proposals[0].Diff)
	}
	if !hasDiffLine(proposals[0].Diff, "add", "") {
		t.Fatalf("proposal diff did not preserve trailing newline: %#v", proposals[0].Diff)
	}
	messages, err := appStore.ListMessages(context.Background(), conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[1].Role != "assistant" || messages[1].Content != "Created an edit proposal for `README.md`." {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestCreateMessageCanCreateEmptyEditProposalFromChat(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "README.md"), "# Linea\n")
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{chunks: []string{"should not stream"}}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	res := postMessage(t, server, conversation.ID, "propose edit README.md\n```md\n```")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "Created an edit proposal for `README.md`.") {
		t.Fatalf("stream body missing proposal confirmation:\n%s", res.Body.String())
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "README.md" || proposals[0].Status != "pending" {
		t.Fatalf("proposals = %#v", proposals)
	}
	for _, line := range proposals[0].Diff {
		if line.Type == "add" {
			t.Fatalf("empty proposal added line: %#v", line)
		}
	}
}

func hasDiffLine(lines []agent.DiffLine, lineType string, text string) bool {
	for _, line := range lines {
		if line.Type == lineType && line.Text == text {
			return true
		}
	}
	return false
}

func TestCreateMessageAcceptsAlternateEditProposalPhrase(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "README.md"), "# Linea\n")
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{chunks: []string{"should not stream"}}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	res := postMessage(t, server, conversation.ID, "create proposal README.md\n# Linea\n\nChanged.\n")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "should not stream") {
		t.Fatalf("edit proposal command called assistant:\n%s", res.Body.String())
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "README.md" || proposals[0].Status != "pending" {
		t.Fatalf("proposals = %#v", proposals)
	}
}

func TestCreateMessageCanStartAgentLoopFromChat(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "notes.md"), "agent loop notes\n")
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{chunks: []string{"should not stream"}}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	res := postMessage(t, server, conversation.ID, "agent search notes\nquery: notes")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	body := res.Body.String()
	if !strings.Contains(body, "Started agent loop `search notes`.") || !strings.Contains(body, "Search workspace") {
		t.Fatalf("stream body missing agent loop confirmation:\n%s", body)
	}
	if strings.Contains(body, "should not stream") {
		t.Fatalf("agent loop command called assistant:\n%s", body)
	}
	loops := runtime.ListAgentLoops(context.Background())
	if len(loops) != 1 || loops[0].Goal != "search notes" || loops[0].State != "completed" {
		t.Fatalf("loops = %#v", loops)
	}
	messages, err := appStore.ListMessages(context.Background(), conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[1].Role != "assistant" || !strings.Contains(messages[1].Content, "Started agent loop `search notes`.") {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestTemporaryMessageCanStartAgentLoopFromChat(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "notes.md"), "agent loop notes\n")
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(store.NewMemoryStore(), fakeAssistant{chunks: []string{"should not stream"}}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("content", "run agent search notes\nquery: notes"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/chat/temp", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "Started agent loop `search notes`.") {
		t.Fatalf("stream body missing agent loop confirmation:\n%s", res.Body.String())
	}
	loops := runtime.ListAgentLoops(context.Background())
	if len(loops) != 1 || loops[0].Goal != "search notes" {
		t.Fatalf("loops = %#v", loops)
	}
}

func TestCreateMessagePreservesRawUnfencedEditProposalBody(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "README.md"), "# Linea")
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{chunks: []string{"should not stream"}}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	res := postMessage(t, server, conversation.ID, "create proposal README.md\n# Linea  \n")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "README.md" || proposals[0].Status != "pending" {
		t.Fatalf("proposals = %#v", proposals)
	}
	if !hasDiffLine(proposals[0].Diff, "add", "# Linea  ") {
		t.Fatalf("proposal diff did not preserve trailing spaces: %#v", proposals[0].Diff)
	}
	if !hasDiffLine(proposals[0].Diff, "add", "") {
		t.Fatalf("proposal diff did not preserve trailing newline: %#v", proposals[0].Diff)
	}
}

func TestCreateMessagePreservesLeadingBlankLinesInUnfencedEditProposal(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "README.md"), "# Linea")
	appStore := store.NewMemoryStore()
	conversation, err := appStore.CreateConversation(context.Background(), "Test")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{chunks: []string{"should not stream"}}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	res := postMessage(t, server, conversation.ID, "create proposal README.md\n\n\n# Linea\n")

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	proposals := runtime.ListEditProposals(context.Background())
	if len(proposals) != 1 || proposals[0].Path != "README.md" || proposals[0].Status != "pending" {
		t.Fatalf("proposals = %#v", proposals)
	}
	blankAdds := 0
	for _, line := range proposals[0].Diff {
		if line.Type == "add" && line.Text == "" {
			blankAdds++
		}
	}
	if blankAdds < 2 {
		t.Fatalf("proposal diff did not preserve leading blank lines: %#v", proposals[0].Diff)
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

func TestAgentRunSummaryEndpoint(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithCommandAllowlist([]string{"make test"}))
	if _, err := runtime.CheckCommand(context.Background(), agent.CommandCheckInput{Command: "rm -rf ."}); err != nil {
		t.Fatalf("CheckCommand() error = %v", err)
	}
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/run-summary", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var summary agent.RunSummary
	if err := json.NewDecoder(res.Body).Decode(&summary); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if summary.State != "attention" || summary.CommandChecks != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestAgentRunEndpointsCreateAndListSnapshots(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithCommandAllowlist([]string{"make test"}))
	if _, err := runtime.CheckCommand(context.Background(), agent.CommandCheckInput{Command: "rm -rf ."}); err != nil {
		t.Fatalf("CheckCommand() error = %v", err)
	}
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/runs", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created store.AgentRun
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.ID == "" || created.State != "attention" {
		t.Fatalf("created = %#v", created)
	}
	var summary agent.RunSummary
	if err := json.Unmarshal(created.Summary, &summary); err != nil {
		t.Fatalf("Unmarshal(summary) error = %v", err)
	}
	if summary.CommandChecks != 1 {
		t.Fatalf("summary = %#v", summary)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agent/runs", nil)
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var runs []store.AgentRun
	if err := json.NewDecoder(res.Body).Decode(&runs); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID != created.ID {
		t.Fatalf("runs = %#v", runs)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "agent run" || traces[0].State != "attention" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentSubagentsEndpoint(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("")
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/subagents", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var subagents []agent.Subagent
	if err := json.NewDecoder(res.Body).Decode(&subagents); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(subagents) != 4 || subagents[0].ID != "review" {
		t.Fatalf("subagents = %#v", subagents)
	}
}

func TestAgentSubagentRunEndpoint(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "notes.md"), "agent notes\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/subagents/search/run", strings.NewReader(`{"query":"agent"}`))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var run agent.SubagentRun
	if err := json.NewDecoder(res.Body).Decode(&run); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if run.SubagentID != "search" || run.State != "completed" {
		t.Fatalf("run = %#v", run)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "subagent run" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentMCPServersEndpoint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeAPITestFile(t, configPath, `{"mcpServers":{"docs":{"command":"node","args":["server.js"],"env":{"TOKEN":"secret"},"tools":[{"name":"search_docs","description":"Search docs"}]}}}`)
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithMCPConfigPath(configPath))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/mcp-servers", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var servers []agent.MCPServer
	if err := json.NewDecoder(res.Body).Decode(&servers); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(servers) != 1 || servers[0].ID != "docs" || len(servers[0].EnvKeys) != 1 {
		t.Fatalf("servers = %#v", servers)
	}
}

func TestAgentMCPToolsEndpoint(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "mcp.json")
	writeAPITestFile(t, configPath, `{"mcpServers":{"docs":{"command":"node","tools":[{"name":"search_docs","description":"Search docs"}]}}}`)
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithMCPConfigPath(configPath))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/mcp-tools", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var tools []agent.MCPTool
	if err := json.NewDecoder(res.Body).Decode(&tools); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(tools) != 1 || tools[0].ID != "docs/search_docs" || tools[0].Description != "Search docs" {
		t.Fatalf("tools = %#v", tools)
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

func TestAgentHookRunEndpointsCreateAndListRuns(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("")
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	body := strings.NewReader(`{"hookId":"before_tool","state":"completed","detail":"read file"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/hook-runs", body)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created agent.HookRun
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.ID == "" || created.HookID != "before_tool" || created.State != "completed" {
		t.Fatalf("created = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agent/hook-runs", nil)
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var runs []agent.HookRun
	if err := json.NewDecoder(res.Body).Decode(&runs); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID != created.ID {
		t.Fatalf("runs = %#v", runs)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "hook run" || traces[0].Detail != "before_tool" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentHookRunEndpointRejectsUnknownHook(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("")
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/hook-runs", strings.NewReader(`{"hookId":"unknown","state":"completed"}`))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
}

func TestAgentHookRunEndpointExecutesHook(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(t.TempDir()), agent.WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), agent.CommandApprovalInput{Command: "printf ok", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/hooks/after_check/run", strings.NewReader(fmt.Sprintf(`{"command":"printf ok","approvalId":%q}`, approval.ID)))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var execution agent.HookExecution
	if err := json.NewDecoder(res.Body).Decode(&execution); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if execution.HookRun.HookID != "after_check" || execution.HookRun.State != "completed" {
		t.Fatalf("hook run = %#v", execution.HookRun)
	}
	if execution.CommandRun == nil || execution.CommandRun.Output != "ok" {
		t.Fatalf("command run = %#v", execution.CommandRun)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "hook execution" || traces[0].Detail != "after_check" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentSkillRunEndpointExecutesSkill(t *testing.T) {
	skillsDir := t.TempDir()
	writeAPITestFile(t, filepath.Join(skillsDir, "review-change.md"), "# Review change\n\nCommand: printf ok\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithSkillsDir(skillsDir), agent.WithWorkspaceRoot(t.TempDir()), agent.WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), agent.CommandApprovalInput{Command: "printf ok", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/skills/review_change/run", strings.NewReader(fmt.Sprintf(`{"approvalId":%q}`, approval.ID)))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var execution agent.SkillExecution
	if err := json.NewDecoder(res.Body).Decode(&execution); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if execution.SkillRun.SkillID != "review_change" || execution.SkillRun.State != "completed" {
		t.Fatalf("skill run = %#v", execution.SkillRun)
	}
	if execution.CommandRun == nil || execution.CommandRun.Output != "ok" {
		t.Fatalf("command run = %#v", execution.CommandRun)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agent/skill-runs", nil)
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var runs []agent.SkillRun
	if err := json.NewDecoder(res.Body).Decode(&runs); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID != execution.SkillRun.ID {
		t.Fatalf("runs = %#v", runs)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "skill execution" || traces[0].Detail != "review_change" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentLoopEndpointsCreateAndListLoops(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "notes.md"), "agent loop notes\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root), agent.WithCommandAllowlist([]string{"make test"}))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	body := strings.NewReader(`{"goal":"search agent and run tests","query":"agent","command":"make test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/loops", body)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created agent.AgentLoop
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.ID == "" || created.State != "waiting_approval" || len(created.Steps) == 0 {
		t.Fatalf("loop = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agent/loops", nil)
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var loops []agent.AgentLoop
	if err := json.NewDecoder(res.Body).Decode(&loops); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(loops) != 1 || loops[0].ID != created.ID {
		t.Fatalf("loops = %#v", loops)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "agent loop" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentLoopContinueAndCancelEndpoints(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(t.TempDir()), agent.WithCommandAllowlist([]string{"printf ok"}))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/loops", strings.NewReader(`{"goal":"run command","command":"printf ok"}`))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var loop agent.AgentLoop
	if err := json.NewDecoder(res.Body).Decode(&loop); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if loop.State != "waiting_approval" {
		t.Fatalf("loop = %#v", loop)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/agent/command-approvals", strings.NewReader(`{"command":"printf ok","state":"approved"}`))
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("approval status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/agent/loops/"+loop.ID+"/continue", strings.NewReader(`{}`))
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("continue status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var continued agent.AgentLoop
	if err := json.NewDecoder(res.Body).Decode(&continued); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if continued.State != "completed" || !apiLoopHasStep(continued, "command_run", "completed") {
		t.Fatalf("continued = %#v", continued)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/agent/loops", strings.NewReader(`{"goal":"edit file"}`))
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var editLoop agent.AgentLoop
	if err := json.NewDecoder(res.Body).Decode(&editLoop); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/agent/loops/"+editLoop.ID+"/cancel", nil)
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var canceled agent.AgentLoop
	if err := json.NewDecoder(res.Body).Decode(&canceled); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if canceled.State != "canceled" || canceled.Summary != "Canceled." {
		t.Fatalf("canceled = %#v", canceled)
	}
}

func TestAgentCommandCheckEndpointsCreateAndListChecks(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithCommandAllowlist([]string{"make test"}))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	body := strings.NewReader(`{"command":"make test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/command-checks", body)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created agent.CommandCheck
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.ID == "" || !created.Allowed || created.Command != "make test" {
		t.Fatalf("created = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agent/command-checks", nil)
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var checks []agent.CommandCheck
	if err := json.NewDecoder(res.Body).Decode(&checks); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(checks) != 1 || checks[0].ID != created.ID {
		t.Fatalf("checks = %#v", checks)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "command check" || traces[0].State != "allowed" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentCommandApprovalEndpointsCreateAndListApprovals(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithCommandAllowlist([]string{"make test"}))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	body := strings.NewReader(`{"command":"make test","state":"approved","detail":"before commit"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/command-approvals", body)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created agent.CommandApproval
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.ID == "" || created.Command != "make test" || created.State != "approved" {
		t.Fatalf("created = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agent/command-approvals", nil)
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var approvals []agent.CommandApproval
	if err := json.NewDecoder(res.Body).Decode(&approvals); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(approvals) != 1 || approvals[0].ID != created.ID {
		t.Fatalf("approvals = %#v", approvals)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "command approval" || traces[0].State != "approved" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentCommandCheckEndpointBlocksUnlistedCommand(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithCommandAllowlist([]string{"make test"}))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/command-checks", strings.NewReader(`{"command":"rm -rf ."}`))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created agent.CommandCheck
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.Allowed || created.Reason != "not in allowlist" {
		t.Fatalf("created = %#v", created)
	}
}

func TestAgentCommandRunEndpointsCreateAndListRuns(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(t.TempDir()), agent.WithCommandAllowlist([]string{"printf ok"}))
	approval, err := runtime.AddCommandApproval(context.Background(), agent.CommandApprovalInput{Command: "printf ok", State: "approved"})
	if err != nil {
		t.Fatalf("AddCommandApproval() error = %v", err)
	}
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	body := strings.NewReader(fmt.Sprintf(`{"command":"printf ok","approvalId":%q}`, approval.ID))
	req := httptest.NewRequest(http.MethodPost, "/api/agent/command-runs", body)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var created agent.CommandRun
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if created.ID == "" || created.Command != "printf ok" || created.ExitCode != 0 || created.Output != "ok" {
		t.Fatalf("created = %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agent/command-runs", nil)
	res = httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var runs []agent.CommandRun
	if err := json.NewDecoder(res.Body).Decode(&runs); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(runs) != 1 || runs[0].ID != created.ID {
		t.Fatalf("runs = %#v", runs)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "command run" || traces[0].State != "completed" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentCommandRunEndpointRejectsMissingApproval(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(t.TempDir()), agent.WithCommandAllowlist([]string{"printf ok"}))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/command-runs", strings.NewReader(`{"command":"printf ok"}`))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusBadRequest, res.Body.String())
	}
}

func TestAgentCommandRunEndpointRejectsBlockedCommand(t *testing.T) {
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(t.TempDir()), agent.WithCommandAllowlist([]string{"printf ok"}))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/command-runs", strings.NewReader(`{"command":"printf no"}`))
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

func TestAgentWorkspaceDiagnosticsEndpoint(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "broken.go"), "package main\nfunc main( {\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/workspace/diagnostics", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var diagnostics []agent.Diagnostic
	if err := json.NewDecoder(res.Body).Decode(&diagnostics); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(diagnostics) == 0 || diagnostics[0].Path != "broken.go" || diagnostics[0].Severity != "error" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "read diagnostics" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentWorkspaceSymbolsEndpoint(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "main.go"), "package main\n\ntype App struct{}\nfunc Run() {}\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/workspace/symbols?q=run", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var symbols []agent.WorkspaceSymbol
	if err := json.NewDecoder(res.Body).Decode(&symbols); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(symbols) != 1 || symbols[0].Name != "Run" || symbols[0].Path != "main.go" {
		t.Fatalf("symbols = %#v", symbols)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "read symbols" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentWorkspaceReferencesEndpoint(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "main.go"), "package main\n\nfunc Run() {}\nfunc main() { Run() }\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/workspace/references?q=Run", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var references []agent.WorkspaceReference
	if err := json.NewDecoder(res.Body).Decode(&references); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(references) != 2 || references[0].Path != "main.go" || references[1].Path != "main.go" {
		t.Fatalf("references = %#v", references)
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "read references" {
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

func TestAgentEditProposalEndpointCreatesDiffWithoutWriting(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "notes.md")
	writeAPITestFile(t, filePath, "one\ntwo\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	body := strings.NewReader(`{"path":"notes.md","content":"one\nthree\n","summary":"change second line"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/edit-proposals", body)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusCreated, res.Body.String())
	}
	var proposal agent.EditProposal
	if err := json.NewDecoder(res.Body).Decode(&proposal); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if proposal.ID == "" || proposal.Path != "notes.md" || proposal.Status != "pending" || len(proposal.Diff) == 0 {
		t.Fatalf("proposal = %#v", proposal)
	}
	current, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(current) != "one\ntwo\n" {
		t.Fatalf("file was changed: %q", string(current))
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "propose edit" || traces[0].State != "pending" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentEditProposalListEndpoint(t *testing.T) {
	root := t.TempDir()
	writeAPITestFile(t, filepath.Join(root, "notes.md"), "one\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	if _, err := runtime.ProposeEdit(context.Background(), agent.EditProposalInput{Path: "notes.md", Content: "two\n"}); err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/agent/edit-proposals", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var proposals []agent.EditProposal
	if err := json.NewDecoder(res.Body).Decode(&proposals); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(proposals) != 1 || proposals[0].Path != "notes.md" {
		t.Fatalf("proposals = %#v", proposals)
	}
}

func TestAgentEditProposalReviewEndpoint(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "notes.md")
	writeAPITestFile(t, filePath, "one\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	proposal, err := runtime.ProposeEdit(context.Background(), agent.EditProposalInput{Path: "notes.md", Content: "two\n"})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPatch, "/api/agent/edit-proposals/"+proposal.ID, strings.NewReader(`{"status":"approved","detail":"reviewed"}`))
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var reviewed agent.EditProposal
	if err := json.NewDecoder(res.Body).Decode(&reviewed); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if reviewed.Status != "approved" || reviewed.ReviewDetail != "reviewed" || reviewed.ReviewedAt == nil {
		t.Fatalf("reviewed = %#v", reviewed)
	}
	current, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(current) != "one\n" {
		t.Fatalf("file was changed: %q", string(current))
	}
	traces := runtime.ListTraces(context.Background())
	if len(traces) != 1 || traces[0].Event != "review edit" || traces[0].State != "approved" {
		t.Fatalf("traces = %#v", traces)
	}
}

func TestAgentEditProposalApplyEndpointWritesApprovedProposal(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "notes.md")
	writeAPITestFile(t, filePath, "one\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(root))
	proposal, err := runtime.ProposeEdit(context.Background(), agent.EditProposalInput{Path: "notes.md", Content: "two\n"})
	if err != nil {
		t.Fatalf("ProposeEdit() error = %v", err)
	}
	if _, err := runtime.ReviewEditProposal(context.Background(), proposal.ID, agent.EditProposalReviewInput{Status: "approved"}); err != nil {
		t.Fatalf("ReviewEditProposal() error = %v", err)
	}
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/agent/edit-proposals/"+proposal.ID+"/apply", nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var applied agent.EditProposal
	if err := json.NewDecoder(res.Body).Decode(&applied); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if applied.Status != "applied" {
		t.Fatalf("applied = %#v", applied)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(content) != "two\n" {
		t.Fatalf("file content = %q", string(content))
	}
}

func TestAgentWorkspaceUpdateEndpointChangesRoot(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	writeAPITestFile(t, filepath.Join(secondRoot, "notes.md"), "agent\n")
	appStore := store.NewMemoryStore()
	runtime := agent.NewRuntime("", agent.WithWorkspaceRoot(firstRoot))
	server := NewServerWithAgentRuntime(appStore, fakeAssistant{}, nil, testFiles(), "", func(context.Context) Status {
		return Status{}
	}, nil, runtime).Handler()

	body := strings.NewReader(fmt.Sprintf(`{"root":%q}`, secondRoot))
	req := httptest.NewRequest(http.MethodPatch, "/api/agent/workspace", body)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	results, err := runtime.SearchFiles(context.Background(), "agent")
	if err != nil {
		t.Fatalf("SearchFiles() error = %v", err)
	}
	if len(results) != 1 || results[0].Path != "notes.md" {
		t.Fatalf("results = %#v", results)
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

func postTemporaryMessage(t *testing.T, handler http.Handler, content string, history any) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("content", content); err != nil {
		t.Fatalf("WriteField(content) error = %v", err)
	}
	if history != nil {
		payload, err := json.Marshal(history)
		if err != nil {
			t.Fatalf("Marshal(history) error = %v", err)
		}
		if err := writer.WriteField("history", string(payload)); err != nil {
			t.Fatalf("WriteField(history) error = %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/chat/temp", &body)
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

type failingImportStore struct {
	store.Store
	addCalls   int
	failOnCall int
}

func (s *failingImportStore) AddMessage(ctx context.Context, conversationID, role, content string) (store.Message, error) {
	s.addCalls++
	if s.addCalls == s.failOnCall {
		return store.Message{}, errors.New("forced add failure")
	}
	return s.Store.AddMessage(ctx, conversationID, role, content)
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
	messages      []store.Message
	searchResults []llm.SearchResult
}

func (f *capturingAssistant) GenerateStream(_ context.Context, messages []store.Message, _ []llm.Attachment, searchResults []llm.SearchResult, onChunk func(string) error) error {
	f.messages = messages
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

func apiLoopHasStep(loop agent.AgentLoop, kind string, state string) bool {
	for _, step := range loop.Steps {
		if step.Kind == kind && step.State == state {
			return true
		}
	}
	return false
}
