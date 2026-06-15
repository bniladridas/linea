package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"linea/backend/internal/integrations/github"
	"linea/backend/internal/integrations/gitlab"
	"linea/backend/internal/integrations/google"
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

func githubTools(tokenFn func(string) (string, error)) []IntegrationTool {
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
				token, err := tokenFn("github")
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
				token, err := tokenFn("github")
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
				token, err := tokenFn("github")
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
				token, err := tokenFn("github")
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
				token, err := tokenFn("github")
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
		{
			Name:        "github_get_pull_request",
			Description: "Get details of a specific GitHub pull request",
			InputSchema: schema(map[string]any{
				"owner":  strProp("Repository owner (user or org)"),
				"repo":   strProp("Repository name"),
				"number": map[string]any{"type": "integer", "description": "Pull request number"},
			}, []string{"owner", "repo", "number"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("github")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo", "number"); err != nil {
					return "", err
				}
				c := github.NewClient(token)
				pr, err := c.GetPullRequest(ctx, stringArg(args, "owner"), stringArg(args, "repo"), intArg(args, "number"))
				if err != nil {
					return "", err
				}
				draft := ""
				if pr.Draft {
					draft = " [DRAFT]"
				}
				mergeable := "unknown"
				if pr.Mergeable != nil {
					if *pr.Mergeable {
						mergeable = "yes"
					} else {
						mergeable = "no"
					}
				}
				return fmt.Sprintf("#%d%s %s [%s]\nBy: @%s\nMergeable: %s\n\n%s\n\n%s", pr.Number, draft, pr.Title, pr.State, pr.User.Login, mergeable, pr.Body, pr.HTMLURL), nil
			},
		},
		{
			Name:        "github_create_pull_request",
			Description: "Create a pull request on a GitHub repository",
			InputSchema: schema(map[string]any{
				"owner": strProp("Repository owner (user or org)"),
				"repo":  strProp("Repository name"),
				"title": strProp("Pull request title"),
				"body":  strProp("Pull request body/description"),
				"head":  strProp("The name of the branch where changes are implemented"),
				"base":  strProp("The name of the branch you want the changes pulled into"),
			}, []string{"owner", "repo", "title", "head", "base"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("github")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo", "title", "head", "base"); err != nil {
					return "", err
				}
				c := github.NewClient(token)
				pr, err := c.CreatePR(ctx, stringArg(args, "owner"), stringArg(args, "repo"), stringArg(args, "title"), stringArg(args, "body"), stringArg(args, "head"), stringArg(args, "base"))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Created PR #%d: %s\n%s", pr.Number, pr.Title, pr.HTMLURL), nil
			},
		},
		{
			Name:        "github_search_code",
			Description: "Search code across GitHub repositories",
			InputSchema: schema(map[string]any{
				"query": strProp("GitHub code search query (e.g. 'function repo:owner/repo')"),
			}, []string{"query"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("github")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "query"); err != nil {
					return "", err
				}
				c := github.NewClient(token)
				result, err := c.SearchCode(ctx, stringArg(args, "query"))
				if err != nil {
					return "", err
				}
				if result.TotalCount == 0 {
					return "No code results found.", nil
				}
				var out strings.Builder
				fmt.Fprintf(&out, "Found %d results:\n\n", result.TotalCount)
				for _, item := range result.Items {
					fmt.Fprintf(&out, "%s/%s\n   %s\n\n", item.Repository.FullName, item.Path, item.HTMLURL)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "github_get_user",
			Description: "Get details of the authenticated GitHub user",
			InputSchema: schema(nil, nil),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("github")
				if err != nil {
					return "", err
				}
				c := github.NewClient(token)
				user, err := c.GetUser(ctx)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("@%s (%s)\nID: %d\nAvatar: %s", user.Login, user.Name, user.ID, user.AvatarURL), nil
			},
		},
	}
}

func gitlabTools(tokenFn func(string) (string, error)) []IntegrationTool {
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
			Name:        "gitlab_list_issues",
			Description: "List issues for a GitLab project",
			InputSchema: schema(map[string]any{
				"owner": strProp("Project owner (user or group)"),
				"repo":  strProp("Project name"),
				"state": strProp("Issue state: opened, closed, all (default: opened)"),
			}, []string{"owner", "repo"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("gitlab")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo"); err != nil {
					return "", err
				}
				c := gitlab.NewClient(token)
				issues, err := c.ListIssues(ctx, stringArg(args, "owner"), stringArg(args, "repo"), stringArg(args, "state"))
				if err != nil {
					return "", err
				}
				if len(issues) == 0 {
					return "No issues found.", nil
				}
				var out strings.Builder
				for _, issue := range issues {
					fmt.Fprintf(&out, "!%d %s [%s] by @%s\n", issue.IID, issue.Title, issue.State, issue.Author.Username)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "gitlab_get_issue",
			Description: "Get details of a specific GitLab issue",
			InputSchema: schema(map[string]any{
				"owner":  strProp("Project owner (user or group)"),
				"repo":   strProp("Project name"),
				"id":     map[string]any{"type": "integer", "description": "Issue IID (project-level issue number)"},
			}, []string{"owner", "repo", "id"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("gitlab")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo", "id"); err != nil {
					return "", err
				}
				c := gitlab.NewClient(token)
				issue, err := c.GetIssue(ctx, stringArg(args, "owner"), stringArg(args, "repo"), intArg(args, "id"))
				if err != nil {
					return "", err
				}
				labelStr := ""
				if len(issue.Labels) > 0 {
					labelStr = " [" + strings.Join(issue.Labels, ", ") + "]"
				}
				return fmt.Sprintf("!%d %s\nState: %s\nBy: @%s%s\n\n%s\n\n%s", issue.IID, issue.Title, issue.State, issue.Author.Username, labelStr, issue.Description, issue.WebURL), nil
			},
		},
		{
			Name:        "gitlab_create_issue",
			Description: "Create a new issue on a GitLab project",
			InputSchema: schema(map[string]any{
				"owner":       strProp("Project owner (user or group)"),
				"repo":        strProp("Project name"),
				"title":       strProp("Issue title"),
				"description": strProp("Issue description/body"),
			}, []string{"owner", "repo", "title"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("gitlab")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo", "title"); err != nil {
					return "", err
				}
				c := gitlab.NewClient(token)
				issue, err := c.CreateIssue(ctx, stringArg(args, "owner"), stringArg(args, "repo"), stringArg(args, "title"), stringArg(args, "description"))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Created issue !%d: %s\n%s", issue.IID, issue.Title, issue.WebURL), nil
			},
		},
		{
			Name:        "gitlab_search_issues",
			Description: "Search GitLab issues",
			InputSchema: schema(map[string]any{
				"query": strProp("GitLab search query"),
			}, []string{"query"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("gitlab")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "query"); err != nil {
					return "", err
				}
				c := gitlab.NewClient(token)
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
					fmt.Fprintf(&out, "!%d %s [%s]\n   %s\n\n", item.IID, item.Title, item.State, item.WebURL)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "gitlab_list_merge_requests",
			Description: "List merge requests for a GitLab project",
			InputSchema: schema(map[string]any{
				"owner": strProp("Project owner (user or group)"),
				"repo":  strProp("Project name"),
				"state": strProp("MR state: opened, closed, merged, all (default: opened)"),
			}, []string{"owner", "repo"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("gitlab")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo"); err != nil {
					return "", err
				}
				c := gitlab.NewClient(token)
				mrs, err := c.ListMergeRequests(ctx, stringArg(args, "owner"), stringArg(args, "repo"), stringArg(args, "state"))
				if err != nil {
					return "", err
				}
				if len(mrs) == 0 {
					return "No merge requests found.", nil
				}
				var out strings.Builder
				for _, mr := range mrs {
					draft := ""
					if mr.Draft {
						draft = " [DRAFT]"
					}
					fmt.Fprintf(&out, "!%d%s %s [%s] by @%s\n", mr.IID, draft, mr.Title, mr.State, mr.Author.Username)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "gitlab_get_merge_request",
			Description: "Get details of a specific GitLab merge request",
			InputSchema: schema(map[string]any{
				"owner":  strProp("Project owner (user or group)"),
				"repo":   strProp("Project name"),
				"id":     map[string]any{"type": "integer", "description": "MR IID (project-level MR number)"},
			}, []string{"owner", "repo", "id"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("gitlab")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo", "id"); err != nil {
					return "", err
				}
				c := gitlab.NewClient(token)
				mr, err := c.GetMergeRequest(ctx, stringArg(args, "owner"), stringArg(args, "repo"), intArg(args, "id"))
				if err != nil {
					return "", err
				}
				draft := ""
				if mr.Draft {
					draft = " [DRAFT]"
				}
				return fmt.Sprintf("!%d%s %s [%s]\nBy: @%s\nMerge status: %s\n\n%s\n\n%s", mr.IID, draft, mr.Title, mr.State, mr.Author.Username, mr.MergeStatus, mr.Description, mr.WebURL), nil
			},
		},
		{
			Name:        "gitlab_create_merge_request",
			Description: "Create a merge request on a GitLab project",
			InputSchema: schema(map[string]any{
				"owner":         strProp("Project owner (user or group)"),
				"repo":          strProp("Project name"),
				"title":         strProp("Merge request title"),
				"description":   strProp("Merge request description/body"),
				"source_branch": strProp("The name of the source branch"),
				"target_branch": strProp("The name of the target branch"),
			}, []string{"owner", "repo", "title", "source_branch", "target_branch"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("gitlab")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "owner", "repo", "title", "source_branch", "target_branch"); err != nil {
					return "", err
				}
				c := gitlab.NewClient(token)
				mr, err := c.CreateMergeRequest(ctx, stringArg(args, "owner"), stringArg(args, "repo"), stringArg(args, "title"), stringArg(args, "description"), stringArg(args, "source_branch"), stringArg(args, "target_branch"))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Created MR !%d: %s\n%s", mr.IID, mr.Title, mr.WebURL), nil
			},
		},
		{
			Name:        "gitlab_search_code",
			Description: "Search code across GitLab projects",
			InputSchema: schema(map[string]any{
				"query": strProp("GitLab code search query"),
			}, []string{"query"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("gitlab")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "query"); err != nil {
					return "", err
				}
				c := gitlab.NewClient(token)
				result, err := c.SearchCode(ctx, stringArg(args, "query"))
				if err != nil {
					return "", err
				}
				if result.TotalCount == 0 {
					return "No code results found.", nil
				}
				var out strings.Builder
				fmt.Fprintf(&out, "Found %d results:\n\n", result.TotalCount)
				for _, item := range result.Items {
					fmt.Fprintf(&out, "%s/%s\n   %s\n\n", item.Project.FullPath, item.Path, item.WebURL)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "gitlab_get_user",
			Description: "Get details of the authenticated GitLab user",
			InputSchema: schema(nil, nil),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("gitlab")
				if err != nil {
					return "", err
				}
				c := gitlab.NewClient(token)
				user, err := c.GetUser(ctx)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("@%s (%s)\nID: %d\nAvatar: %s", user.Username, user.Name, user.ID, user.AvatarURL), nil
			},
		},
	}
}

func googleTools(tokenFn func(string) (string, error)) []IntegrationTool {
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
			Name:        "gmail_list_threads",
			Description: "List Gmail threads, optionally filtered by query",
			InputSchema: schema(map[string]any{
				"query":      strProp("Gmail search query (optional, e.g. 'from:user@example.com after:2024/1/1')"),
				"maxResults": map[string]any{"type": "integer", "description": "Maximum number of threads to return (default 20)"},
			}, nil),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("google")
				if err != nil {
					return "", err
				}
				c := google.NewClient(token)
				maxResults := 20
				if v, ok := args["maxResults"]; ok {
					switch n := v.(type) {
					case float64:
						maxResults = int(n)
					}
				}
				resp, err := c.ListThreads(ctx, stringArg(args, "query"), maxResults)
				if err != nil {
					return "", err
				}
				if len(resp.Threads) == 0 {
					return "No threads found.", nil
				}
				var out strings.Builder
				fmt.Fprintf(&out, "Found %d threads:\n\n", len(resp.Threads))
				for _, t := range resp.Threads {
					fmt.Fprintf(&out, "%s\n   %s\n\n", t.ID, t.Snippet)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "gmail_get_thread",
			Description: "Get a Gmail thread by ID",
			InputSchema: schema(map[string]any{
				"id": strProp("Thread ID"),
			}, []string{"id"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("google")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "id"); err != nil {
					return "", err
				}
				c := google.NewClient(token)
				thread, err := c.GetThread(ctx, stringArg(args, "id"))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Thread: %s\nSnippet: %s", thread.ID, thread.Snippet), nil
			},
		},
		{
			Name:        "gmail_send",
			Description: "Send an email via Gmail",
			InputSchema: schema(map[string]any{
				"to":      strProp("Recipient email address"),
				"subject": strProp("Email subject"),
				"body":    strProp("Email body text"),
			}, []string{"to", "subject", "body"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("google")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "to", "subject", "body"); err != nil {
					return "", err
				}
				c := google.NewClient(token)
				msg, err := c.SendMessage(ctx, stringArg(args, "to"), stringArg(args, "subject"), stringArg(args, "body"))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Message sent: %s", msg.ID), nil
			},
		},
		{
			Name:        "calendar_list_events",
			Description: "List upcoming calendar events",
			InputSchema: schema(map[string]any{
				"calendarId": strProp("Calendar ID (default: primary)"),
				"maxResults": map[string]any{"type": "integer", "description": "Maximum number of events to return (default 10)"},
			}, nil),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("google")
				if err != nil {
					return "", err
				}
				c := google.NewClient(token)
				maxResults := 10
				if v, ok := args["maxResults"]; ok {
					switch n := v.(type) {
					case float64:
						maxResults = int(n)
					}
				}
				resp, err := c.ListEvents(ctx, stringArg(args, "calendarId"), maxResults)
				if err != nil {
					return "", err
				}
				if len(resp.Items) == 0 {
					return "No events found.", nil
				}
				var out strings.Builder
				for _, ev := range resp.Items {
					fmt.Fprintf(&out, "%s\n   %s - %s\n   %s\n\n", ev.Summary, ev.Start.String(), ev.End.String(), ev.HTMLLink)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "calendar_create_event",
			Description: "Create a calendar event",
			InputSchema: schema(map[string]any{
				"summary":     strProp("Event title"),
				"description": strProp("Event description"),
				"startTime":   strProp("Start time in RFC3339 format (e.g. 2026-06-15T14:00:00Z)"),
				"endTime":     strProp("End time in RFC3339 format"),
			}, []string{"summary", "startTime", "endTime"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("google")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "summary", "startTime", "endTime"); err != nil {
					return "", err
				}
				c := google.NewClient(token)
				ev, err := c.CreateEvent(ctx, stringArg(args, "summary"), stringArg(args, "description"), stringArg(args, "startTime"), stringArg(args, "endTime"))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Created event: %s\n%s", ev.Summary, ev.HTMLLink), nil
			},
		},
		{
			Name:        "drive_list_files",
			Description: "List files in Google Drive",
			InputSchema: schema(map[string]any{
				"query":      strProp("Drive search query (optional, e.g. \"name contains 'report'\")"),
				"maxResults": map[string]any{"type": "integer", "description": "Maximum number of files to return (default 20)"},
			}, nil),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("google")
				if err != nil {
					return "", err
				}
				c := google.NewClient(token)
				maxResults := 20
				if v, ok := args["maxResults"]; ok {
					switch n := v.(type) {
					case float64:
						maxResults = int(n)
					}
				}
				resp, err := c.ListFiles(ctx, stringArg(args, "query"), maxResults)
				if err != nil {
					return "", err
				}
				if len(resp.Files) == 0 {
					return "No files found.", nil
				}
				var out strings.Builder
				for _, f := range resp.Files {
					fmt.Fprintf(&out, "%s (%s)\n   %s\n\n", f.Name, f.MimeType, f.WebViewLink)
				}
				return out.String(), nil
			},
		},
		{
			Name:        "drive_search_files",
			Description: "Search files in Google Drive",
			InputSchema: schema(map[string]any{
				"query": strProp("Drive search query (e.g. \"name contains 'budget' and mimeType contains 'spreadsheet'\")"),
			}, []string{"query"}),
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				token, err := tokenFn("google")
				if err != nil {
					return "", err
				}
				if err := requireArgs(args, "query"); err != nil {
					return "", err
				}
				c := google.NewClient(token)
				resp, err := c.SearchFiles(ctx, stringArg(args, "query"))
				if err != nil {
					return "", err
				}
				if len(resp.Files) == 0 {
					return "No files found.", nil
				}
				var out strings.Builder
				for _, f := range resp.Files {
					fmt.Fprintf(&out, "%s (%s)\n   %s\n\n", f.Name, f.MimeType, f.WebViewLink)
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

func NewIntegrationServer(tokenFn func(string) (string, error)) *IntegrationServer {
	tools := githubTools(tokenFn)
	tools = append(tools, gitlabTools(tokenFn)...)
	tools = append(tools, googleTools(tokenFn)...)
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
