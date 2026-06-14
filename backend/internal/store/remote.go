package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var _ Store = (*RemoteStore)(nil)

// RemoteStore proxies Store calls to a remote Linea instance via its HTTP API.
// Set LINEA_SYNC_URL to the remote base URL and LINEA_SYNC_TOKEN to the bearer
// token if the remote requires authentication.
type RemoteStore struct {
	base   string
	token  string
	client *http.Client
}

func NewRemoteStore(baseURL, token string) (*RemoteStore, error) {
	if baseURL == "" {
		return nil, errors.New("remote store: base URL is required")
	}
	return &RemoteStore{
		base:   strings.TrimRight(baseURL, "/"),
		token:  token,
		client: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (s *RemoteStore) do(ctx context.Context, method, path string, body, out any) error {
	var buf *bytes.Buffer
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewBuffer(data)
	} else {
		buf = &bytes.Buffer{}
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base+path, buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: remote store: %s %s", ErrNotFound, method, path)
	}
	if resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("remote store: %s %s returned %d", method, path, resp.StatusCode)
		}
		return fmt.Errorf("remote store: %s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (s *RemoteStore) ListConversations(ctx context.Context) ([]Conversation, error) {
	var out []Conversation
	return out, s.do(ctx, http.MethodGet, "/api/conversations", nil, &out)
}

func (s *RemoteStore) CreateConversation(ctx context.Context, title string) (Conversation, error) {
	var out Conversation
	return out, s.do(ctx, http.MethodPost, "/api/conversations", map[string]string{"title": title}, &out)
}

func (s *RemoteStore) UpdateConversationTitle(ctx context.Context, id, title string) (Conversation, error) {
	var out Conversation
	return out, s.do(ctx, http.MethodPatch, "/api/conversations/"+id, map[string]string{"title": title}, &out)
}

func (s *RemoteStore) DeleteConversation(ctx context.Context, id string) error {
	return s.do(ctx, http.MethodDelete, "/api/conversations/"+id, nil, nil)
}

func (s *RemoteStore) ListMessages(ctx context.Context, conversationID string) ([]Message, error) {
	var out []Message
	return out, s.do(ctx, http.MethodGet, "/api/conversations/"+conversationID+"/messages", nil, &out)
}

func (s *RemoteStore) AddMessage(ctx context.Context, conversationID, role, content string) (Message, error) {
	var out Message
	body := map[string]string{"role": role, "content": content}
	return out, s.do(ctx, http.MethodPost, "/api/conversations/"+conversationID+"/messages", body, &out)
}

func (s *RemoteStore) ListAgentRuns(ctx context.Context) ([]AgentRun, error) {
	var out []AgentRun
	return out, s.do(ctx, http.MethodGet, "/api/agent/runs", nil, &out)
}

func (s *RemoteStore) ListUsers(ctx context.Context) ([]User, error) {
	var out []User
	return out, s.do(ctx, http.MethodGet, "/api/users", nil, &out)
}

func (s *RemoteStore) CreateUser(ctx context.Context, email, name string) (User, error) {
	var out User
	return out, s.do(ctx, http.MethodPost, "/api/users", map[string]string{"email": email, "name": name}, &out)
}

func (s *RemoteStore) SaveOAuthToken(ctx context.Context, token OAuthToken) error {
	return s.do(ctx, http.MethodPost, "/api/oauth/tokens", token, nil)
}

func (s *RemoteStore) GetOAuthToken(ctx context.Context, id string) (OAuthToken, error) {
	var out OAuthToken
	return out, s.do(ctx, http.MethodGet, "/api/oauth/tokens/"+id, nil, &out)
}

func (s *RemoteStore) ListOAuthTokens(ctx context.Context) ([]OAuthToken, error) {
	var out []OAuthToken
	return out, s.do(ctx, http.MethodGet, "/api/oauth/tokens", nil, &out)
}

func (s *RemoteStore) DeleteOAuthToken(ctx context.Context, id string) error {
	return s.do(ctx, http.MethodDelete, "/api/oauth/tokens/"+id, nil, nil)
}

func (s *RemoteStore) Close() error { return nil }

func (s *RemoteStore) AddAgentRun(ctx context.Context, state string, summary json.RawMessage) (AgentRun, error) {
	var out AgentRun
	state = strings.TrimSpace(state)
	if state == "" {
		state = "ready"
	}
	if len(summary) == 0 {
		summary = json.RawMessage(`{}`)
	}
	body := map[string]any{"state": state, "summary": summary}
	return out, s.do(ctx, http.MethodPost, "/api/agent/runs", body, &out)
}
