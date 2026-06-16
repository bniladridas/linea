package saas

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"linea/backend/internal/store"
)

type APIKey struct {
	ID          string    `json:"id"`
	Key         string    `json:"key,omitempty"`
	UserID      string    `json:"userId"`
	WorkspaceID string    `json:"workspaceId,omitempty"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
}

var ErrKeyNotFound = errors.New("api key not found")
var ErrUserNotFound = errors.New("user not found")
var ErrEmailExists = errors.New("email already exists")
var ErrUnauthorized = errors.New("unauthorized")

type Manager struct {
	mu       sync.RWMutex
	store    store.Store
	adminKey string
}

func NewManager(s store.Store) *Manager {
	return &Manager{store: s}
}

func (m *Manager) SetAdminKey(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adminKey = key
}

func (m *Manager) getAdminKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.adminKey
}

func (m *Manager) ValidateAdminKey(key string) error {
	if key == "" {
		return ErrUnauthorized
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.adminKey == "" {
		return ErrUnauthorized
	}
	if key != m.adminKey {
		return ErrUnauthorized
	}
	return nil
}

func (m *Manager) CreateKey(ctx context.Context, userID, name, workspaceID string) (APIKey, error) {
	if userID == "" {
		return APIKey{}, errors.New("userID is required")
	}

	if _, err := m.store.GetUserByID(ctx, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return APIKey{}, ErrUserNotFound
		}
		return APIKey{}, err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return APIKey{}, err
	}
	key := "lin_" + hex.EncodeToString(raw)
	hash := sha256Hex(key)

	sk := store.SaasAPIKey{
		KeyHash:     hash,
		UserID:      userID,
		WorkspaceID: workspaceID,
		Name:        name,
	}
	saved, err := m.store.CreateSaasAPIKey(ctx, sk)
	if err != nil {
		return APIKey{}, err
	}

	return APIKey{
		ID:          saved.ID,
		Key:         key,
		UserID:      saved.UserID,
		WorkspaceID: saved.WorkspaceID,
		Name:        saved.Name,
		CreatedAt:   saved.CreatedAt,
	}, nil
}

func (m *Manager) CreateWorkspace(ctx context.Context, name string) (store.Workspace, error) {
	return m.store.CreateWorkspace(ctx, name)
}

func (m *Manager) ListWorkspaces(ctx context.Context) ([]store.Workspace, error) {
	return m.store.ListWorkspaces(ctx)
}

func (m *Manager) AddWorkspaceMember(ctx context.Context, workspaceID, userID string) error {
	return m.store.AddWorkspaceMember(ctx, workspaceID, userID)
}

func (m *Manager) RemoveWorkspaceMember(ctx context.Context, workspaceID, userID string) error {
	return m.store.RemoveWorkspaceMember(ctx, workspaceID, userID)
}

func (m *Manager) ListWorkspaceMembers(ctx context.Context, workspaceID string) ([]store.WorkspaceMember, error) {
	return m.store.ListWorkspaceMembers(ctx, workspaceID)
}

func (m *Manager) ValidateKey(ctx context.Context, key string) (APIKey, error) {
	sk, err := m.store.GetSaasAPIKeyByHash(ctx, sha256Hex(key))
	if err != nil {
		if errors.Is(err, store.ErrKeyNotFound) {
			return APIKey{}, ErrKeyNotFound
		}
		return APIKey{}, err
	}
	return APIKey{
		ID:          sk.ID,
		UserID:      sk.UserID,
		WorkspaceID: sk.WorkspaceID,
		Name:        sk.Name,
		CreatedAt:   sk.CreatedAt,
	}, nil
}

func (m *Manager) ListKeys(ctx context.Context) ([]APIKey, error) {
	keys, err := m.store.ListSaasAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]APIKey, 0, len(keys))
	for _, k := range keys {
		items = append(items, APIKey{
			ID:          k.ID,
			UserID:      k.UserID,
			WorkspaceID: k.WorkspaceID,
			Name:        k.Name,
			CreatedAt:   k.CreatedAt,
		})
	}
	return items, nil
}

func (m *Manager) DeleteKey(ctx context.Context, id string) error {
	err := m.store.DeleteSaasAPIKey(ctx, id)
	if errors.Is(err, store.ErrKeyNotFound) {
		return ErrKeyNotFound
	}
	return err
}

func (m *Manager) CreateUser(ctx context.Context, email, name string) (store.User, error) {
	u, err := m.store.CreateUser(ctx, email, name)
	if errors.Is(err, store.ErrEmailExists) {
		return store.User{}, ErrEmailExists
	}
	return u, err
}

func (m *Manager) GetUser(ctx context.Context, userID string) (store.User, error) {
	return m.store.GetUserByID(ctx, userID)
}

func (m *Manager) ListUsers(ctx context.Context) ([]store.User, error) {
	return m.store.ListUsers(ctx)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
