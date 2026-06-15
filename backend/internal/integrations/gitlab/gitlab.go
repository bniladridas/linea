package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	token   string
	baseURL string
	client  *http.Client
}

type Issue struct {
	ID          int       `json:"id"`
	IID         int       `json:"iid"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	WebURL      string    `json:"web_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Author      struct {
		Username string `json:"username"`
	} `json:"author"`
	Labels []string `json:"labels"`
}

type MergeRequest struct {
	ID          int       `json:"id"`
	IID         int       `json:"iid"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	WebURL      string    `json:"web_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Author      struct {
		Username string `json:"username"`
	} `json:"author"`
	Draft       bool   `json:"draft"`
	MergeStatus string `json:"merge_status"`
}

type SearchResultItem struct {
	ID          int       `json:"id"`
	IID         int       `json:"iid"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	Description string    `json:"description"`
	WebURL      string    `json:"web_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SearchResult struct {
	TotalCount int
	Items      []SearchResultItem
}

type CodeSearchResultItem struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
	WebURL   string `json:"web_url"`
	Project  struct {
		FullPath string `json:"full_path"`
	} `json:"project"`
}

type CodeSearchResult struct {
	TotalCount int
	Items      []CodeSearchResultItem
}

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func NewClient(token string) *Client {
	return &Client{
		token:   token,
		baseURL: "https://gitlab.com/api/v4",
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func projectPath(owner, repo string) string {
	return url.PathEscape(owner + "/" + repo)
}

func (c *Client) do(ctx context.Context, path string, out any) error {
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
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
		return fmt.Errorf("gitlab api: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
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
	req.Header.Set("Accept", "application/json")
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
		return fmt.Errorf("gitlab api: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func (c *Client) searchRequest(ctx context.Context, path string, out any) (int, error) {
	u := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "linea")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("gitlab api: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	total := 0
	if t := resp.Header.Get("X-Total"); t != "" {
		total, _ = strconv.Atoi(t)
	}

	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return 0, err
		}
	}
	return total, nil
}

func (c *Client) ListIssues(ctx context.Context, owner, repo string, state string) ([]Issue, error) {
	if state == "" {
		state = "opened"
	}
	var issues []Issue
	return issues, c.do(ctx, fmt.Sprintf("/projects/%s/issues?state=%s&per_page=20", projectPath(owner, repo), url.QueryEscape(state)), &issues)
}

func (c *Client) GetIssue(ctx context.Context, owner, repo string, id int) (Issue, error) {
	var issue Issue
	return issue, c.do(ctx, fmt.Sprintf("/projects/%s/issues/%d", projectPath(owner, repo), id), &issue)
}

func (c *Client) CreateIssue(ctx context.Context, owner, repo, title, description string) (Issue, error) {
	var issue Issue
	payload := map[string]string{"title": title, "description": description}
	return issue, c.doPost(ctx, fmt.Sprintf("/projects/%s/issues", projectPath(owner, repo)), payload, &issue)
}

func (c *Client) SearchIssues(ctx context.Context, query string) (SearchResult, error) {
	var items []SearchResultItem
	total, err := c.searchRequest(ctx, fmt.Sprintf("/search?scope=issues&search=%s", url.QueryEscape(query)), &items)
	if err != nil {
		return SearchResult{}, err
	}
	if total == 0 {
		total = len(items)
	}
	return SearchResult{TotalCount: total, Items: items}, nil
}

func (c *Client) ListMergeRequests(ctx context.Context, owner, repo string, state string) ([]MergeRequest, error) {
	if state == "" {
		state = "opened"
	}
	var mrs []MergeRequest
	return mrs, c.do(ctx, fmt.Sprintf("/projects/%s/merge_requests?state=%s&per_page=20", projectPath(owner, repo), url.QueryEscape(state)), &mrs)
}

func (c *Client) GetMergeRequest(ctx context.Context, owner, repo string, id int) (MergeRequest, error) {
	var mr MergeRequest
	return mr, c.do(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d", projectPath(owner, repo), id), &mr)
}

func (c *Client) CreateMergeRequest(ctx context.Context, owner, repo, title, description, sourceBranch, targetBranch string) (MergeRequest, error) {
	var mr MergeRequest
	payload := map[string]string{"title": title, "description": description, "source_branch": sourceBranch, "target_branch": targetBranch}
	return mr, c.doPost(ctx, fmt.Sprintf("/projects/%s/merge_requests", projectPath(owner, repo)), payload, &mr)
}

func (c *Client) SearchCode(ctx context.Context, query string) (CodeSearchResult, error) {
	var items []CodeSearchResultItem
	total, err := c.searchRequest(ctx, fmt.Sprintf("/search?scope=blobs&search=%s", url.QueryEscape(query)), &items)
	if err != nil {
		return CodeSearchResult{}, err
	}
	if total == 0 {
		total = len(items)
	}
	return CodeSearchResult{TotalCount: total, Items: items}, nil
}

func (c *Client) GetUser(ctx context.Context) (User, error) {
	var user User
	return user, c.do(ctx, "/user", &user)
}
