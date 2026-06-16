package saas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linea/backend/internal/store"
)

func newTestStore() store.Store {
	return store.NewMemoryStore()
}

func TestManagerCreateAndValidateKey(t *testing.T) {
	mgr := NewManager(newTestStore())

	// Create a user first (store FK requirement)
	user, err := mgr.CreateUser(context.Background(), "alice@test.com", "Alice")
	if err != nil {
		t.Fatalf("CreateUser error = %v", err)
	}

	ak, err := mgr.CreateKey(context.Background(), user.ID, "my-key", "")
	if err != nil {
		t.Fatalf("CreateKey error = %v", err)
	}
	if ak.ID == "" {
		t.Fatal("CreateKey returned empty ID")
	}
	if ak.Key == "" {
		t.Fatal("CreateKey returned empty key")
	}
	if !strings.HasPrefix(ak.Key, "lin_") {
		t.Fatalf("key prefix = %q, want lin_", ak.Key[:4])
	}
	if ak.UserID != user.ID || ak.Name != "my-key" {
		t.Fatalf("unexpected key: %+v", ak)
	}

	got, err := mgr.ValidateKey(context.Background(), ak.Key)
	if err != nil {
		t.Fatalf("ValidateKey error = %v", err)
	}
	if got.ID != ak.ID || got.UserID != user.ID {
		t.Fatalf("validated key = %+v, want id=%q user=%q", got, ak.ID, user.ID)
	}

	if _, err := mgr.ValidateKey(context.Background(), "wrong-key"); err != ErrKeyNotFound {
		t.Fatalf("ValidateKey(wrong) error = %v, want ErrKeyNotFound", err)
	}
}

func TestManagerListAndDeleteKey(t *testing.T) {
	mgr := NewManager(newTestStore())

	user, _ := mgr.CreateUser(context.Background(), "bob@test.com", "Bob")

	ak1, _ := mgr.CreateKey(context.Background(), user.ID, "key1", "")
	ak2, _ := mgr.CreateKey(context.Background(), user.ID, "key2", "")

	keys, err := mgr.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys error = %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("ListKeys length = %d, want 2", len(keys))
	}
	// Both keys should have the raw key blanked
	for _, k := range keys {
		if k.Key != "" {
			t.Fatalf("ListKeys returned raw key for %s", k.ID)
		}
	}

	if err := mgr.DeleteKey(context.Background(), ak1.ID); err != nil {
		t.Fatalf("DeleteKey error = %v", err)
	}

	keys, _ = mgr.ListKeys(context.Background())
	if len(keys) != 1 || keys[0].ID != ak2.ID {
		t.Fatalf("after delete: %d keys, want 1 remaining", len(keys))
	}

	if err := mgr.DeleteKey(context.Background(), "missing"); err != ErrKeyNotFound {
		t.Fatalf("DeleteKey(missing) error = %v, want ErrKeyNotFound", err)
	}
}

func TestManagerUserLifecycle(t *testing.T) {
	mgr := NewManager(newTestStore())

	u1, err := mgr.CreateUser(context.Background(), "carol@test.com", "Carol")
	if err != nil {
		t.Fatalf("CreateUser error = %v", err)
	}
	if u1.Email != "carol@test.com" || u1.Name != "Carol" {
		t.Fatalf("unexpected user: %+v", u1)
	}

	_, err = mgr.CreateUser(context.Background(), "dave@test.com", "Dave")
	if err != nil {
		t.Fatalf("CreateUser error = %v", err)
	}

	users, err := mgr.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListUsers length = %d, want 2", len(users))
	}

	got, err := mgr.GetUser(context.Background(), u1.ID)
	if err != nil {
		t.Fatalf("GetUser error = %v", err)
	}
	if got.ID != u1.ID || got.Email != "carol@test.com" {
		t.Fatalf("GetUser = %+v", got)
	}

	if _, err := mgr.GetUser(context.Background(), "missing"); err != store.ErrNotFound {
		t.Fatalf("GetUser(missing) error = %v, want ErrNotFound", err)
	}

	if _, err := mgr.CreateUser(context.Background(), "carol@test.com", "Carol 2"); err != ErrEmailExists {
		t.Fatalf("CreateUser(duplicate) error = %v, want ErrEmailExists", err)
	}
}

func TestManagerAdminKey(t *testing.T) {
	mgr := NewManager(newTestStore())

	if err := mgr.ValidateAdminKey("anything"); err != ErrUnauthorized {
		t.Fatalf("ValidateAdminKey before SetAdminKey error = %v, want ErrUnauthorized", err)
	}
	if err := mgr.ValidateAdminKey(""); err != ErrUnauthorized {
		t.Fatalf("ValidateAdminKey(empty) error = %v, want ErrUnauthorized", err)
	}

	mgr.SetAdminKey("my-admin-secret")

	if err := mgr.ValidateAdminKey("my-admin-secret"); err != nil {
		t.Fatalf("ValidateAdminKey(correct) error = %v", err)
	}
	if err := mgr.ValidateAdminKey("wrong"); err != ErrUnauthorized {
		t.Fatalf("ValidateAdminKey(wrong) error = %v, want ErrUnauthorized", err)
	}
}

func TestHandlerUserEndpoints(t *testing.T) {
	mgr := NewManager(newTestStore())
	h := NewHandler(mgr)
	mux := http.NewServeMux()
	h.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Create user
	resp, err := http.Post(ts.URL+"/api/saas/users", "application/json", strings.NewReader(`{"email":"eve@test.com","name":"Eve"}`))
	if err != nil {
		t.Fatalf("POST user error = %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST user status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode user error = %v", err)
	}
	resp.Body.Close()
	if created.ID == "" || created.Email != "eve@test.com" || created.Name != "Eve" {
		t.Fatalf("created user = %+v", created)
	}

	// List users
	resp, err = http.Get(ts.URL + "/api/saas/users")
	if err != nil {
		t.Fatalf("GET users error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET users status = %d, want 200", resp.StatusCode)
	}
	var users []any
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		t.Fatalf("decode users error = %v", err)
	}
	resp.Body.Close()
	if len(users) != 1 {
		t.Fatalf("users length = %d, want 1", len(users))
	}

	// Missing fields
	resp, err = http.Post(ts.URL+"/api/saas/users", "application/json", strings.NewReader(`{"email":""}`))
	if err != nil {
		t.Fatalf("POST user (bad) error = %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST user (bad) status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// Duplicate email
	resp, err = http.Post(ts.URL+"/api/saas/users", "application/json", strings.NewReader(`{"email":"eve@test.com","name":"Eve 2"}`))
	if err != nil {
		t.Fatalf("POST user (dup) error = %v", err)
	}
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST user (dup) status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestHandlerKeyEndpoints(t *testing.T) {
	mgr := NewManager(newTestStore())
	h := NewHandler(mgr)
	mux := http.NewServeMux()
	h.Register(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Create user first
	user, _ := mgr.CreateUser(context.Background(), "frank@test.com", "Frank")

	// Create key
	body := `{"userId":"` + user.ID + `","name":"frank-key"}`
	resp, err := http.Post(ts.URL+"/api/saas/keys", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST key error = %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST key status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		ID     string `json:"id"`
		Key    string `json:"key"`
		UserID string `json:"userId"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode key error = %v", err)
	}
	resp.Body.Close()
	if created.ID == "" || created.Key == "" || created.UserID != user.ID || created.Name != "frank-key" {
		t.Fatalf("created key = %+v", created)
	}

	// List keys
	resp, err = http.Get(ts.URL + "/api/saas/keys")
	if err != nil {
		t.Fatalf("GET keys error = %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET keys status = %d, want 200", resp.StatusCode)
	}
	var keys []any
	if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
		t.Fatalf("decode keys error = %v", err)
	}
	resp.Body.Close()
	if len(keys) != 1 {
		t.Fatalf("keys length = %d, want 1", len(keys))
	}

	// Missing userId
	resp, err = http.Post(ts.URL+"/api/saas/keys", "application/json", strings.NewReader(`{"name":"bad"}`))
	if err != nil {
		t.Fatalf("POST key (bad) error = %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST key (bad) status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	// Delete key
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/saas/keys/"+created.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE key error = %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE key status = %d, want 204", resp.StatusCode)
	}
	resp.Body.Close()

	// Delete missing
	req, _ = http.NewRequest(http.MethodDelete, ts.URL+"/api/saas/keys/missing", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE key (missing) error = %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE key (missing) status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify key is usable via ValidateKey
	if _, err := mgr.ValidateKey(context.Background(), created.Key); err != ErrKeyNotFound {
		t.Fatalf("ValidateKey after delete error = %v, want ErrKeyNotFound", err)
	}
}

func TestMiddlewareV1Auth(t *testing.T) {
	mgr := NewManager(newTestStore())
	user, _ := mgr.CreateUser(context.Background(), "grace@test.com", "Grace")
	ak, _ := mgr.CreateKey(context.Background(), user.ID, "grace-key", "")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := store.UserIDFromContext(r.Context())
		if uid != user.ID {
			t.Fatalf("inner handler got userID = %q, want %q", uid, user.ID)
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := Middleware(mgr, http.NotFoundHandler(), inner)

	t.Run("valid key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
		req.Header.Set("Authorization", "Bearer "+ak.Key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})

	t.Run("no auth header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})
}

func TestMiddlewareSkipsSaasRoutes(t *testing.T) {
	mgr := NewManager(newTestStore())
	var called bool
	adminHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := Middleware(mgr, adminHandler, http.NotFoundHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/saas/users", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if !called {
		t.Fatal("adminHandler was not called for /api/saas/ route")
	}
}

func TestHandlerRegisterPublic(t *testing.T) {
	mgr := NewManager(newTestStore())
	h := NewHandler(mgr)
	mux := http.NewServeMux()
	h.RegisterPublic(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	t.Run("success creates user and api key", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", strings.NewReader(`{"email":"reg@test.com","name":"Reginald"}`))
		if err != nil {
			t.Fatalf("POST register error = %v", err)
		}
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201", resp.StatusCode)
		}
		var result RegisterResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode error = %v", err)
		}
		resp.Body.Close()
		if result.User.ID == "" || result.User.Email != "reg@test.com" || result.User.Name != "Reginald" {
			t.Fatalf("user = %+v", result.User)
		}
		if result.APIKey.ID == "" || result.APIKey.Key == "" || result.APIKey.UserID != result.User.ID {
			t.Fatalf("apiKey = %+v", result.APIKey)
		}
		// Key must be usable
		got, err := mgr.ValidateKey(context.Background(), result.APIKey.Key)
		if err != nil {
			t.Fatalf("ValidateKey error = %v", err)
		}
		if got.UserID != result.User.ID {
			t.Fatalf("key user = %q, want %q", got.UserID, result.User.ID)
		}
	})

	t.Run("empty name defaults to email", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", strings.NewReader(`{"email":"noname@test.com"}`))
		if err != nil {
			t.Fatalf("POST register error = %v", err)
		}
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("status = %d, want 201", resp.StatusCode)
		}
		var result RegisterResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode error = %v", err)
		}
		resp.Body.Close()
		if result.User.Name != "noname@test.com" {
			t.Fatalf("name = %q, want email fallback", result.User.Name)
		}
	})

	t.Run("missing email returns 400", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", strings.NewReader(`{"name":"No Email"}`))
		if err != nil {
			t.Fatalf("POST register error = %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("empty email returns 400", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", strings.NewReader(`{"email":"","name":"Empty"}`))
		if err != nil {
			t.Fatalf("POST register error = %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("duplicate email returns 409", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", strings.NewReader(`{"email":"reg@test.com","name":"Duplicate"}`))
		if err != nil {
			t.Fatalf("POST register error = %v", err)
		}
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d, want 409", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("invalid json returns 400", func(t *testing.T) {
		resp, err := http.Post(ts.URL+"/api/v1/auth/register", "application/json", strings.NewReader(`not json`))
		if err != nil {
			t.Fatalf("POST register error = %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
		resp.Body.Close()
	})
}

func TestMiddlewareSkipsAuthRegisterPath(t *testing.T) {
	mgr := NewManager(newTestStore())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := Middleware(mgr, http.NotFoundHandler(), inner)

	t.Run("register path bypasses auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (bypass auth)", w.Code)
		}
	})

	t.Run("other auth paths still require auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})
}

func TestAdminMiddleware(t *testing.T) {
	mgr := NewManager(newTestStore())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("no admin key configured", func(t *testing.T) {
		handler := AdminMiddleware(mgr, inner)
		req := httptest.NewRequest(http.MethodGet, "/api/saas/users", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 when no admin key set", w.Code)
		}
	})

	mgr.SetAdminKey("admin-secret")

	t.Run("no auth header", func(t *testing.T) {
		handler := AdminMiddleware(mgr, inner)
		req := httptest.NewRequest(http.MethodGet, "/api/saas/users", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		handler := AdminMiddleware(mgr, inner)
		req := httptest.NewRequest(http.MethodGet, "/api/saas/users", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", w.Code)
		}
	})

	t.Run("correct key", func(t *testing.T) {
		handler := AdminMiddleware(mgr, inner)
		req := httptest.NewRequest(http.MethodGet, "/api/saas/users", nil)
		req.Header.Set("Authorization", "Bearer admin-secret")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})
}
