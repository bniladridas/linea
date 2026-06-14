package oauth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"linea/backend/internal/store"
)

type Provider string

const (
	ProviderGitHub Provider = "github"
)

type ProviderConfig struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	Scopes       []string
	RedirectURL  string
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

type Registry struct {
	mu        sync.RWMutex
	providers map[Provider]ProviderConfig
	states    map[string]string
	encKey    []byte
}

func NewRegistry(callbackBaseURL string, encKey string, clientIDs, clientSecrets map[Provider]string) *Registry {
	r := &Registry{
		providers: make(map[Provider]ProviderConfig),
		states:    make(map[string]string),
	}
	if encKey != "" {
		hash := sha256.Sum256([]byte(encKey))
		r.encKey = hash[:]
	}
	for _, p := range []struct {
		provider Provider
		authURL  string
		tokenURL string
		scopes   []string
	}{
		{ProviderGitHub, "https://github.com/login/oauth/authorize", "https://github.com/login/oauth/access_token", []string{"repo", "read:user"}},
	} {
		clientID := clientIDs[p.provider]
		clientSecret := clientSecrets[p.provider]
		if clientID == "" {
			continue
		}
		r.providers[p.provider] = ProviderConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			AuthURL:      p.authURL,
			TokenURL:     p.tokenURL,
			Scopes:       p.scopes,
			RedirectURL:  callbackBaseURL + "/api/oauth/" + string(p.provider) + "/callback",
		}
	}
	return r
}

func (r *Registry) Configured() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers) > 0
}

func (r *Registry) AuthURL(provider Provider) (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.providers[provider]
	if !ok {
		return "", "", fmt.Errorf("oauth provider %q not configured", provider)
	}
	stateB := make([]byte, 16)
	if _, err := rand.Read(stateB); err != nil {
		return "", "", err
	}
	state := fmt.Sprintf("%x", stateB)
	r.states[state] = string(provider)

	v := url.Values{}
	v.Set("client_id", cfg.ClientID)
	v.Set("redirect_uri", cfg.RedirectURL)
	v.Set("state", state)
	v.Set("scope", strings.Join(cfg.Scopes, " "))
	v.Set("response_type", "code")

	return cfg.AuthURL + "?" + v.Encode(), state, nil
}

func (r *Registry) Exchange(ctx context.Context, provider Provider, code string, existingID string) (*store.OAuthToken, error) {
	cfg, ok := r.providers[provider]
	if !ok {
		return nil, fmt.Errorf("oauth provider %q not configured", provider)
	}

	v := url.Values{}
	v.Set("client_id", cfg.ClientID)
	v.Set("client_secret", cfg.ClientSecret)
	v.Set("code", code)
	v.Set("redirect_uri", cfg.RedirectURL)

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.TokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token,omitempty"`
		ExpiresIn    int    `json:"expires_in,omitempty"`
		Error        string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tokenResponse.Error != "" {
		return nil, fmt.Errorf("token exchange error: %s", tokenResponse.Error)
	}
	if tokenResponse.AccessToken == "" {
		return nil, errors.New("empty access token in response")
	}

	var expiresAt time.Time
	if tokenResponse.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	}

	encAccess, err := r.encrypt([]byte(tokenResponse.AccessToken))
	if err != nil {
		return nil, fmt.Errorf("encrypt access token: %w", err)
	}
	var encRefresh []byte
	if tokenResponse.RefreshToken != "" {
		encRefresh, err = r.encrypt([]byte(tokenResponse.RefreshToken))
		if err != nil {
			return nil, fmt.Errorf("encrypt refresh token: %w", err)
		}
	}

	tokenID := existingID
	if tokenID == "" {
		tokenID = store.NewID()
	}
	oauthToken := store.OAuthToken{
		ID:           tokenID,
		Provider:     string(provider),
		AccessToken:  encAccess,
		RefreshToken: encRefresh,
		ExpiresAt:    expiresAt,
	}

	return &oauthToken, nil
}

func (r *Registry) VerifyState(state string) (Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	provider, ok := r.states[state]
	if !ok {
		return "", errors.New("invalid state parameter")
	}
	delete(r.states, state)
	return Provider(provider), nil
}

func (r *Registry) DecryptToken(encrypted []byte) (string, error) {
	decrypted, err := r.decrypt(encrypted)
	if err != nil {
		return "", err
	}
	return string(decrypted), nil
}

func (r *Registry) encrypt(plaintext []byte) ([]byte, error) {
	if r.encKey == nil {
		return plaintext, nil
	}
	block, err := aes.NewCipher(r.encKey)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aesGCM.Seal(nonce, nonce, plaintext, nil), nil
}

func (r *Registry) decrypt(ciphertext []byte) ([]byte, error) {
	if r.encKey == nil {
		return ciphertext, nil
	}
	block, err := aes.NewCipher(r.encKey)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := aesGCM.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return aesGCM.Open(nil, nonce, ciphertext, nil)
}
