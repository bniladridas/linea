package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type Conversation struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversationId"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"createdAt"`
}

type AgentRun struct {
	ID        string          `json:"id"`
	State     string          `json:"state"`
	Summary   json.RawMessage `json:"summary"`
	CreatedAt time.Time       `json:"createdAt"`
}

type Store interface {
	ListConversations(context.Context) ([]Conversation, error)
	CreateConversation(context.Context, string) (Conversation, error)
	UpdateConversationTitle(context.Context, string, string) (Conversation, error)
	DeleteConversation(context.Context, string) error
	ListMessages(context.Context, string) ([]Message, error)
	AddMessage(context.Context, string, string, string) (Message, error)
	ListAgentRuns(context.Context) ([]AgentRun, error)
	AddAgentRun(context.Context, string, json.RawMessage) (AgentRun, error)
	Close() error
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", "")
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:], b[10:])
	return string(dst)
}

func TitleFromMessage(content string) string {
	title := strings.Join(strings.Fields(content), " ")
	if title == "" {
		return "Untitled"
	}
	runes := []rune(title)
	if len(runes) > 60 {
		return strings.TrimSpace(string(runes[:57])) + "..."
	}
	return title
}

func displayTitle(title string, firstUserMessage *string) string {
	if strings.TrimSpace(title) != "Untitled" || firstUserMessage == nil {
		return title
	}
	return TitleFromMessage(*firstUserMessage)
}

func nextTimestamp(after time.Time) time.Time {
	now := time.Now().UTC()
	if now.After(after) {
		return now
	}
	return after.Add(time.Nanosecond)
}

type MemoryStore struct {
	mu            sync.RWMutex
	conversations map[string]Conversation
	messages      map[string][]Message
	agentRuns     []AgentRun
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		conversations: map[string]Conversation{},
		messages:      map[string][]Message{},
	}
}

func (s *MemoryStore) ListConversations(context.Context) ([]Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Conversation, 0, len(s.conversations))
	for _, conversation := range s.conversations {
		for _, message := range s.messages[conversation.ID] {
			if message.Role == "user" {
				conversation.Title = displayTitle(conversation.Title, &message.Content)
				break
			}
		}
		items = append(items, conversation)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}

func (s *MemoryStore) CreateConversation(_ context.Context, title string) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	conversation := Conversation{ID: NewID(), Title: title, CreatedAt: now, UpdatedAt: now}
	s.conversations[conversation.ID] = conversation
	return conversation, nil
}

func (s *MemoryStore) UpdateConversationTitle(_ context.Context, conversationID, title string) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return Conversation{}, ErrNotFound
	}
	conversation.Title = title
	conversation.UpdatedAt = nextTimestamp(conversation.UpdatedAt)
	s.conversations[conversationID] = conversation
	return conversation, nil
}

func (s *MemoryStore) DeleteConversation(_ context.Context, conversationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.conversations[conversationID]; !ok {
		return ErrNotFound
	}
	delete(s.conversations, conversationID)
	delete(s.messages, conversationID)
	return nil
}

func (s *MemoryStore) ListMessages(_ context.Context, conversationID string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.conversations[conversationID]; !ok {
		return nil, ErrNotFound
	}
	messages := append([]Message(nil), s.messages[conversationID]...)
	if messages == nil {
		messages = []Message{}
	}
	return messages, nil
}

func (s *MemoryStore) AddMessage(_ context.Context, conversationID, role, content string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return Message{}, ErrNotFound
	}
	now := nextTimestamp(conversation.UpdatedAt)
	message := Message{ID: NewID(), ConversationID: conversationID, Role: role, Content: content, CreatedAt: now}
	s.messages[conversationID] = append(s.messages[conversationID], message)
	conversation.UpdatedAt = now
	s.conversations[conversationID] = conversation
	return message, nil
}

func (s *MemoryStore) ListAgentRuns(context.Context) ([]AgentRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]AgentRun(nil), s.agentRuns...), nil
}

func (s *MemoryStore) Close() error { return nil }

func (s *MemoryStore) AddAgentRun(_ context.Context, state string, summary json.RawMessage) (AgentRun, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		state = "ready"
	}
	if len(summary) == 0 {
		summary = json.RawMessage(`{}`)
	}
	run := AgentRun{
		ID:        NewID(),
		State:     state,
		Summary:   append(json.RawMessage(nil), summary...),
		CreatedAt: time.Now().UTC(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentRuns = append([]AgentRun{run}, s.agentRuns...)
	if len(s.agentRuns) > 50 {
		s.agentRuns = s.agentRuns[:50]
	}
	return run, nil
}
