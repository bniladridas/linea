package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"linea/backend/internal/integrations/github"
	"linea/backend/internal/integrations/gitlab"
	"linea/backend/internal/integrations/google"
	"linea/backend/internal/oauth"
	"linea/backend/internal/store"
)

func (s *Server) listOAuthProviders(w http.ResponseWriter, r *http.Request) {
	if s.oauthRegistry == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}

	tokens, err := s.store.ListOAuthTokens(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not list tokens."})
		return
	}
	connected := make(map[string]bool)
	for _, tok := range tokens {
		connected[tok.Provider] = true
	}

	providers := []map[string]any{
		{
			"id":        "github",
			"name":      "GitHub",
			"connected": connected["github"],
			"scopes":    []string{"repo", "read:user"},
		},
		{
			"id":        "gitlab",
			"name":      "GitLab",
			"connected": connected["gitlab"],
			"scopes":    []string{"api", "read_user", "read_repository"},
		},
		{
			"id":        "google",
			"name":      "Google",
			"connected": connected["google"],
			"scopes":    []string{"gmail.readonly", "gmail.send", "calendar", "drive.readonly"},
		},
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) startOAuthFlow(w http.ResponseWriter, r *http.Request) {
	if s.oauthRegistry == nil {
		writeError(w, http.StatusNotFound, "OAuth is not configured.")
		return
	}
	provider := oauth.Provider(r.PathValue("provider"))
	authURL, _, err := s.oauthRegistry.AuthURL(provider)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if s.oauthRegistry == nil {
		writeError(w, http.StatusNotFound, "OAuth is not configured.")
		return
	}

	provider := oauth.Provider(r.PathValue("provider"))
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		writeError(w, http.StatusBadRequest, "Missing authorization code.")
		return
	}

	expectedProvider, err := s.oauthRegistry.VerifyState(state)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid state parameter.")
		return
	}
	if expectedProvider != provider {
		writeError(w, http.StatusBadRequest, "Provider mismatch in state.")
		return
	}

	// Find existing token for this provider so we update rather than duplicate
	var existingID string
	existing, err := s.store.ListOAuthTokens(r.Context())
	if err != nil {
		slog.Warn("oauth callback: could not list existing tokens, may create duplicate", "error", err)
	}
	for _, t := range existing {
		if t.Provider == string(provider) {
			existingID = t.ID
			break
		}
	}

	token, err := s.oauthRegistry.Exchange(r.Context(), provider, code, existingID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Token exchange failed: "+err.Error())
		return
	}

	// Save the token before enriching with account info
	if err := s.store.SaveOAuthToken(r.Context(), *token); err != nil {
		slog.Error("oauth callback: save token", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not save token.")
		return
	}

	// Populate account info from the provider's API
	accessToken, err := s.oauthRegistry.DecryptToken(token.AccessToken)
	if err == nil && accessToken != "" {
		switch string(provider) {
		case "github":
			gh := github.NewClient(accessToken)
			if user, err := gh.GetUser(r.Context()); err == nil {
				token.AccountName = user.Login
				token.AccountID = fmt.Sprintf("%d", user.ID)
			}
		case "gitlab":
			gl := gitlab.NewClient(accessToken)
			if user, err := gl.GetUser(r.Context()); err == nil {
				token.AccountName = user.Username
				token.AccountID = fmt.Sprintf("%d", user.ID)
			}
		case "google":
			gg := google.NewClient(accessToken)
			if userInfo, err := gg.GetUserInfo(r.Context()); err == nil {
				token.AccountName = userInfo.Email
				token.AccountID = userInfo.ID
			}
		}
		if token.AccountName != "" {
			if err := s.store.SaveOAuthToken(r.Context(), *token); err != nil {
				slog.Error("failed to save enriched oauth token", "error", err)
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	jsProvider, _ := json.Marshal(string(provider))
	jsTokenID, _ := json.Marshal(token.ID)
	fmt.Fprintf(w, `<!doctype html>
<html><body><script>
if (window.opener) {
  window.opener.postMessage({type:"oauth-callback",provider:%s,status:"success",tokenId:%s},"*");
  window.close();
} else {
  document.write("OAuth connected. You may close this window.");
}
</script></body></html>`, jsProvider, jsTokenID)
}

func (s *Server) saveOAuthToken(w http.ResponseWriter, r *http.Request) {
	if s.oauthRegistry == nil {
		writeError(w, http.StatusNotFound, "OAuth is not configured.")
		return
	}
	var token struct {
		ID           string    `json:"id"`
		Provider     string    `json:"provider"`
		AccountName  string    `json:"accountName"`
		AccountID    string    `json:"accountId"`
		AccessToken  []byte    `json:"accessToken"`
		RefreshToken []byte    `json:"refreshToken"`
		ExpiresAt    time.Time `json:"expiresAt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&token); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid token body.")
		return
	}
	st := store.OAuthToken{
		ID:           token.ID,
		Provider:     token.Provider,
		AccountName:  token.AccountName,
		AccountID:    token.AccountID,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresAt:    token.ExpiresAt,
	}
	if err := s.store.SaveOAuthToken(r.Context(), st); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save token.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          st.ID,
		"provider":    st.Provider,
		"accountName": st.AccountName,
		"accountId":   st.AccountID,
		"createdAt":   st.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (s *Server) listOAuthTokens(w http.ResponseWriter, r *http.Request) {
	if s.oauthRegistry == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	tokens, err := s.store.ListOAuthTokens(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not list tokens.")
		return
	}
	// Return tokens without sensitive data
	type safeToken struct {
		ID          string `json:"id"`
		Provider    string `json:"provider"`
		AccountName string `json:"accountName"`
		AccountID   string `json:"accountId"`
		ExpiresAt   string `json:"expiresAt,omitempty"`
		CreatedAt   string `json:"createdAt"`
	}
	safe := make([]safeToken, 0, len(tokens))
	for _, t := range tokens {
		st := safeToken{
			ID:          t.ID,
			Provider:    t.Provider,
			AccountName: t.AccountName,
			AccountID:   t.AccountID,
			CreatedAt:   t.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		}
		if !t.ExpiresAt.IsZero() {
			st.ExpiresAt = t.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		safe = append(safe, st)
	}
	writeJSON(w, http.StatusOK, safe)
}

func (s *Server) deleteOAuthToken(w http.ResponseWriter, r *http.Request) {
	if s.oauthRegistry == nil {
		writeError(w, http.StatusNotFound, "OAuth is not configured.")
		return
	}
	if err := s.store.DeleteOAuthToken(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, http.StatusNotFound, "Token not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
