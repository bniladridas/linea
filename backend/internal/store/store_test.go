package store

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMemoryStoreAgentRuns(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	first, err := s.AddAgentRun(ctx, "ready", json.RawMessage(`{"state":"ready"}`))
	if err != nil {
		t.Fatalf("AddAgentRun(first) error = %v", err)
	}
	second, err := s.AddAgentRun(ctx, "attention", json.RawMessage(`{"state":"attention","commandChecks":1}`))
	if err != nil {
		t.Fatalf("AddAgentRun(second) error = %v", err)
	}

	runs, err := s.ListAgentRuns(ctx)
	if err != nil {
		t.Fatalf("ListAgentRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("ListAgentRuns() length = %d, want 2", len(runs))
	}
	if runs[0].ID != second.ID || runs[1].ID != first.ID {
		t.Fatalf("ListAgentRuns() order = %#v", runs)
	}
	if string(runs[0].Summary) != `{"state":"attention","commandChecks":1}` {
		t.Fatalf("summary = %s", runs[0].Summary)
	}
}

func TestMemoryStoreUsers(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	u1, err := s.CreateUser(ctx, "alice@example.com", "Alice")
	if err != nil {
		t.Fatalf("CreateUser(alice) error = %v", err)
	}
	if u1.Email != "alice@example.com" || u1.Name != "Alice" {
		t.Fatalf("unexpected user: %+v", u1)
	}

	u2, err := s.CreateUser(ctx, "bob@example.com", "Bob")
	if err != nil {
		t.Fatalf("CreateUser(bob) error = %v", err)
	}
	if u2.Email != "bob@example.com" {
		t.Fatalf("unexpected email for u2: %q", u2.Email)
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListUsers() length = %d, want 2", len(users))
	}

	// Test duplicate email
	if _, err := s.CreateUser(ctx, "alice@example.com", "Alice 2"); err == nil || err.Error() != "email already exists" {
		t.Fatalf("CreateUser(duplicate) error = %v, want 'email already exists'", err)
	}
}

func TestSyncStoreConversationIDMatchesBothSides(t *testing.T) {
	ctx := context.Background()
	local := NewMemoryStore()

	remoteServer := newTestSyncServer()
	defer remoteServer.Close()
	remote, err := NewRemoteStore(remoteServer.URL(), "")
	if err != nil {
		t.Fatal(err)
	}

	s := NewSyncStore(local, remote)
	defer s.Close()

	c, err := s.CreateConversation(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}

	lc, err := local.ListConversations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(lc) != 1 || lc[0].ID != c.ID {
		t.Fatalf("local conversation ID = %q, want %q", lc[0].ID, c.ID)
	}

	m, err := s.AddMessage(ctx, c.ID, "user", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if m.ConversationID != c.ID {
		t.Fatalf("message ConversationID = %q, want %q", m.ConversationID, c.ID)
	}

	msgs, err := s.ListMessages(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != m.ID {
		t.Fatalf("ListMessages returned %#v", msgs)
	}
}

func TestSyncStoreReadsFromLocal(t *testing.T) {
	ctx := context.Background()
	local := NewMemoryStore()
	localC, _ := local.CreateConversation(ctx, "local-only")
	local.AddMessage(ctx, localC.ID, "user", "data")

	remoteServer := newTestSyncServer()
	defer remoteServer.Close()
	remote, err := NewRemoteStore(remoteServer.URL(), "")
	if err != nil {
		t.Fatal(err)
	}

	s := NewSyncStore(local, remote)
	defer s.Close()

	convs, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 1 || convs[0].ID != localC.ID {
		t.Fatalf("expected local conversation, got %#v", convs)
	}

	msgs, err := s.ListMessages(ctx, localC.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestSyncStoreDataReachesRemote(t *testing.T) {
	ctx := context.Background()
	local := NewMemoryStore()

	rs := newTestSyncServer()
	defer rs.Close()
	remote, err := NewRemoteStore(rs.URL(), "")
	if err != nil {
		t.Fatal(err)
	}

	s := NewSyncStore(local, remote)
	defer s.Close()

	c, err := s.CreateConversation(ctx, "e2e-test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.AddMessage(ctx, c.ID, "user", "hello from sync")
	if err != nil {
		t.Fatal(err)
	}

	rconvs, err := rs.store.ListConversations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rconvs) != 1 || rconvs[0].ID != c.ID {
		t.Fatalf("remote conversations = %#v, want conversation %q", rconvs, c.ID)
	}
	if rconvs[0].Title != "e2e-test" {
		t.Fatalf("remote conversation title = %q, want %q", rconvs[0].Title, "e2e-test")
	}

	rm, err := rs.store.ListMessages(ctx, c.ID)
	if err != nil {
		t.Fatalf("remote should have messages for conversation %q: %v", c.ID, err)
	}
	if len(rm) != 1 || rm[0].Content != "hello from sync" || rm[0].ConversationID != c.ID {
		t.Fatalf("remote messages = %#v", rm)
	}
}

func TestSyncStoreRemoteFailureIsolated(t *testing.T) {
	ctx := context.Background()
	local := NewMemoryStore()

	remoteServer := newTestSyncServer()
	remoteServer.Close()
	remote, err := NewRemoteStore(remoteServer.URL(), "")
	if err != nil {
		t.Fatal(err)
	}

	s := NewSyncStore(local, remote)
	defer s.Close()

	c, err := s.CreateConversation(ctx, "test")
	if err != nil {
		t.Fatalf("CreateConversation should succeed despite remote failure: %v", err)
	}
	if c.Title != "test" {
		t.Fatalf("unexpected conversation: %#v", c)
	}

	convs, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 1 {
		t.Fatalf("expected 1 local conversation, got %d", len(convs))
	}
}

type remoteSyncStore struct {
	store  *MemoryStore
	server *httptest.Server
}

func newTestSyncServer() *remoteSyncStore {
	rs := &remoteSyncStore{store: NewMemoryStore()}
	rs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/conversations":
			var req struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			storeCtx := WithConversationID(context.Background(), req.ID)
			rs.store.CreateConversation(storeCtx, req.Title)
			resp := Conversation{ID: req.ID, Title: req.Title}
			writeJSON(w, http.StatusCreated, resp)
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/api/conversations/"):
			parts := strings.Split(r.URL.Path, "/")
			id := parts[len(parts)-1]
			var req struct {
				Title string `json:"title"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			c, err := rs.store.UpdateConversationTitle(context.Background(), id, req.Title)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, c)
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/api/conversations/"):
			parts := strings.Split(r.URL.Path, "/")
			id := parts[len(parts)-1]
			if err := rs.store.DeleteConversation(context.Background(), id); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/messages"):
			var req struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			parts := strings.Split(r.URL.Path, "/")
			convID := parts[len(parts)-2]
			msg, err := rs.store.AddMessage(context.Background(), convID, req.Role, req.Content)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusCreated, msg)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return rs
}

func (r *remoteSyncStore) Close() {
	r.server.Close()
}

func (r *remoteSyncStore) URL() string {
	return r.server.URL
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func TestMemoryStoreSaasAPIKeys(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	key1 := SaasAPIKey{KeyHash: "hash1", UserID: "u1", Name: "key1"}
	saved1, err := s.CreateSaasAPIKey(ctx, key1)
	if err != nil {
		t.Fatalf("CreateSaasAPIKey(key1) error = %v", err)
	}
	if saved1.ID == "" {
		t.Fatal("CreateSaasAPIKey returned empty ID")
	}
	if saved1.KeyHash != "hash1" || saved1.UserID != "u1" || saved1.Name != "key1" {
		t.Fatalf("unexpected key: %+v", saved1)
	}

	key2 := SaasAPIKey{KeyHash: "hash2", UserID: "u2", Name: "key2"}
	saved2, err := s.CreateSaasAPIKey(ctx, key2)
	if err != nil {
		t.Fatalf("CreateSaasAPIKey(key2) error = %v", err)
	}
	if saved2.ID == "" {
		t.Fatal("CreateSaasAPIKey returned empty ID")
	}

	got1, err := s.GetSaasAPIKeyByHash(ctx, "hash1")
	if err != nil {
		t.Fatalf("GetSaasAPIKeyByHash(hash1) error = %v", err)
	}
	if got1.ID != saved1.ID || got1.Name != "key1" {
		t.Fatalf("got key = %+v, want %+v", got1, saved1)
	}

	got2, err := s.GetSaasAPIKeyByHash(ctx, "hash2")
	if err != nil {
		t.Fatalf("GetSaasAPIKeyByHash(hash2) error = %v", err)
	}
	if got2.ID != saved2.ID {
		t.Fatalf("got key ID = %q, want %q", got2.ID, saved2.ID)
	}

	keys, err := s.ListSaasAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListSaasAPIKeys() error = %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("ListSaasAPIKeys() length = %d, want 2", len(keys))
	}

	if err := s.DeleteSaasAPIKey(ctx, saved1.ID); err != nil {
		t.Fatalf("DeleteSaasAPIKey() error = %v", err)
	}

	if _, err := s.GetSaasAPIKeyByHash(ctx, "hash1"); err != ErrKeyNotFound {
		t.Fatalf("GetSaasAPIKeyByHash(deleted) error = %v, want ErrKeyNotFound", err)
	}

	if _, err := s.GetSaasAPIKeyByHash(ctx, "missing"); err != ErrKeyNotFound {
		t.Fatalf("GetSaasAPIKeyByHash(missing) error = %v, want ErrKeyNotFound", err)
	}

	if err := s.DeleteSaasAPIKey(ctx, "missing-id"); err != ErrKeyNotFound {
		t.Fatalf("DeleteSaasAPIKey(missing) error = %v, want ErrKeyNotFound", err)
	}

	keys, err = s.ListSaasAPIKeys(ctx)
	if err != nil {
		t.Fatalf("ListSaasAPIKeys() error = %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("ListSaasAPIKeys() after delete length = %d, want 1", len(keys))
	}
	if keys[0].ID != saved2.ID {
		t.Fatalf("remaining key ID = %q, want %q", keys[0].ID, saved2.ID)
	}
}

func TestTitleFromMessage(t *testing.T) {
	if got := TitleFromMessage("  hello   linea  "); got != "hello linea" {
		t.Fatalf("TitleFromMessage() = %q", got)
	}
	if got := TitleFromMessage(""); got != "Untitled" {
		t.Fatalf("TitleFromMessage(empty) = %q", got)
	}
	if got := TitleFromMessage(strings.Repeat("😀", 61)); len([]rune(got)) != 60 {
		t.Fatalf("TitleFromMessage(unicode) length = %d, want 60", len([]rune(got)))
	}
}
