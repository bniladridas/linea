package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListIssues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/owner/repo/issues" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("state") != "opened" {
			t.Fatalf("unexpected state: %s", r.URL.Query().Get("state"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"id":100,"iid":5,"title":"Test Issue","state":"opened","description":"desc","web_url":"https://gitlab.com/owner/repo/-/issues/5","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","author":{"username":"testuser"},"labels":["bug"]}
		]`))
	}))
	defer ts.Close()

	c := &Client{token: "test-token", baseURL: ts.URL, client: ts.Client()}
	issues, err := c.ListIssues(context.Background(), "owner", "repo", "opened")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].IID != 5 || issues[0].Title != "Test Issue" || issues[0].State != "opened" {
		t.Fatalf("unexpected issue: %#v", issues[0])
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0] != "bug" {
		t.Fatalf("unexpected labels: %#v", issues[0].Labels)
	}
}

func TestListIssuesDefaultState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "opened" {
			t.Fatalf("expected default opened state, got %s", r.URL.Query().Get("state"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	issues, err := c.ListIssues(context.Background(), "o", "r", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues")
	}
}

func TestGetIssue(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/o/r/issues/42" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":200,"iid":42,"title":"The Answer","state":"opened","description":"meaning","web_url":"https://gitlab.com/o/r/-/issues/42","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","author":{"username":"deepthought"}}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	issue, err := c.GetIssue(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatal(err)
	}
	if issue.IID != 42 || issue.Title != "The Answer" || issue.Author.Username != "deepthought" {
		t.Fatalf("unexpected issue: %#v", issue)
	}
}

func TestCreateIssue(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/projects/o/r/issues" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":300,"iid":7,"title":"New Bug","state":"opened","web_url":"https://gitlab.com/o/r/-/issues/7","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z","author":{"username":"testuser"}}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	issue, err := c.CreateIssue(context.Background(), "o", "r", "New Bug", "Description here")
	if err != nil {
		t.Fatal(err)
	}
	if issue.IID != 7 || issue.Title != "New Bug" || issue.State != "opened" {
		t.Fatalf("unexpected issue: %#v", issue)
	}
}

func TestSearchIssues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/search") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("scope") != "issues" {
			t.Fatalf("unexpected scope: %s", r.URL.Query().Get("scope"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Total", "1")
		w.Write([]byte(`[{"id":400,"iid":8,"title":"Found Issue","state":"opened","web_url":"https://gitlab.com/o/r/-/issues/8","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z"}]`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	result, err := c.SearchIssues(context.Background(), "issue query")
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 1 {
		t.Fatalf("expected 1 result, got %d", result.TotalCount)
	}
	if result.Items[0].IID != 8 {
		t.Fatalf("expected IID 8, got %d", result.Items[0].IID)
	}
}

func TestSearchIssuesFallbackTotal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":1,"iid":1,"title":"X","state":"opened","web_url":"https://gitlab.com/o/r/-/issues/1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}]`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	result, err := c.SearchIssues(context.Background(), "query")
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 1 {
		t.Fatalf("expected fallback total count 1, got %d", result.TotalCount)
	}
}

func TestListMergeRequests(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/o/r/merge_requests" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"id":500,"iid":10,"title":"Add Feature","state":"opened","web_url":"https://gitlab.com/o/r/-/merge_requests/10","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","author":{"username":"dev1"},"draft":false,"merge_status":"can_be_merged"},
			{"id":501,"iid":11,"title":"WIP","state":"opened","web_url":"https://gitlab.com/o/r/-/merge_requests/11","created_at":"2024-01-03T00:00:00Z","updated_at":"2024-01-04T00:00:00Z","author":{"username":"dev2"},"draft":true,"merge_status":"checking"}
		]`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	mrs, err := c.ListMergeRequests(context.Background(), "o", "r", "opened")
	if err != nil {
		t.Fatal(err)
	}
	if len(mrs) != 2 {
		t.Fatalf("expected 2 MRs, got %d", len(mrs))
	}
	if mrs[0].IID != 10 || mrs[0].Draft {
		t.Fatal("unexpected first MR")
	}
	if !mrs[1].Draft {
		t.Fatal("expected second MR to be draft")
	}
}

func TestListMergeRequestsDefaultState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "opened" {
			t.Fatalf("expected default opened state")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	mrs, err := c.ListMergeRequests(context.Background(), "o", "r", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(mrs) != 0 {
		t.Fatalf("expected 0 MRs")
	}
}

func TestGetMergeRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/o/r/merge_requests/7" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":600,"iid":7,"title":"Fix Bug","state":"merged","web_url":"https://gitlab.com/o/r/-/merge_requests/7","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","author":{"username":"fixer"},"draft":false,"merge_status":"can_be_merged"}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	mr, err := c.GetMergeRequest(context.Background(), "o", "r", 7)
	if err != nil {
		t.Fatal(err)
	}
	if mr.IID != 7 || mr.Title != "Fix Bug" || mr.State != "merged" {
		t.Fatalf("unexpected MR: %#v", mr)
	}
}

func TestCreateMergeRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/projects/o/r/merge_requests" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":700,"iid":42,"title":"New MR","state":"opened","web_url":"https://gitlab.com/o/r/-/merge_requests/42","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z","author":{"username":"testuser"},"draft":false,"merge_status":"can_be_merged"}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	mr, err := c.CreateMergeRequest(context.Background(), "o", "r", "New MR", "Description", "feature", "main")
	if err != nil {
		t.Fatal(err)
	}
	if mr.IID != 42 || mr.Title != "New MR" || mr.State != "opened" {
		t.Fatalf("unexpected MR: %#v", mr)
	}
}

func TestSearchCode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/search") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("scope") != "blobs" {
			t.Fatalf("unexpected scope: %s", r.URL.Query().Get("scope"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Total", "2")
		w.Write([]byte(`[
			{"filename":"main.go","path":"cmd/server/main.go","web_url":"https://gitlab.com/o/r/-/blob/main/cmd/server/main.go","project":{"full_path":"o/r"}},
			{"filename":"api.go","path":"internal/api/api.go","web_url":"https://gitlab.com/o/r/-/blob/main/internal/api/api.go","project":{"full_path":"o/r"}}
		]`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	result, err := c.SearchCode(context.Background(), "func main")
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 2 {
		t.Fatalf("expected 2 results, got %d", result.TotalCount)
	}
	if result.Items[0].Filename != "main.go" {
		t.Fatalf("expected main.go, got %s", result.Items[0].Filename)
	}
}

func TestSearchCodeFallbackTotal(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"filename":"x.go","path":"x.go","web_url":"https://gitlab.com/o/r/-/blob/main/x.go","project":{"full_path":"o/r"}}]`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	result, err := c.SearchCode(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 1 {
		t.Fatalf("expected fallback total 1, got %d", result.TotalCount)
	}
}

func TestGetUser(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":123,"username":"testuser","name":"Test User","avatar_url":"https://gitlab.com/uploads/-/system/user/avatar/123/avatar.png"}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	user, err := c.GetUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "testuser" || user.Name != "Test User" {
		t.Fatalf("unexpected user: %#v", user)
	}
}

func TestAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"404 Not Found"}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	_, err := c.ListIssues(context.Background(), "o", "r", "opened")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 in error, got: %s", err.Error())
	}
}

func TestHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`internal error`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	_, err := c.ListMergeRequests(context.Background(), "o", "r", "opened")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient("test-token")
	if c.token != "test-token" {
		t.Fatalf("expected test-token, got %s", c.token)
	}
	if c.baseURL != "https://gitlab.com/api/v4" {
		t.Fatalf("expected default base URL, got %s", c.baseURL)
	}
}
