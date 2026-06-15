package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistryConfigured(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "", nil, nil)
	if r.Configured() {
		t.Fatal("expected not configured without providers")
	}

	r = NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	if !r.Configured() {
		t.Fatal("expected configured")
	}
}

func TestAuthURL(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	url, state, err := r.AuthURL(ProviderGitHub)
	if err != nil {
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("expected non-empty url")
	}
	if state == "" {
		t.Fatal("expected non-empty state")
	}
}

func TestAuthURLGoogleIncludesAccessTypeOffline(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGoogle: "client-id"}, nil)
	url, _, err := r.AuthURL(ProviderGoogle)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "access_type=offline") {
		t.Fatalf("expected access_type=offline in google auth url, got: %s", url)
	}
}

func TestAuthURLUnconfigured(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "", nil, nil)
	_, _, err := r.AuthURL(ProviderGitHub)
	if err == nil {
		t.Fatal("expected error for unconfigured provider")
	}
}

func TestVerifyState(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	_, state, err := r.AuthURL(ProviderGitHub)
	if err != nil {
		t.Fatal(err)
	}

	p, err := r.VerifyState(state)
	if err != nil {
		t.Fatal(err)
	}
	if p != ProviderGitHub {
		t.Fatalf("expected github, got %s", p)
	}

	_, err = r.VerifyState(state)
	if err == nil {
		t.Fatal("expected error for reused state")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "0123456789abcdef0123456789abcdef", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	plaintext := "gho_test_token_12345"
	encrypted, err := r.encrypt([]byte(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if string(encrypted) == plaintext {
		t.Fatal("expected encryption to change data")
	}

	decrypted, err := r.decrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != plaintext {
		t.Fatalf("expected %q, got %q", plaintext, string(decrypted))
	}
}

func TestEncryptWithoutKey(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	plaintext := "gho_test_token"
	encrypted, err := r.encrypt([]byte(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if string(encrypted) != plaintext {
		t.Fatal("expected no encryption when key is empty")
	}
}

func TestExchange(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/oauth/access_token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Fatalf("expected grant_type=authorization_code, got %s", r.Form.Get("grant_type"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"gho_exchanged","scope":"repo","token_type":"bearer"}`))
	}))
	defer ts.Close()

	r := NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	r.providers[ProviderGitHub] = ProviderConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		AuthURL:      ts.URL + "/login/oauth/authorize",
		TokenURL:     ts.URL + "/login/oauth/access_token",
		Scopes:       []string{"repo"},
		RedirectURL:  "http://localhost:8080/api/oauth/github/callback",
	}

	token, err := r.Exchange(context.Background(), ProviderGitHub, "test_code", "")
	if err != nil {
		t.Fatal(err)
	}
	if token.Provider != "github" {
		t.Fatalf("expected github, got %s", token.Provider)
	}
	if token.ID == "" {
		t.Fatal("expected non-empty id")
	}

	decrypted, err := r.DecryptToken(token.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "gho_exchanged" {
		t.Fatalf("expected gho_exchanged, got %s", decrypted)
	}
}

func TestExchangeError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad_verification_code","error_description":"The code passed is incorrect or expired."}`))
	}))
	defer ts.Close()

	r := NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	r.providers[ProviderGitHub] = ProviderConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     ts.URL + "/login/oauth/access_token",
	}

	_, err := r.Exchange(context.Background(), ProviderGitHub, "bad_code", "")
	if err == nil {
		t.Fatal("expected error for bad code")
	}
}

func TestExchangeEmptyToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	r := NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	r.providers[ProviderGitHub] = ProviderConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     ts.URL + "/login/oauth/access_token",
	}

	_, err := r.Exchange(context.Background(), ProviderGitHub, "code", "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestExchangeUnconfiguredProvider(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "", nil, nil)
	_, err := r.Exchange(context.Background(), ProviderGitHub, "code", "")
	if err == nil {
		t.Fatal("expected error for unconfigured provider")
	}
}

func TestRefreshToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"gho_refreshed","expires_in":3600,"token_type":"bearer"}`))
	}))
	defer ts.Close()

	r := NewRegistry("http://localhost:8080", "test-encryption-key", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	r.providers[ProviderGitHub] = ProviderConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     ts.URL + "/token",
	}
	encRefresh, err := r.encrypt([]byte("old-refresh-token"))
	if err != nil {
		t.Fatal(err)
	}

	newAccess, newRefresh, expiresAt, err := r.RefreshToken(context.Background(), ProviderGitHub, encRefresh)
	if err != nil {
		t.Fatal(err)
	}
	if expiresAt.IsZero() {
		t.Fatal("expected non-zero expiry")
	}
	if !expiresAt.After(time.Now()) {
		t.Fatal("expected expiry in the future")
	}
	if len(newRefresh) > 0 {
		t.Fatal("expected no new refresh token when not returned")
	}
	decrypted, err := r.DecryptToken(newAccess)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "gho_refreshed" {
		t.Fatalf("expected gho_refreshed, got %s", decrypted)
	}
}

func TestRefreshTokenWithRotation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"gho_refreshed2","refresh_token":"new-refresh-456","expires_in":3600,"token_type":"bearer"}`))
	}))
	defer ts.Close()

	r := NewRegistry("http://localhost:8080", "test-encryption-key", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	r.providers[ProviderGitHub] = ProviderConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     ts.URL + "/token",
	}
	encRefresh, err := r.encrypt([]byte("old-refresh-token"))
	if err != nil {
		t.Fatal(err)
	}

	_, newRefresh, _, err := r.RefreshToken(context.Background(), ProviderGitHub, encRefresh)
	if err != nil {
		t.Fatal(err)
	}
	if len(newRefresh) == 0 {
		t.Fatal("expected new refresh token")
	}
	decrypted, err := r.DecryptToken(newRefresh)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "new-refresh-456" {
		t.Fatalf("expected new-refresh-456, got %s", decrypted)
	}
}

func TestRefreshTokenEmpty(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	_, _, _, err := r.RefreshToken(context.Background(), ProviderGitHub, []byte{})
	if err == nil {
		t.Fatal("expected error for empty refresh token")
	}
}

func TestRefreshTokenUnconfigured(t *testing.T) {
	r := NewRegistry("http://localhost:8080", "", nil, nil)
	_, _, _, err := r.RefreshToken(context.Background(), ProviderGitHub, []byte("token"))
	if err == nil {
		t.Fatal("expected error for unconfigured provider")
	}
}

func TestRefreshTokenError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been revoked."}`))
	}))
	defer ts.Close()

	r := NewRegistry("http://localhost:8080", "", map[Provider]string{ProviderGitHub: "client-id"}, nil)
	r.providers[ProviderGitHub] = ProviderConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		TokenURL:     ts.URL + "/token",
	}
	_, _, _, err := r.RefreshToken(context.Background(), ProviderGitHub, []byte("bad-refresh"))
	if err == nil {
		t.Fatal("expected error for bad refresh")
	}
}
