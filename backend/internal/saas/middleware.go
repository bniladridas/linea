package saas

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"linea/backend/internal/store"
)

func Middleware(mgr *Manager, admin http.Handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/saas" || strings.HasPrefix(r.URL.Path, "/api/saas/") {
			admin.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/v1/auth/register" || r.URL.Path == "/api/v1/auth/register/" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"missing authorization"}` + "\n"))
			return
		}
		key := strings.TrimSpace(auth[len("Bearer "):])
		if key == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"missing authorization"}` + "\n"))
			return
		}

		ak, err := mgr.ValidateKey(r.Context(), key)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				slog.Warn("saas: invalid api key", "remote", r.RemoteAddr)
				w.Header().Set("WWW-Authenticate", "Bearer")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"invalid api key"}` + "\n"))
			} else {
				slog.Error("saas: validate key", "error", err, "remote", r.RemoteAddr)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"internal error"}` + "\n"))
			}
			return
		}

		ctx := store.WithUserID(r.Context(), ak.UserID)
		if ak.WorkspaceID != "" {
			ctx = store.WithWorkspaceID(ctx, ak.WorkspaceID)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func AdminMiddleware(mgr *Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mgr.getAdminKey() == "" {
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"missing authorization"}` + "\n"))
			return
		}
		key := strings.TrimSpace(auth[len("Bearer "):])
		if key == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"missing authorization"}` + "\n"))
			return
		}
		if err := mgr.ValidateAdminKey(key); err != nil {
			slog.Warn("saas: invalid admin key", "remote", r.RemoteAddr)
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
