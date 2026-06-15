package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	token   string
	baseURL string
	client  *http.Client
}

type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Mergeable *bool `json:"mergeable"`
	Draft     bool  `json:"draft"`
}

type SearchResult struct {
	TotalCount int `json:"total_count"`
	Items      []struct {
		Number      int       `json:"number"`
		Title       string    `json:"title"`
		State       string    `json:"state"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
		CreatedAt   time.Time `json:"created_at"`
		UpdatedAt   time.Time `json:"updated_at"`
		PullRequest *struct{} `json:"pull_request"`
	} `json:"items"`
}

type Repo struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	HTMLURL     string `json:"html_url"`
	Private     bool   `json:"private"`
	Fork        bool   `json:"fork"`
}

type CodeSearchResult struct {
	TotalCount int `json:"total_count"`
	Items      []struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		HTMLURL    string `json:"html_url"`
		Repository Repo   `json:"repository"`
	} `json:"items"`
}

func NewClient(token string) *Client {
	return &Client{
		token:   token,
		baseURL: "https://api.github.com",
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, path string, out any) error {
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "linea")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("github api: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (c *Client) doPost(ctx context.Context, path string, payload, out any) error {
	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = strings.NewReader(string(data))
	}
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, "POST", u, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "linea")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("github api: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (c *Client) ListIssues(ctx context.Context, owner, repo string, state string) ([]Issue, error) {
	if state == "" {
		state = "open"
	}
	var issues []Issue
	return issues, c.do(ctx, fmt.Sprintf("/repos/%s/%s/issues?state=%s&per_page=20", owner, repo, url.QueryEscape(state)), &issues)
}

func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (Issue, error) {
	var issue Issue
	return issue, c.do(ctx, fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number), &issue)
}

func (c *Client) CreateIssue(ctx context.Context, owner, repo, title, body string) (Issue, error) {
	var issue Issue
	payload := map[string]string{"title": title, "body": body}
	return issue, c.doPost(ctx, fmt.Sprintf("/repos/%s/%s/issues", owner, repo), payload, &issue)
}

func (c *Client) SearchIssues(ctx context.Context, query string) (SearchResult, error) {
	var result SearchResult
	return result, c.do(ctx, fmt.Sprintf("/search/issues?q=%s&per_page=10", url.QueryEscape(query)), &result)
}

func (c *Client) ListPullRequests(ctx context.Context, owner, repo string, state string) ([]PullRequest, error) {
	if state == "" {
		state = "open"
	}
	var prs []PullRequest
	return prs, c.do(ctx, fmt.Sprintf("/repos/%s/%s/pulls?state=%s&per_page=20", owner, repo, url.QueryEscape(state)), &prs)
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequest, error) {
	var pr PullRequest
	return pr, c.do(ctx, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number), &pr)
}

func (c *Client) CreatePR(ctx context.Context, owner, repo, title, body, head, base string) (PullRequest, error) {
	var pr PullRequest
	payload := map[string]any{"title": title, "body": body, "head": head, "base": base}
	return pr, c.doPost(ctx, fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), payload, &pr)
}

func (c *Client) SearchCode(ctx context.Context, query string) (CodeSearchResult, error) {
	var result CodeSearchResult
	return result, c.do(ctx, fmt.Sprintf("/search/code?q=%s&per_page=10", url.QueryEscape(query)), &result)
}

func (c *Client) GetUser(ctx context.Context) (struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}, error) {
	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	return user, c.do(ctx, "/user", &user)
}
