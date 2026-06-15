package store

import (
	"context"
	"encoding/json"
	"log/slog"
)

var _ Store = (*SyncStore)(nil)

type SyncStore struct {
	local  Store
	remote *RemoteStore
}

func NewSyncStore(local Store, remote *RemoteStore) *SyncStore {
	return &SyncStore{local: local, remote: remote}
}

func (s *SyncStore) syncRemote(fn func() error) {
	if err := fn(); err != nil {
		slog.Warn("sync: remote failed", "error", err)
	}
}

func (s *SyncStore) ListConversations(ctx context.Context) ([]Conversation, error) {
	return s.local.ListConversations(ctx)
}

func (s *SyncStore) CreateConversation(ctx context.Context, title string) (Conversation, error) {
	id := ConversationIDFromContext(ctx)
	if id == "" {
		id = NewID()
	}
	ctx = WithConversationID(ctx, id)
	c, err := s.local.CreateConversation(ctx, title)
	if err != nil {
		return c, err
	}
	s.syncRemote( func() error {
		_, err := s.remote.CreateConversation(ctx, title)
		return err
	})
	return c, nil
}

func (s *SyncStore) UpdateConversationTitle(ctx context.Context, id, title string) (Conversation, error) {
	c, err := s.local.UpdateConversationTitle(ctx, id, title)
	if err != nil {
		return c, err
	}
	s.syncRemote( func() error {
		_, err := s.remote.UpdateConversationTitle(ctx, id, title)
		return err
	})
	return c, nil
}

func (s *SyncStore) DeleteConversation(ctx context.Context, id string) error {
	if err := s.local.DeleteConversation(ctx, id); err != nil {
		return err
	}
	s.syncRemote( func() error {
		return s.remote.DeleteConversation(ctx, id)
	})
	return nil
}

func (s *SyncStore) ListMessages(ctx context.Context, conversationID string) ([]Message, error) {
	return s.local.ListMessages(ctx, conversationID)
}

func (s *SyncStore) AddMessage(ctx context.Context, conversationID, role, content string) (Message, error) {
	m, err := s.local.AddMessage(ctx, conversationID, role, content)
	if err != nil {
		return m, err
	}
	s.syncRemote( func() error {
		_, err := s.remote.AddMessage(ctx, conversationID, role, content)
		return err
	})
	return m, nil
}

func (s *SyncStore) ListAgentRuns(ctx context.Context) ([]AgentRun, error) {
	return s.local.ListAgentRuns(ctx)
}

func (s *SyncStore) AddAgentRun(ctx context.Context, state string, summary json.RawMessage) (AgentRun, error) {
	r, err := s.local.AddAgentRun(ctx, state, summary)
	if err != nil {
		return r, err
	}
	s.syncRemote( func() error {
		_, err := s.remote.AddAgentRun(ctx, state, summary)
		return err
	})
	return r, nil
}

func (s *SyncStore) ListUsers(ctx context.Context) ([]User, error) {
	return s.local.ListUsers(ctx)
}

func (s *SyncStore) CreateUser(ctx context.Context, email, name string) (User, error) {
	u, err := s.local.CreateUser(ctx, email, name)
	if err != nil {
		return u, err
	}
	s.syncRemote( func() error {
		_, err := s.remote.CreateUser(ctx, email, name)
		return err
	})
	return u, nil
}

func (s *SyncStore) GetUserByEmail(ctx context.Context, email string) (User, error) {
	return s.local.GetUserByEmail(ctx, email)
}

func (s *SyncStore) GetUserByID(ctx context.Context, id string) (User, error) {
	return s.local.GetUserByID(ctx, id)
}

func (s *SyncStore) SaveOAuthToken(ctx context.Context, token OAuthToken) error {
	if err := s.local.SaveOAuthToken(ctx, token); err != nil {
		return err
	}
	s.syncRemote( func() error {
		return s.remote.SaveOAuthToken(ctx, token)
	})
	return nil
}

func (s *SyncStore) GetOAuthToken(ctx context.Context, id string) (OAuthToken, error) {
	return s.local.GetOAuthToken(ctx, id)
}

func (s *SyncStore) ListOAuthTokens(ctx context.Context) ([]OAuthToken, error) {
	return s.local.ListOAuthTokens(ctx)
}

func (s *SyncStore) DeleteOAuthToken(ctx context.Context, id string) error {
	if err := s.local.DeleteOAuthToken(ctx, id); err != nil {
		return err
	}
	s.syncRemote( func() error {
		return s.remote.DeleteOAuthToken(ctx, id)
	})
	return nil
}

func (s *SyncStore) CreateSession(ctx context.Context, userID string) (Session, error) {
	return s.local.CreateSession(ctx, userID)
}

func (s *SyncStore) GetSessionByToken(ctx context.Context, token string) (Session, error) {
	return s.local.GetSessionByToken(ctx, token)
}

func (s *SyncStore) DeleteSession(ctx context.Context, token string) error {
	return s.local.DeleteSession(ctx, token)
}

func (s *SyncStore) Close() error {
	if err := s.local.Close(); err != nil {
		slog.Warn("sync: local close", "error", err)
	}
	return s.remote.Close()
}
