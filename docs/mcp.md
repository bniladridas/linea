# MCP Tools

Linea has built-in MCP tools for GitHub, GitLab, and Google. No config files needed. They register programmatically.

## GitHub

Eight tools. Requires OAuth setup (see [OAuth docs](./oauth.md)).

| Tool | Description |
| :--- | :--- |
| `github_list_issues` | List issues for a repo |
| `github_get_issue` | Get details of a specific issue |
| `github_create_issue` | Create a new issue |
| `github_search_issues` | Search issues and PRs |
| `github_list_pull_requests` | List pull requests for a repo |
| `github_get_pull_request` | Get details of a specific PR |
| `github_create_pull_request` | Create a pull request |
| `github_search_code` | Search code across repos |

Also `github_get_user` for authenticated user details.

## GitLab

Eight tools. Requires OAuth setup.

| Tool | Description |
| :--- | :--- |
| `gitlab_list_issues` | List issues for a project |
| `gitlab_get_issue` | Get details of a specific issue |
| `gitlab_create_issue` | Create a new issue |
| `gitlab_search_issues` | Search issues |
| `gitlab_list_merge_requests` | List merge requests for a project |
| `gitlab_get_merge_request` | Get details of a specific MR |
| `gitlab_create_merge_request` | Create a merge request |
| `gitlab_search_code` | Search code across projects |

Also `gitlab_get_user`.

## Google

Seven tools. Requires OAuth setup.

| Tool | Description |
| :--- | :--- |
| `gmail_list_threads` | List Gmail threads |
| `gmail_get_thread` | Get a thread by ID |
| `gmail_send` | Send email |
| `calendar_list_events` | List upcoming events |
| `calendar_create_event` | Create an event |
| `drive_list_files` | List Drive files |
| `drive_search_files` | Search Drive files |

## Enabling

Set the required environment variables (see [OAuth docs](./oauth.md)), then connect via Settings > Connections in the UI. Tokens are encrypted at rest with AES-256-GCM.
