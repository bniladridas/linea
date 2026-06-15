package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"linea/backend/internal/oauth"
	"linea/backend/internal/store"
)

func (s *Server) EnableAccounts() {
	s.enableAccounts = true
}

func extractBearerToken(r *http.Request) (string, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return "", fmt.Errorf("no bearer token")
	}
	token := auth[len("Bearer "):]
	if token == "" {
		return "", fmt.Errorf("empty bearer token")
	}
	return token, nil
}

func (s *Server) sessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.sessionUser(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "Invalid or missing session token.")
			return
		}
		ctx := store.WithUserID(r.Context(), user.ID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) sessionUser(r *http.Request) (store.User, error) {
	token, err := extractBearerToken(r)
	if err != nil {
		return store.User{}, err
	}
	session, err := s.store.GetSessionByToken(r.Context(), token)
	if err != nil {
		return store.User{}, err
	}
	return s.store.GetUserByID(r.Context(), session.UserID)
}

func (s *Server) listAuthProviders(w http.ResponseWriter, r *http.Request) {
	if s.oauthRegistry == nil {
		writeJSON(w, http.StatusOK, []map[string]any{})
		return
	}
	providers := s.oauthRegistry.IdentityProviders()
	result := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		result = append(result, map[string]any{
			"id":   string(p),
			"name": providerDisplayName(p),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) startIdentityAuth(w http.ResponseWriter, r *http.Request) {
	if s.oauthRegistry == nil {
		writeError(w, http.StatusNotFound, "OAuth is not configured.")
		return
	}
	provider := oauth.Provider(r.PathValue("provider"))
	if provider == "" {
		writeError(w, http.StatusBadRequest, "Provider is required.")
		return
	}
	authURL, _, err := s.oauthRegistry.IdentityAuthURL(provider)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

func (s *Server) handleIdentityCallback(w http.ResponseWriter, r *http.Request) {
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

	expectedProvider, err := s.oauthRegistry.VerifyIdentityState(state)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid state parameter.")
		return
	}
	if expectedProvider != provider {
		writeError(w, http.StatusBadRequest, "Provider mismatch.")
		return
	}

	_, email, name, err := s.oauthRegistry.ExchangeIdentityToken(r.Context(), provider, code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Token exchange failed.")
		slog.Error("identity token exchange", "provider", provider, "error", err)
		return
	}

	if email == "" {
		writeError(w, http.StatusInternalServerError, "Could not retrieve email from provider.")
		slog.Error("identity callback: no email from provider", "provider", provider)
		return
	}

	// Find or create user
	user, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "Could not look up user.")
			slog.Error("identity callback: get user by email", "error", err)
			return
		}
		user, err = s.store.CreateUser(r.Context(), email, name)
		if err != nil {
			if errors.Is(err, store.ErrEmailExists) {
				// Race: another request created the user between our lookup and create
				user, err = s.store.GetUserByEmail(r.Context(), email)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "Could not find user after race.")
					slog.Error("identity callback: get user after email exists race", "error", err)
					return
				}
			} else {
				writeError(w, http.StatusInternalServerError, "Could not create user.")
				slog.Error("identity callback: create user", "error", err)
				return
			}
		} else {
			slog.Info("created new user", "email", email, "userId", user.ID, "provider", provider)
		}
	}

	// Create session
	session, err := s.store.CreateSession(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create session.")
		slog.Error("identity callback: create session", "error", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":   session.Token,
		"userId":  user.ID,
		"email":   user.Email,
		"name":    user.Name,
		"expires": session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	user, err := s.sessionUser(r)
	if err != nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "Invalid or missing session token.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"userId": user.ID,
		"email":  user.Email,
		"name":   user.Name,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token, err := extractBearerToken(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "No session token.")
		return
	}
	session, err := s.store.GetSessionByToken(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Invalid session token.")
		return
	}
	if err := s.store.DeleteSession(r.Context(), session.ID); err != nil {
		slog.Error("logout: delete session", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not delete session.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func providerDisplayName(p oauth.Provider) string {
	switch p {
	case oauth.ProviderGitHub:
		return "GitHub"
	case oauth.ProviderGitLab:
		return "GitLab"
	case oauth.ProviderGoogle:
		return "Google"
	default:
		return string(p)
	}
}
