package store

import (
	"context"
	"testing"
)

func TestMemoryStoreConversationLifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	conversation, err := s.CreateConversation(ctx, "Planning")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if conversation.ID == "" {
		t.Fatal("CreateConversation() returned empty ID")
	}

	userMessage, err := s.AddMessage(ctx, conversation.ID, "user", "hello")
	if err != nil {
		t.Fatalf("AddMessage(user) error = %v", err)
	}
	assistantMessage, err := s.AddMessage(ctx, conversation.ID, "assistant", "hi")
	if err != nil {
		t.Fatalf("AddMessage(assistant) error = %v", err)
	}

	messages, err := s.ListMessages(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("ListMessages() length = %d, want 2", len(messages))
	}
	if messages[0].ID != userMessage.ID || messages[1].ID != assistantMessage.ID {
		t.Fatalf("ListMessages() did not preserve insertion order")
	}

	conversations, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 1 || conversations[0].ID != conversation.ID {
		t.Fatalf("ListConversations() = %#v, want created conversation", conversations)
	}
	if !conversations[0].UpdatedAt.After(conversation.UpdatedAt) {
		t.Fatalf("conversation UpdatedAt was not advanced after messages")
	}
}

func TestMemoryStoreDisplaysTitleFromFirstUserMessage(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	conversation, err := s.CreateConversation(ctx, "Untitled")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if _, err := s.AddMessage(ctx, conversation.ID, "user", "write a simple program in go"); err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}
	if _, err := s.AddMessage(ctx, conversation.ID, "user", "write a simple program in rust"); err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}

	conversations, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if got := conversations[0].Title; got != "write a simple program in go" {
		t.Fatalf("conversation title = %q", got)
	}
}

func TestMemoryStoreUpdateConversationTitle(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	conversation, err := s.CreateConversation(ctx, "Old")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	updated, err := s.UpdateConversationTitle(ctx, conversation.ID, "New")
	if err != nil {
		t.Fatalf("UpdateConversationTitle() error = %v", err)
	}
	if updated.Title != "New" {
		t.Fatalf("updated title = %q", updated.Title)
	}
	if !updated.UpdatedAt.After(conversation.UpdatedAt) {
		t.Fatalf("updated timestamp was not advanced")
	}
}

func TestMemoryStoreEmptyConversationMessages(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	conversation, err := s.CreateConversation(ctx, "Empty")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	messages, err := s.ListMessages(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if messages == nil {
		t.Fatal("ListMessages() returned nil, want empty slice")
	}
	if len(messages) != 0 {
		t.Fatalf("ListMessages() length = %d, want 0", len(messages))
	}
}

func TestMemoryStoreDeleteConversation(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	conversation, err := s.CreateConversation(ctx, "Delete")
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if _, err := s.AddMessage(ctx, conversation.ID, "user", "hello"); err != nil {
		t.Fatalf("AddMessage() error = %v", err)
	}

	if err := s.DeleteConversation(ctx, conversation.ID); err != nil {
		t.Fatalf("DeleteConversation() error = %v", err)
	}
	if _, err := s.ListMessages(ctx, conversation.ID); err != ErrNotFound {
		t.Fatalf("ListMessages() error = %v, want ErrNotFound", err)
	}
	conversations, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 0 {
		t.Fatalf("ListConversations() length = %d, want 0", len(conversations))
	}
}

func TestMemoryStoreMissingConversation(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	if _, err := s.ListMessages(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("ListMessages() error = %v, want ErrNotFound", err)
	}
	if _, err := s.AddMessage(ctx, "missing", "user", "hello"); err != ErrNotFound {
		t.Fatalf("AddMessage() error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteConversation(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("DeleteConversation() error = %v, want ErrNotFound", err)
	}
}

func TestTitleFromMessage(t *testing.T) {
	if got := TitleFromMessage("  hello   linea  "); got != "hello linea" {
		t.Fatalf("TitleFromMessage() = %q", got)
	}
	if got := TitleFromMessage(""); got != "Untitled" {
		t.Fatalf("TitleFromMessage(empty) = %q", got)
	}
}
