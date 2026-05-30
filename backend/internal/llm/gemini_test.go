package llm

import (
	"strings"
	"testing"

	"linea/backend/internal/store"
)

func TestBuildPromptIncludesConversationAttachmentsAndSearch(t *testing.T) {
	prompt := buildPrompt(
		[]store.Message{{Role: "user", Content: "Summarize this"}},
		[]Attachment{{Name: "notes.txt", Content: "attached notes"}},
		[]SearchResult{{Title: "Source", URL: "https://example.com", Snippet: "search snippet"}},
	)

	for _, want := range []string{
		"USER: Summarize this",
		"Answer the latest user message directly",
		"notes.txt",
		"attached notes",
		"Source (https://example.com): search snippet",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
