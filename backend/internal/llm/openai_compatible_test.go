package llm

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linea/backend/internal/store"
)

func TestOpenAICompatibleClientStreamsChatCompletions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAICompatibleClient("test", server.URL+"/v1", "key", "model")
	var got string
	err := client.GenerateStream(t.Context(), []store.Message{{Role: "user", Content: "hello"}}, nil, nil, func(chunk string) error {
		got += chunk
		return nil
	})
	if err != nil {
		t.Fatalf("GenerateStream() error = %v", err)
	}
	if got != "hello" {
		t.Fatalf("streamed content = %q, want hello", got)
	}
}

func TestOpenAICompatibleClientQuotaErrorCanFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "quota", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewOpenAICompatibleClient("test", server.URL+"/v1", "key", "model")
	err := client.GenerateStream(t.Context(), nil, nil, nil, nil)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("GenerateStream() error = %v, want ErrQuotaExceeded", err)
	}
}

func TestBuildOpenAIMessagesIncludesSystemContextAndConversation(t *testing.T) {
	messages := buildOpenAIMessages(
		[]store.Message{{Role: "user", Content: "Summarize this"}},
		[]Attachment{{Name: "notes.txt", Content: "attached notes"}},
		[]SearchResult{{Title: "Source", URL: "https://example.com", Snippet: "search snippet"}},
	)

	if len(messages) < 3 {
		t.Fatalf("message count = %d, want at least 3", len(messages))
	}
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, "You are Linea") {
		t.Fatalf("first message = %#v, want Linea system prompt", messages[0])
	}
	if !strings.Contains(messages[1].Content, "notes.txt") || !strings.Contains(messages[1].Content, "Source") {
		t.Fatalf("context message missing attachment/search context: %#v", messages[1])
	}
	if messages[len(messages)-1].Role != "user" || messages[len(messages)-1].Content != "Summarize this" {
		t.Fatalf("last message = %#v, want user request", messages[len(messages)-1])
	}
}
