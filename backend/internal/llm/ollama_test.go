package llm

import (
	"strings"
	"testing"

	"linea/backend/internal/store"
)

func TestBuildOllamaMessagesUsesSystemAndConversationRoles(t *testing.T) {
	messages := buildOllamaMessages(
		[]store.Message{
			{Role: "user", Content: "What did I ask?"},
			{Role: "assistant", Content: "A question."},
			{Role: "user", Content: "Reply with only ok."},
		},
		[]Attachment{{Name: "notes.txt", Content: "attached notes"}},
		[]SearchResult{{Title: "Source", URL: "https://example.com", Snippet: "snippet"}},
	)

	if len(messages) != 5 {
		t.Fatalf("message count = %d, want 5", len(messages))
	}
	if messages[0].Role != "system" || !strings.Contains(messages[0].Content, "Answer the latest user message directly") {
		t.Fatalf("system message = %#v", messages[0])
	}
	if !strings.Contains(messages[0].Content, "output the requested result immediately") || !strings.Contains(messages[0].Content, "Do not respond with only an acknowledgement or a sure/certainly/of course preface") {
		t.Fatalf("system message does not block acknowledgement-only replies: %#v", messages[0])
	}
	if messages[1].Role != "system" || !strings.Contains(messages[1].Content, "attached notes") || !strings.Contains(messages[1].Content, "https://example.com") {
		t.Fatalf("context message = %#v", messages[1])
	}
	if messages[2].Role != "user" || messages[3].Role != "assistant" || messages[4].Role != "user" {
		t.Fatalf("conversation roles = %q, %q, %q", messages[2].Role, messages[3].Role, messages[4].Role)
	}
	if messages[4].Content != "Reply with only ok." {
		t.Fatalf("latest user content = %q", messages[4].Content)
	}
}

func TestBuildOllamaMessagesAddsFollowUpTaskHint(t *testing.T) {
	messages := buildOllamaMessages(
		[]store.Message{
			{Role: "user", Content: "Can you write a sentence for school?"},
			{Role: "assistant", Content: "Certainly"},
			{Role: "user", Content: "Do it please?"},
		},
		nil,
		nil,
	)

	if len(messages) != 5 {
		t.Fatalf("message count = %d, want 5", len(messages))
	}
	hint := messages[4]
	if hint.Role != "system" || !strings.Contains(hint.Content, "Can you write a sentence for school?") {
		t.Fatalf("follow-up hint = %#v", hint)
	}
}

func TestCleanOllamaResponseExtractsWritingResult(t *testing.T) {
	cleaned := cleanOllamaResponse(
		`Certainly! Here's a simple sentence for school: "The sun sets over the ocean."`,
		[]store.Message{{Role: "user", Content: "Can you write a sentence for school?"}},
	)

	if cleaned != "The sun sets over the ocean." {
		t.Fatalf("cleaned response = %q", cleaned)
	}
}

func TestCleanOllamaResponseUsesPreviousRequestForFollowUp(t *testing.T) {
	cleaned := cleanOllamaResponse(
		`Of course! Here's the sentence: "Today is our first day of class."`,
		[]store.Message{
			{Role: "user", Content: "Can you write a sentence for school?"},
			{Role: "assistant", Content: "Certainly"},
			{Role: "user", Content: "Do it please?"},
		},
	)

	if cleaned != "Today is our first day of class." {
		t.Fatalf("cleaned response = %q", cleaned)
	}
}

func TestCleanOllamaResponseStripsAcknowledgementPrefix(t *testing.T) {
	cleaned := cleanOllamaResponse(
		"Sure! The build passed.",
		[]store.Message{{Role: "user", Content: "Did the build pass?"}},
	)

	if cleaned != "The build passed." {
		t.Fatalf("cleaned response = %q", cleaned)
	}
}
