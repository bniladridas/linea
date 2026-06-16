package saas

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"linea/backend/internal/store"
)

type Handler struct {
	mgr *Manager
}

func NewHandler(mgr *Manager) *Handler {
	return &Handler{mgr: mgr}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/saas/users", h.createUser)
	mux.HandleFunc("GET /api/saas/users", h.listUsers)
	mux.HandleFunc("POST /api/saas/keys", h.createKey)
	mux.HandleFunc("GET /api/saas/keys", h.listKeys)
	mux.HandleFunc("DELETE /api/saas/keys/{id}", h.deleteKey)
	mux.HandleFunc("POST /api/saas/workspaces", h.createWorkspace)
	mux.HandleFunc("GET /api/saas/workspaces", h.listWorkspaces)
	mux.HandleFunc("POST /api/saas/workspaces/{id}/members", h.addWorkspaceMember)
	mux.HandleFunc("DELETE /api/saas/workspaces/{id}/members/{userId}", h.removeWorkspaceMember)
	mux.HandleFunc("GET /api/saas/workspaces/{id}/members", h.listWorkspaceMembers)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Email == "" || body.Name == "" {
		writeError(w, http.StatusBadRequest, "email and name are required")
		return
	}
	user, err := h.mgr.CreateUser(r.Context(), body.Email, body.Name)
	if err != nil {
		if errors.Is(err, ErrEmailExists) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.mgr.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) createKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID      string `json:"userId"`
		WorkspaceID string `json:"workspaceId,omitempty"`
		Name        string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.UserID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}
	key, err := h.mgr.CreateKey(r.Context(), body.UserID, body.Name, body.WorkspaceID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, key)
}

func (h *Handler) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	ws, err := h.mgr.CreateWorkspace(r.Context(), body.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ws)
}

func (h *Handler) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.mgr.ListWorkspaces(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, workspaces)
}

func (h *Handler) addWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		UserID string `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.UserID == "" {
		writeError(w, http.StatusBadRequest, "userId is required")
		return
	}
	if err := h.mgr.AddWorkspaceMember(r.Context(), id, body.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeWorkspaceMember(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	userID := r.PathValue("userId")
	if err := h.mgr.RemoveWorkspaceMember(r.Context(), id, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	members, err := h.mgr.ListWorkspaceMembers(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, members)
}

func (h *Handler) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.mgr.ListKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *Handler) deleteKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.mgr.DeleteKey(r.Context(), id); err != nil {
		if errors.Is(err, ErrKeyNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write json", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
