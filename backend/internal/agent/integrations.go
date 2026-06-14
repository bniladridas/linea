package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"linea/backend/internal/integrations/github"
)

var ErrToolNotFound = errors.New("integration tool not found")

type IntegrationServer struct {
	ServerID   string
	ServerName string
	Tools      []IntegrationTool
}

type IntegrationTool struct {
	Name        string
	Description string
	Handler     func(context.Context, map[string]any) (string, error)
	InputSchema string
}

func githubTools(tokenFn func() (string, error)) []IntegrationTool {
	schema := func(props map[string]any, required []string) string {
		s := map[string]any{
			"type":       "object",
			"properties": props,
			"required":   required,
		}
		b, _ := json.Marshal(s)
		return string(b)
	}
	strProp := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}

	return []IntegrationTool{
		{
			Name:        "github_list_issues",
			Description: "List issues for a GitHub repository",
			InputSchema: schema(map[string]any{
				"owner": strProp("Repository owner (user or org)"),
				"repo":  strProp("Repository name"),
				"state": strProp("Issue state: open, closed, all (default: open)"),
			}, []string{"owner", "repo"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn()
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo"); err != nil {
					return "", err
				}
				c := github.NewClient(token)
				issues, err := c.ListIssues(ctx, stringArg(args, "owner"), stringArg(args, "repo"), stringArg(args, "state"))
				if err != nil {
					return "", err
				}
				if len(issues) == 0 {
					return "No issues found.", nil
				}
				var out strings.Builder
				for _, issue := range issues {
					fmt.Fprintf(&out, "#%d %s [%s] by @%s\n", issue.Number, issue.Title, issue.State, issue.User.Login)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "github_get_issue",
			Description: "Get details of a specific GitHub issue",
			InputSchema: schema(map[string]any{
				"owner":  strProp("Repository owner (user or org)"),
				"repo":   strProp("Repository name"),
				"number": map[string]any{"type": "integer", "description": "Issue number"},
			}, []string{"owner", "repo", "number"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn()
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo", "number"); err != nil {
					return "", err
				}
				c := github.NewClient(token)
				issue, err := c.GetIssue(ctx, stringArg(args, "owner"), stringArg(args, "repo"), intArg(args, "number"))
				if err != nil {
					return "", err
				}
				var labels []string
				for _, l := range issue.Labels {
					labels = append(labels, l.Name)
				}
				labelStr := ""
				if len(labels) > 0 {
					labelStr = " [" + strings.Join(labels, ", ") + "]"
				}
				return fmt.Sprintf("#%d %s\nState: %s\nBy: @%s%s\n\n%s\n\n%s", issue.Number, issue.Title, issue.State, issue.User.Login, labelStr, issue.Body, issue.HTMLURL), nil
			},
		},
		{
			Name:        "github_create_issue",
			Description: "Create a new issue on a GitHub repository",
			InputSchema: schema(map[string]any{
				"owner": strProp("Repository owner (user or org)"),
				"repo":  strProp("Repository name"),
				"title": strProp("Issue title"),
				"body":  strProp("Issue body/description"),
			}, []string{"owner", "repo", "title"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn()
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo", "title"); err != nil {
					return "", err
				}
				c := github.NewClient(token)
				issue, err := c.CreateIssue(ctx, stringArg(args, "owner"), stringArg(args, "repo"), stringArg(args, "title"), stringArg(args, "body"))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Created issue #%d: %s\n%s", issue.Number, issue.Title, issue.HTMLURL), nil
			},
		},
		{
			Name:        "github_search_issues",
			Description: "Search GitHub issues and pull requests",
			InputSchema: schema(map[string]any{
				"query": strProp("GitHub search query (e.g. 'is:issue is:open repo:owner/repo label:bug')"),
			}, []string{"query"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn()
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "query"); err != nil {
					return "", err
				}
				c := github.NewClient(token)
				result, err := c.SearchIssues(ctx, stringArg(args, "query"))
				if err != nil {
					return "", err
				}
				if result.TotalCount == 0 {
					return "No results found.", nil
				}
				var out strings.Builder
				fmt.Fprintf(&out, "Found %d results:\n\n", result.TotalCount)
				for _, item := range result.Items {
					pr := ""
					if item.PullRequest != nil {
						pr = " [PR]"
					}
					fmt.Fprintf(&out, "#%d%s %s [%s]\n   %s\n\n", item.Number, pr, item.Title, item.State, item.HTMLURL)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "github_list_pull_requests",
			Description: "List pull requests for a GitHub repository",
			InputSchema: schema(map[string]any{
				"owner": strProp("Repository owner (user or org)"),
				"repo":  strProp("Repository name"),
				"state": strProp("PR state: open, closed, all (default: open)"),
			}, []string{"owner", "repo"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn()
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo"); err != nil {
					return "", err
				}
				c := github.NewClient(token)
				prs, err := c.ListPullRequests(ctx, stringArg(args, "owner"), stringArg(args, "repo"), stringArg(args, "state"))
				if err != nil {
					return "", err
				}
				if len(prs) == 0 {
					return "No pull requests found.", nil
				}
				var out strings.Builder
				for _, pr := range prs {
					draft := ""
					if pr.Draft {
						draft = " [DRAFT]"
					}
					fmt.Fprintf(&out, "#%d%s %s [%s] by @%s\n", pr.Number, draft, pr.Title, pr.State, pr.User.Login)
				}
				return out.String(), nil
			},
		},
	}
}

func requireArgs(args map[string]any, keys ...string) error {
	var missing []string
	for _, k := range keys {
		v, ok := args[k]
		if !ok || v == nil || v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required arguments: %s", strings.Join(missing, ", "))
	}
	return nil
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func NewIntegrationServer(tokenFn func() (string, error)) *IntegrationServer {
	tools := githubTools(tokenFn)
	server := &IntegrationServer{
		ServerID:   "integrations",
		ServerName: "Integrations",
	}
	for _, t := range tools {
		server.Tools = append(server.Tools, t)
	}
	return server
}

func (s *IntegrationServer) ToMCPTools() []MCPTool {
	tools := make([]MCPTool, 0, len(s.Tools))
	for _, t := range s.Tools {
		tools = append(tools, MCPTool{
			ID:          "integrations/" + t.Name,
			ServerID:    s.ServerID,
			ServerName:  s.ServerName,
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			State:       "ready",
		})
	}
	return tools
}

func (s *IntegrationServer) CallTool(ctx context.Context, toolID string, args map[string]any) (string, error) {
	for _, t := range s.Tools {
		if "integrations/"+t.Name == toolID || t.Name == toolID {
			return t.Handler(ctx, args)
		}
	}
	return "", ErrToolNotFound
}
