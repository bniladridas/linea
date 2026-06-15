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

type contextKey string

const contextKeyUserID contextKey = "userID"

const contextKeyConversationID contextKey = "conversationID"

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, contextKeyUserID, userID)
}

func UserIDFromContext(ctx context.Context) string {
	userID, _ := ctx.Value(contextKeyUserID).(string)
	return userID
}

func WithConversationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKeyConversationID, id)
}

func ConversationIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKeyConversationID).(string)
	return id
}

var ErrNotFound = errors.New("not found")
var ErrEmailExists = errors.New("email already exists")
var ErrSessionNotFound = errors.New("session not found")

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Conversation struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId,omitempty"`
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

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type OAuthToken struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	AccountName  string    `json:"accountName"`
	AccountID    string    `json:"accountId"`
	AccessToken  []byte    `json:"accessToken"`
	RefreshToken []byte    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
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
	ListUsers(context.Context) ([]User, error)
	CreateUser(context.Context, string, string) (User, error)
	GetUserByEmail(context.Context, string) (User, error)
	GetUserByID(context.Context, string) (User, error)
	SaveOAuthToken(context.Context, OAuthToken) error
	GetOAuthToken(context.Context, string) (OAuthToken, error)
	ListOAuthTokens(context.Context) ([]OAuthToken, error)
	DeleteOAuthToken(context.Context, string) error
	CreateSession(context.Context, string) (Session, error)
	GetSessionByToken(context.Context, string) (Session, error)
	DeleteSession(context.Context, string) error
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
	users         map[string]User
	oauthTokens   map[string]OAuthToken
	sessions      map[string]Session
	sessionByToken map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		conversations:  map[string]Conversation{},
		messages:       map[string][]Message{},
		users:          map[string]User{},
		oauthTokens:    map[string]OAuthToken{},
		sessions:       map[string]Session{},
		sessionByToken: map[string]string{},
	}
}

func (s *MemoryStore) ListConversations(ctx context.Context) ([]Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	userID := UserIDFromContext(ctx)
	items := make([]Conversation, 0, len(s.conversations))
	for _, conversation := range s.conversations {
		if userID != "" && conversation.UserID != "" && conversation.UserID != userID {
			continue
		}
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

func (s *MemoryStore) CreateConversation(ctx context.Context, title string) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := NewID()
	if override := ConversationIDFromContext(ctx); override != "" {
		id = override
	}
	now := time.Now().UTC()
	conversation := Conversation{ID: id, Title: title, CreatedAt: now, UpdatedAt: now, UserID: UserIDFromContext(ctx)}
	s.conversations[conversation.ID] = conversation
	return conversation, nil
}

func (s *MemoryStore) UpdateConversationTitle(ctx context.Context, conversationID, title string) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return Conversation{}, ErrNotFound
	}
	if userID := UserIDFromContext(ctx); userID != "" && conversation.UserID != "" && conversation.UserID != userID {
		return Conversation{}, ErrNotFound
	}
	conversation.Title = title
	conversation.UpdatedAt = nextTimestamp(conversation.UpdatedAt)
	s.conversations[conversationID] = conversation
	return conversation, nil
}

func (s *MemoryStore) DeleteConversation(ctx context.Context, conversationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return ErrNotFound
	}
	if userID := UserIDFromContext(ctx); userID != "" && conversation.UserID != "" && conversation.UserID != userID {
		return ErrNotFound
	}
	delete(s.conversations, conversationID)
	delete(s.messages, conversationID)
	return nil
}

func (s *MemoryStore) ListMessages(ctx context.Context, conversationID string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return nil, ErrNotFound
	}
	if userID := UserIDFromContext(ctx); userID != "" && conversation.UserID != "" && conversation.UserID != userID {
		return nil, ErrNotFound
	}
	messages := append([]Message(nil), s.messages[conversationID]...)
	if messages == nil {
		messages = []Message{}
	}
	return messages, nil
}

func (s *MemoryStore) AddMessage(ctx context.Context, conversationID, role, content string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conversation, ok := s.conversations[conversationID]
	if !ok {
		return Message{}, ErrNotFound
	}
	if userID := UserIDFromContext(ctx); userID != "" && conversation.UserID != "" && conversation.UserID != userID {
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

func (s *MemoryStore) ListUsers(context.Context) ([]User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]User, 0, len(s.users))
	for _, u := range s.users {
		items = append(items, u)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

func (s *MemoryStore) CreateUser(_ context.Context, email, name string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Email == email {
			return User{}, ErrEmailExists
		}
	}
	now := time.Now().UTC()
	user := User{ID: NewID(), Email: email, Name: name, CreatedAt: now, UpdatedAt: now}
	s.users[user.ID] = user
	return user, nil
}

func (s *MemoryStore) GetUserByID(_ context.Context, id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (s *MemoryStore) GetUserByEmail(_ context.Context, email string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, u := range s.users {
		if u.Email == email {
			return u, nil
		}
	}
	return User{}, ErrNotFound
}

func (s *MemoryStore) CreateSession(_ context.Context, userID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	token := NewID()
	session := Session{
		ID:        NewID(),
		UserID:    userID,
		Token:     token,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * 24 * time.Hour),
	}
	s.sessions[session.ID] = session
	s.sessionByToken[token] = session.ID
	return session, nil
}

func (s *MemoryStore) GetSessionByToken(_ context.Context, token string) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.sessionByToken[token]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	session, ok := s.sessions[id]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		return Session{}, ErrSessionNotFound
	}
	return session, nil
}

func (s *MemoryStore) DeleteSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	delete(s.sessions, id)
	delete(s.sessionByToken, session.Token)
	return nil
}

func (s *MemoryStore) SaveOAuthToken(_ context.Context, token OAuthToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token.ID == "" {
		token.ID = NewID()
	}
	token.UpdatedAt = time.Now().UTC()
	if _, exists := s.oauthTokens[token.ID]; !exists {
		token.CreatedAt = token.UpdatedAt
	}
	s.oauthTokens[token.ID] = token
	return nil
}

func (s *MemoryStore) GetOAuthToken(_ context.Context, id string) (OAuthToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.oauthTokens[id]
	if !ok {
		return OAuthToken{}, ErrNotFound
	}
	return token, nil
}

func (s *MemoryStore) ListOAuthTokens(_ context.Context) ([]OAuthToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tokens := make([]OAuthToken, 0, len(s.oauthTokens))
	for _, token := range s.oauthTokens {
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func (s *MemoryStore) DeleteOAuthToken(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.oauthTokens[id]; !ok {
		return ErrNotFound
	}
	delete(s.oauthTokens, id)
	return nil
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
