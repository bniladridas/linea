package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListIssues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/issues" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("state") != "open" {
			t.Fatalf("unexpected state: %s", r.URL.Query().Get("state"))
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"number":1,"title":"Test Issue","state":"open","html_url":"https://github.com/owner/repo/issues/1","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","user":{"login":"testuser"},"labels":[{"name":"bug"}]}
		]`))
	}))
	defer ts.Close()

	c := &Client{token: "test-token", baseURL: ts.URL, client: ts.Client()}
	issues, err := c.ListIssues(context.Background(), "owner", "repo", "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Number != 1 || issues[0].Title != "Test Issue" || issues[0].State != "open" {
		t.Fatalf("unexpected issue: %#v", issues[0])
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0].Name != "bug" {
		t.Fatalf("unexpected labels: %#v", issues[0].Labels)
	}
}

func TestListIssuesDefaultState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "open" {
			t.Fatalf("expected default open state, got %s", r.URL.Query().Get("state"))
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
		if r.URL.Path != "/repos/o/r/issues/42" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"number":42,"title":"The Answer","state":"closed","html_url":"https://github.com/o/r/issues/42","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","user":{"login":"deepthought"}}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	issue, err := c.GetIssue(context.Background(), "o", "r", 42)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Number != 42 || issue.Title != "The Answer" || issue.User.Login != "deepthought" {
		t.Fatalf("unexpected issue: %#v", issue)
	}
}

func TestCreateIssue(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/repos/o/r/issues" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"number":3,"title":"New Bug","state":"open","html_url":"https://github.com/o/r/issues/3","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z","user":{"login":"testuser"}}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	issue, err := c.CreateIssue(context.Background(), "o", "r", "New Bug", "Description here")
	if err != nil {
		t.Fatal(err)
	}
	if issue.Number != 3 || issue.Title != "New Bug" || issue.State != "open" {
		t.Fatalf("unexpected issue: %#v", issue)
	}
}

func TestSearchIssues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/search/issues") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"total_count":1,"items":[{"number":5,"title":"Found Issue","state":"open","html_url":"https://github.com/o/r/issues/5","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","pull_request":null}]}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	result, err := c.SearchIssues(context.Background(), "is:issue repo:o/r")
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 1 {
		t.Fatalf("expected 1 result, got %d", result.TotalCount)
	}
	if result.Items[0].Number != 5 {
		t.Fatalf("expected issue #5, got #%d", result.Items[0].Number)
	}
	if result.Items[0].PullRequest != nil {
		t.Fatal("expected nil pull_request for issue")
	}
}

func TestSearchIssuesWithPR(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"total_count":1,"items":[{"number":10,"title":"PR Item","state":"open","html_url":"https://github.com/o/r/pull/10","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","pull_request":{}}]}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	result, err := c.SearchIssues(context.Background(), "is:pr")
	if err != nil {
		t.Fatal(err)
	}
	if result.Items[0].PullRequest == nil {
		t.Fatal("expected non-nil pull_request for PR")
	}
}

func TestListPullRequests(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{"number":10,"title":"Add Feature","state":"open","html_url":"https://github.com/o/r/pull/10","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","user":{"login":"dev1"},"draft":false,"mergeable":true},
			{"number":11,"title":"WIP","state":"open","html_url":"https://github.com/o/r/pull/11","created_at":"2024-01-03T00:00:00Z","updated_at":"2024-01-04T00:00:00Z","user":{"login":"dev2"},"draft":true,"mergeable":null}
		]`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	prs, err := c.ListPullRequests(context.Background(), "o", "r", "open")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Fatalf("expected 2 PRs, got %d", len(prs))
	}
	if prs[0].Draft {
		t.Fatal("expected first PR not draft")
	}
	if !prs[1].Draft {
		t.Fatal("expected second PR to be draft")
	}
	if prs[0].Mergeable == nil || !*prs[0].Mergeable {
		t.Fatal("expected first PR mergeable")
	}
	if prs[1].Mergeable != nil {
		t.Fatal("expected second PR mergeable to be nil")
	}
}

func TestListPullRequestsDefaultState(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "open" {
			t.Fatalf("expected default open state")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	prs, err := c.ListPullRequests(context.Background(), "o", "r", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 0 {
		t.Fatalf("expected 0 PRs")
	}
}

func TestGetPullRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls/7" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"number":7,"title":"Fix Bug","state":"closed","html_url":"https://github.com/o/r/pull/7","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-02T00:00:00Z","user":{"login":"fixer"},"draft":false,"mergeable":true}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	pr, err := c.GetPullRequest(context.Background(), "o", "r", 7)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 7 || pr.Title != "Fix Bug" || pr.State != "closed" {
		t.Fatalf("unexpected PR: %#v", pr)
	}
}

func TestGetUser(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"login":"testuser","name":"Test User","avatar_url":"https://avatars.githubusercontent.com/u/1"}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	user, err := c.GetUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user.Login != "testuser" || user.Name != "Test User" {
		t.Fatalf("unexpected user: %#v", user)
	}
}

func TestAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found","documentation_url":"https://docs.github.com/rest"}`))
	}))
	defer ts.Close()

	c := &Client{token: "t", baseURL: ts.URL, client: ts.Client()}
	_, err := c.ListIssues(context.Background(), "o", "r", "open")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 in error, got: %s", err.Error())
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient("test-token")
	if c.token != "test-token" {
		t.Fatalf("expected test-token, got %s", c.token)
	}
	if c.baseURL != "https://api.github.com" {
		t.Fatalf("expected default base URL, got %s", c.baseURL)
	}
}
