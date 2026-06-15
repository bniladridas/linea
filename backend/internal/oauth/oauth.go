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
	"log/slog"
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
	ProviderGitLab Provider = "gitlab"
	ProviderGoogle Provider = "google"
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
	mu               sync.RWMutex
	providers        map[Provider]ProviderConfig
	identityProviders map[Provider]ProviderConfig
	states           map[string]string
	encKey           []byte
}

func NewRegistry(callbackBaseURL string, encKey string, clientIDs, clientSecrets map[Provider]string) *Registry {
	r := &Registry{
		providers:         make(map[Provider]ProviderConfig),
		identityProviders: make(map[Provider]ProviderConfig),
		states:            make(map[string]string),
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
		{ProviderGitLab, "https://gitlab.com/oauth/authorize", "https://gitlab.com/oauth/token", []string{"api", "read_user", "read_repository"}},
		{ProviderGoogle, "https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token", []string{"https://www.googleapis.com/auth/gmail.readonly", "https://www.googleapis.com/auth/gmail.send", "https://www.googleapis.com/auth/calendar", "https://www.googleapis.com/auth/drive.readonly"}},
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
	// Identity-only providers share client IDs but use minimal scopes
	for _, p := range []struct {
		provider Provider
		authURL  string
		tokenURL string
		scopes   []string
	}{
		{ProviderGitHub, "https://github.com/login/oauth/authorize", "https://github.com/login/oauth/access_token", []string{"read:user", "user:email"}},
		{ProviderGitLab, "https://gitlab.com/oauth/authorize", "https://gitlab.com/oauth/token", []string{"read_user"}},
		{ProviderGoogle, "https://accounts.google.com/o/oauth2/v2/auth", "https://oauth2.googleapis.com/token", []string{"openid", "email", "profile"}},
	} {
		clientID := clientIDs[p.provider]
		clientSecret := clientSecrets[p.provider]
		if clientID == "" {
			continue
		}
		r.identityProviders[p.provider] = ProviderConfig{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			AuthURL:      p.authURL,
			TokenURL:     p.tokenURL,
			Scopes:       p.scopes,
			RedirectURL:  callbackBaseURL + "/api/auth/" + string(p.provider) + "/callback",
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

	if provider == ProviderGoogle {
		v.Set("access_type", "offline")
	}

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
	v.Set("grant_type", "authorization_code")

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
	if encRefresh == nil {
		encRefresh = []byte{}
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

func (r *Registry) RefreshToken(ctx context.Context, provider Provider, encryptedRefresh []byte) (newAccessToken []byte, newRefreshToken []byte, expiresAt time.Time, err error) {
	r.mu.RLock()
	cfg, ok := r.providers[provider]
	r.mu.RUnlock()
	if !ok {
		return nil, nil, time.Time{}, fmt.Errorf("oauth provider %q not configured", provider)
	}
	refreshRaw, err := r.decrypt(encryptedRefresh)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("decrypt refresh token: %w", err)
	}
	if len(refreshRaw) == 0 {
		return nil, nil, time.Time{}, errors.New("no refresh token available")
	}

	v := url.Values{}
	v.Set("client_id", cfg.ClientID)
	v.Set("client_secret", cfg.ClientSecret)
	v.Set("refresh_token", string(refreshRaw))
	v.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.TokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, time.Time{}, err
	}

	var tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token,omitempty"`
		ExpiresIn    int    `json:"expires_in,omitempty"`
		Error        string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("parse refresh response: %w", err)
	}
	if tokenResponse.Error != "" {
		return nil, nil, time.Time{}, fmt.Errorf("token refresh error: %s", tokenResponse.Error)
	}
	if tokenResponse.AccessToken == "" {
		return nil, nil, time.Time{}, errors.New("empty access token in refresh response")
	}

	encAccess, err := r.encrypt([]byte(tokenResponse.AccessToken))
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("encrypt refreshed access token: %w", err)
	}

	if tokenResponse.RefreshToken != "" {
		newRefreshToken, err = r.encrypt([]byte(tokenResponse.RefreshToken))
		if err != nil {
			return nil, nil, time.Time{}, fmt.Errorf("encrypt refreshed refresh token: %w", err)
		}
	}

	if tokenResponse.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	}

	return encAccess, newRefreshToken, expiresAt, nil
}

func (r *Registry) IdentityProviders() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	providers := make([]Provider, 0, len(r.identityProviders))
	for p := range r.identityProviders {
		providers = append(providers, p)
	}
	return providers
}

func (r *Registry) IdentityAuthURL(provider Provider) (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, ok := r.identityProviders[provider]
	if !ok {
		return "", "", fmt.Errorf("identity provider %q not configured", provider)
	}
	stateB := make([]byte, 16)
	if _, err := rand.Read(stateB); err != nil {
		return "", "", err
	}
	state := fmt.Sprintf("%x", stateB)
	r.states[state] = "identity:" + string(provider)

	v := url.Values{}
	v.Set("client_id", cfg.ClientID)
	v.Set("redirect_uri", cfg.RedirectURL)
	v.Set("state", state)
	v.Set("scope", strings.Join(cfg.Scopes, " "))
	v.Set("response_type", "code")

	if provider == ProviderGoogle {
		v.Set("access_type", "online")
	}

	return cfg.AuthURL + "?" + v.Encode(), state, nil
}

func (r *Registry) VerifyIdentityState(state string) (Provider, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, ok := r.states[state]
	if !ok {
		return "", errors.New("invalid state parameter")
	}
	delete(r.states, state)
	prefix := "identity:"
	if !strings.HasPrefix(raw, prefix) {
		return "", errors.New("invalid state parameter")
	}
	return Provider(strings.TrimPrefix(raw, prefix)), nil
}

func (r *Registry) ExchangeIdentityToken(ctx context.Context, provider Provider, code string) (accessToken string, email string, name string, err error) {
	cfg, ok := r.identityProviders[provider]
	if !ok {
		return "", "", "", fmt.Errorf("identity provider %q not configured", provider)
	}

	v := url.Values{}
	v.Set("client_id", cfg.ClientID)
	v.Set("client_secret", cfg.ClientSecret)
	v.Set("code", code)
	v.Set("redirect_uri", cfg.RedirectURL)
	v.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.TokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}

	var tokenResponse struct {
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token,omitempty"`
		RefreshToken string `json:"refresh_token,omitempty"`
		ExpiresIn    int    `json:"expires_in,omitempty"`
		Error        string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", "", "", fmt.Errorf("parse token response: %w", err)
	}
	if tokenResponse.Error != "" {
		return "", "", "", fmt.Errorf("token exchange error: %s", tokenResponse.Error)
	}
	if tokenResponse.AccessToken == "" {
		return "", "", "", errors.New("empty access token in response")
	}

	accessToken = tokenResponse.AccessToken

	// Fetch user info from the provider
	switch provider {
	case ProviderGitHub:
		userReq, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
		userReq.Header.Set("Authorization", "Bearer "+accessToken)
		userReq.Header.Set("Accept", "application/vnd.github.v3+json")
		userResp, err := httpClient.Do(userReq)
		if err == nil {
			defer userResp.Body.Close()
			var ghUser struct {
				Login string `json:"login"`
				Email string `json:"email"`
				Name  string `json:"name"`
			}
			if err := json.NewDecoder(userResp.Body).Decode(&ghUser); err == nil {
				name = ghUser.Name
				if name == "" {
					name = ghUser.Login
				}
				email = ghUser.Email
			} else {
				slog.Debug("oauth: decode GitHub user response", "err", err)
			}
		} else {
			slog.Debug("oauth: fetch GitHub user", "err", err)
		}
		// Try /user/emails if primary email was not returned
		if email == "" {
			emailsReq, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
			emailsReq.Header.Set("Authorization", "Bearer "+accessToken)
			emailsReq.Header.Set("Accept", "application/vnd.github.v3+json")
			emailsResp, err := httpClient.Do(emailsReq)
			if err == nil {
				defer emailsResp.Body.Close()
				var emails []struct {
					Email    string `json:"email"`
					Primary  bool   `json:"primary"`
					Verified bool   `json:"verified"`
				}
				if err := json.NewDecoder(emailsResp.Body).Decode(&emails); err == nil {
					for _, e := range emails {
						if e.Primary && e.Verified {
							email = e.Email
							break
						}
					}
					if email == "" && len(emails) > 0 {
						email = emails[0].Email
					}
				} else {
					slog.Debug("oauth: decode GitHub emails response", "err", err)
				}
			} else {
				slog.Debug("oauth: fetch GitHub emails", "err", err)
			}
		}
	case ProviderGitLab:
		userReq, _ := http.NewRequestWithContext(ctx, "GET", "https://gitlab.com/api/v4/user", nil)
		userReq.Header.Set("Authorization", "Bearer "+accessToken)
		userResp, err := httpClient.Do(userReq)
		if err == nil {
			defer userResp.Body.Close()
			var glUser struct {
				Email    string `json:"email"`
				Name     string `json:"name"`
				Username string `json:"username"`
			}
			if err := json.NewDecoder(userResp.Body).Decode(&glUser); err == nil {
				name = glUser.Name
				if name == "" {
					name = glUser.Username
				}
				email = glUser.Email
			} else {
				slog.Debug("oauth: decode GitLab user response", "err", err)
			}
		} else {
			slog.Debug("oauth: fetch GitLab user", "err", err)
		}
	case ProviderGoogle:
		// Google can provide user info via the ID token (JWT) or the userinfo endpoint
		userReq, _ := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
		userReq.Header.Set("Authorization", "Bearer "+accessToken)
		userResp, err := httpClient.Do(userReq)
		if err == nil {
			defer userResp.Body.Close()
			var googleUser struct {
				ID      string `json:"id"`
				Email   string `json:"email"`
				Name    string `json:"name"`
			}
			if err := json.NewDecoder(userResp.Body).Decode(&googleUser); err == nil {
				email = googleUser.Email
				name = googleUser.Name
			} else {
				slog.Debug("oauth: decode Google user response", "err", err)
			}
		} else {
			slog.Debug("oauth: fetch Google user", "err", err)
		}
	}

	return accessToken, email, name, nil
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
