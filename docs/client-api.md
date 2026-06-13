# Client API

This is the shared contract for all Linea clients (web, macOS, iOS, Android, TUI).

# Base

Linea serves the API from a single persistent Go daemon process. All clients connect to it over HTTP.

Default base URL:

```sh
http://127.0.0.1:8080
```

Android emulator connects to `http://10.0.2.2:8080` (host loopback).

Clients should treat IDs as opaque strings.

Dates are JSON strings from the Go server.

# Status

`GET /healthz`

Returns:

```json
{"status":"ok"}
```

`GET /api/version`

Returns the build version.

```json
{"version": "0.1.8"}
```

`GET /api/status`

Returns storage, search, and provider state.

```json
{
  "storage": "postgres",
  "search": "DuckDuckGo + Wikipedia",
  "providers": [
    {"name":"Gemini","model":"gemini-2.5-flash-lite","enabled":true,"role":"primary"}
  ]
}
```

Search uses free no-key fallbacks by default: DuckDuckGo Instant Answer, DuckDuckGo HTML results, and Wikipedia. Research-shaped queries may also use OpenAlex and arXiv. When `SEARXNG_URL` is set, Linea tries that self-hosted SearXNG instance before DuckDuckGo. When `BRAVE_SEARCH_API_KEY` is set, Brave is tried first.

`GET /api/agent`

Returns the local agent contract. It is read-only and does not start MCP servers.

```json
{
  "mode": "local",
  "rules": {
    "source": "AGENTS.md",
    "loaded": true,
    "summary": ["Local-first"]
  },
  "tools": [
    {"id":"read_file","name":"Read files","access":"workspace","approval":"not required"}
  ],
  "hooks": [
    {"id":"before_tool","event":"Before tool calls","state":"planned"}
  ],
  "skills": [
    {"id":"review_change","name":"Review change","state":"ready","command":"make test"}
  ],
  "subagents": [
    {"id":"review","name":"Review","purpose":"Inspect changes for bugs, regressions, and missing checks.","state":"ready","tools":["read_file","search_files","diagnostics"]}
  ],
  "mcpServers": [
    {"id":"docs","name":"docs","state":"ready","command":"node","args":["server.js"],"envKeys":["TOKEN"]}
  ],
  "mcpTools": [
    {"id":"docs/search_docs","serverId":"docs","serverName":"docs","name":"search_docs","description":"Search docs","state":"ready"}
  ],
  "boundaries": ["No destructive action without approval"],
  "next": [],
  "runSummary": {
    "state": "ready",
    "traceEvents": 1,
    "hookRuns": 0,
    "skillRuns": 0,
    "agentLoops": 0,
    "commandApprovals": 0,
    "commandChecks": 0,
    "commandRuns": 0,
    "editProposals": 0,
    "updatedAt": "2026-06-01T00:00:00Z"
  },
  "hookRuns": [],
  "skillRuns": [],
  "agentLoops": [],
  "commandApprovals": [],
  "commandChecks": [],
  "commandRuns": [],
  "traceEvents": [
    {"id":"runtime-ready","event":"agent runtime","state":"ready","createdAt":"2026-06-01T00:00:00Z"}
  ]
}
```

`GET /api/agent/traces`

Returns recent agent trace events.

```json
[
  {
    "id": "id",
    "event": "before tool",
    "state": "recorded",
    "detail": "read-only",
    "createdAt": "2026-06-01T00:00:00Z"
  }
]
```

`GET /api/agent/run-summary`

Returns counts and state for recent agent runtime activity.

```json
{
  "state": "ready",
  "traceEvents": 1,
  "hookRuns": 0,
  "skillRuns": 0,
  "commandApprovals": 0,
  "commandChecks": 0,
  "commandRuns": 0,
  "editProposals": 0,
  "updatedAt": "2026-06-01T00:00:00Z"
}
```

`GET /api/agent/runs`

Returns saved agent run snapshots.

```json
[
  {
    "id": "id",
    "state": "ready",
    "summary": {
      "state": "ready",
      "traceEvents": 1,
      "hookRuns": 0,
      "skillRuns": 0,
      "agentLoops": 0,
      "commandApprovals": 0,
      "commandChecks": 0,
      "commandRuns": 0,
      "editProposals": 0,
      "updatedAt": "2026-06-01T00:00:00Z"
    },
    "createdAt": "2026-06-01T00:00:00Z"
  }
]
```

`POST /api/agent/runs`

Saves the current agent run summary.

```json
{
  "id": "id",
  "state": "ready",
  "summary": {
    "state": "ready",
    "traceEvents": 1,
    "hookRuns": 0,
    "skillRuns": 0,
    "agentLoops": 0,
    "commandApprovals": 0,
    "commandChecks": 0,
    "commandRuns": 0,
    "editProposals": 0,
    "updatedAt": "2026-06-01T00:00:00Z"
  },
  "createdAt": "2026-06-01T00:00:00Z"
}
```

`GET /api/agent/loops`

Returns recent bounded agent loops.

`POST /api/agent/loops`

Starts a bounded local agent loop. Read-only workspace steps may run immediately. `mode` can be `guided`, `auto`, or `developer`; guided loops pause at edit, MCP tool, and command boundaries. Auto loops may gather workspace evidence, run bounded subagent plans for diagnostics/search context, apply their own generated edit proposals when `autoApply` is true, rerun failed commands after auto-applied fixes until the loop iteration cap is reached, run inferred project checks from files such as `package.json`, `Makefile`, or `go.mod`, and call selected MCP tools. Developer loops keep the same bounded loop shape but may run non-destructive explicit commands without the static command allowlist, infer install/lint/format/inspection commands, show redacted command output in the timeline, and run one safe follow-up inspection command after a failed developer command. Destructive, shell-wrapped, broad system, external-state, and credential-read commands remain blocked. Set `LINEA_AGENT_DEVELOPER_MODE=1` and `LINEA_AGENT_WORKSPACE_TRUST=full` to let workspace APIs accept absolute paths. Secret files and secret-looking output are filtered by default. `tempWorkspace` creates or reuses a temporary app package outside the current workspace and returns `previewUrl` when a preview is available.

```json
{
  "goal": "check diagnostics and run tests",
  "mode": "auto",
  "maxIterations": 5,
  "autoApply": true,
  "tempWorkspace": false,
  "sessionId": "conversation-id",
  "query": "diagnostic",
  "filePath": "README.md",
  "command": "make test"
}
```

`POST /api/agent/loops/{id}/continue`

Continues a waiting loop after the needed input or approval exists.

```json
{
  "autoApply": true,
  "query": "diagnostic",
  "filePath": "README.md",
  "command": "make test"
}
```

`POST /api/agent/loops/{id}/cancel`

Cancels a waiting loop.

`POST /api/agent/unrestricted`

Enables or disables unrestricted terminal autonomy for the developer loop. Body: `{"unrestricted": true}`.

`POST /api/agent/background-jobs`

Starts a background autonomous job. The job loop auto-continues until completion. Body: `{"goal": "...", "mode": "auto"}`.

`POST /api/agent/background-jobs/{id}/cancel`

Cancels a running background job.

`GET /api/agent/previews/{id}/{name...}`

Serves a temporary agent preview file. The root path serves the preview entry file.

Preview URLs are returned as `previewUrl` from completed temporary app loops. Linea stores preview snapshots in `LINEA_PREVIEW_CACHE_DIR` or the user cache directory so recent previews can recover across restarts. They may still return a small unavailable page after the cached snapshot is removed.

`GET /api/agent/subagents`

Returns bounded subagent roles.

```json
[
  {
    "id": "review",
    "name": "Review",
    "purpose": "Inspect changes for bugs, regressions, and missing checks.",
    "state": "planned",
    "tools": ["read_file", "search_files", "diagnostics"]
  }
]
```

`POST /api/agent/subagents/{id}/run`

Runs a bounded subagent inspection. It may read workspace diagnostics or search files, but it does not execute commands or edit files.

```json
{
  "goal": "review current diagnostics",
  "query": "diagnostic"
}
```

`POST /api/agent/subagents/run`

Runs a bounded subagent plan. Linea selects up to three relevant subagents from the goal and query, or uses `subagentIds` when provided. Child runs are recorded in the normal subagent run history.

```json
{
  "goal": "review docs and search workspace",
  "query": "agent",
  "subagentIds": ["review", "docs", "search"]
}
```

`GET /api/agent/subagent-plans`

Returns recent bounded subagent plan runs.

`GET /api/agent/subagent-runs`

Returns recent subagent runs.

`GET /api/agent/mcp-servers`

Returns local MCP servers from `LINEA_MCP_CONFIG`. It does not start servers. Env values are not returned.

```json
[
  {
    "id": "docs",
    "name": "docs",
    "state": "ready",
    "command": "node",
    "args": ["server.js"],
    "envKeys": ["TOKEN"]
  }
]
```

`GET /api/agent/mcp-tools`

Returns MCP tool metadata declared in `LINEA_MCP_CONFIG`. If a configured server has no `tools` list, Linea starts it briefly and calls `tools/list`.

```json
[
  {
    "id": "docs/search_docs",
    "serverId": "docs",
    "serverName": "docs",
    "name": "search_docs",
    "description": "Search docs",
    "inputSchema": "{\"type\":\"object\"}",
    "state": "ready"
  }
]
```

`GET /api/agent/mcp-resources`

Returns MCP resource metadata declared in `LINEA_MCP_CONFIG`. If a configured server has no `resources` list, Linea starts it briefly and calls `resources/list`.

`POST /api/agent/mcp-resources/read`

Reads a configured or discovered stdio MCP resource.

```json
{
  "uri": "docs://readme"
}
```

`GET /api/agent/mcp-prompts`

Returns MCP prompt metadata declared in `LINEA_MCP_CONFIG`. If a configured server has no `prompts` list, Linea starts it briefly and calls `prompts/list`.

`POST /api/agent/mcp-prompts/get`

Gets a configured or discovered stdio MCP prompt.

```json
{
  "name": "review",
  "arguments": {"topic": "routing"}
}
```

`GET /api/agent/mcp-calls`

Returns recent MCP tool, resource, and prompt calls.

`GET /api/agent/mcp-subscriptions`

Returns active and recently closed persistent MCP resource subscriptions.

`GET /api/agent/mcp-events`

Returns recent MCP subscription notifications.

`POST /api/agent/mcp-calls`

Calls a configured stdio MCP tool. Tools may be declared in config or discovered from `tools/list`. Agent auto and developer loops can call selected MCP tools, infer simple required arguments from the user goal, and run multi-action MCP plans when the goal asks for all or multi-step MCP work. If arguments cannot be inferred, the loop stops at the MCP boundary for an explicit UI or TUI call.

`POST /api/agent/mcp-resources/subscribe`

Starts a persistent stdio MCP session for the resource server and sends `resources/subscribe`.

```json
{
  "resourceId": "docs/README"
}
```

`POST /api/agent/mcp-subscriptions/{id}/unsubscribe`

Sends `resources/unsubscribe`, marks the subscription inactive, and stops the persistent MCP session when no active subscriptions remain for that server.

```json
{
  "toolId": "docs/search_docs",
  "arguments": {"query": "Linea"}
}
```

`POST /api/agent/traces`

Records an agent trace event.

```json
{
  "event": "before tool",
  "state": "recorded",
  "detail": "read-only"
}
```

`GET /api/agent/hook-runs`

Returns recent hook runs.

```json
[
  {
    "id": "id",
    "hookId": "before_tool",
    "state": "completed",
    "detail": "read file",
    "createdAt": "2026-06-01T00:00:00Z"
  }
]
```

`POST /api/agent/hook-runs`

Records a known hook run. It does not execute commands.

```json
{
  "hookId": "before_tool",
  "state": "completed",
  "detail": "read file"
}
```

`POST /api/agent/hooks/{id}/run`

Runs a known hook. If `command` is set, the command needs an approved `approvalId`.

```json
{
  "command": "make test",
  "approvalId": "approval-id",
  "detail": "before commit"
}
```

Returns:

```json
{
  "hookRun": {
    "id": "id",
    "hookId": "before_commit",
    "state": "completed",
    "detail": "before commit",
    "createdAt": "2026-06-01T00:00:00Z"
  },
  "commandRun": {
    "id": "id",
    "command": "make test",
    "exitCode": 0,
    "output": "ok",
    "truncated": false,
    "createdAt": "2026-06-01T00:00:00Z"
  }
}
```

`GET /api/agent/skill-runs`

Returns recent skill runs.

```json
[
  {
    "id": "id",
    "skillId": "review_change",
    "state": "completed",
    "detail": "make test",
    "createdAt": "2026-06-01T00:00:00Z"
  }
]
```

`POST /api/agent/skills/{id}/run`

Runs a ready local skill. If `command` is empty, Linea uses the skill `Command:` line. Commands need an approved `approvalId`.

```json
{
  "command": "make test",
  "approvalId": "approval-id",
  "detail": "review change"
}
```

Returns:

```json
{
  "skillRun": {
    "id": "id",
    "skillId": "review_change",
    "state": "completed",
    "detail": "review change",
    "createdAt": "2026-06-01T00:00:00Z"
  },
  "commandRun": {
    "id": "id",
    "command": "make test",
    "exitCode": 0,
    "output": "ok",
    "truncated": false,
    "createdAt": "2026-06-01T00:00:00Z"
  }
}
```

`GET /api/agent/command-checks`

Returns recent command checks.

```json
[
  {
    "id": "id",
    "command": "make test",
    "approvalId": "approval-id",
    "allowed": true,
    "reason": "allowed",
    "createdAt": "2026-06-01T00:00:00Z"
  }
]
```

`GET /api/agent/command-approvals`

Returns recent command approvals.

```json
[
  {
    "id": "id",
    "command": "make test",
    "state": "approved",
    "detail": "before commit",
    "createdAt": "2026-06-01T00:00:00Z"
  }
]
```

`POST /api/agent/command-approvals`

Records a command approval. Approved commands must be in `LINEA_COMMAND_ALLOWLIST`.

```json
{
  "command": "make test",
  "state": "approved",
  "detail": "before commit"
}
```

`POST /api/agent/command-checks`

Checks a command against `LINEA_COMMAND_ALLOWLIST`. It does not execute the command.

```json
{
  "command": "make test",
  "approvalId": "approval-id"
}
```

`GET /api/agent/command-runs`

Returns recent command runs.

```json
[
  {
    "id": "id",
    "command": "make test",
    "exitCode": 0,
    "output": "ok",
    "truncated": false,
    "createdAt": "2026-06-01T00:00:00Z"
  }
]
```

`POST /api/agent/command-runs`

Runs a command only when it exactly matches `LINEA_COMMAND_ALLOWLIST` and has an approved `approvalId`. It runs inside `LINEA_WORKSPACE_DIR`.

```json
{
  "command": "make test",
  "approvalId": "approval-id"
}
```

`GET /api/agent/auto-approve-categories`

Returns the list of auto-approved command categories.

```json
["read", "write"]
```

`PUT /api/agent/auto-approve-categories`

Sets the list of auto-approved command categories.

```json
{
  "categories": ["read", "write"]
}
```

`GET /api/agent/workspace/file?path=README.md`

Reads a text file from `LINEA_WORKSPACE_DIR`.

Workspace tools are off when `LINEA_WORKSPACE_DIR` is empty.

```json
{
  "path": "README.md",
  "content": "# Linea",
  "size": 8,
  "truncated": false
}
```

`GET /api/agent/workspace/search?q=Linea`

Searches text files in `LINEA_WORKSPACE_DIR`.

```json
[
  {
    "path": "README.md",
    "line": 1,
    "text": "# Linea"
  }
]
```

`PATCH /api/agent/workspace`

Updates the workspace root for the running server. Existing edit proposals are cleared.

```json
{
  "root": "/Users/name/project"
}
```

`GET /api/agent/workspace/diagnostics`

Returns local workspace diagnostics. Linea tries `LINEA_LSP_COMMAND` first when set, auto-detects `gopls` when available, and otherwise uses the local Go parser fallback. Set `LINEA_LSP_COMMAND=off` to force the fallback.

```json
[
  {
    "path": "backend/main.go",
    "line": 12,
    "column": 8,
    "severity": "error",
    "message": "expected operand"
  }
]
```

`GET /api/agent/workspace/symbols?q=Run`

Returns Go symbols from `LINEA_WORKSPACE_DIR`. Linea tries `LINEA_LSP_COMMAND` first when set, auto-detects `gopls` when available, and otherwise uses the local parser fallback. Set `LINEA_LSP_COMMAND=off` to force the fallback.

```json
[
  {
    "name": "Run",
    "kind": "func",
    "path": "backend/main.go",
    "line": 12
  }
]
```

`GET /api/agent/workspace/references?q=Run`

Returns Go identifier references from `LINEA_WORKSPACE_DIR`. Linea tries `LINEA_LSP_COMMAND` first when set, auto-detects `gopls` when available, and otherwise uses the local scanner fallback. Set `LINEA_LSP_COMMAND=off` to force the fallback.

```json
[
  {
    "name": "Run",
    "path": "backend/main.go",
    "line": 18,
    "text": "Run()"
  }
]
```

`GET /api/agent/edit-proposals`

Returns edit proposals.

Approval and rejection only update proposal state. They do not apply changes to disk.

`POST /api/agent/edit-proposals`

Creates an edit proposal. It does not write the file until applied.

```json
{
  "path": "README.md",
  "content": "# Linea\n",
  "summary": "short note"
}
```

Returns:

```json
{
  "id": "id",
  "path": "README.md",
  "summary": "short note",
  "status": "pending",
  "diff": [
    {"type":"remove","oldLine":1,"text":"# Old"},
    {"type":"add","newLine":1,"text":"# Linea"}
  ],
  "createdAt": "2026-06-01T00:00:00Z"
}
```

`PATCH /api/agent/edit-proposals/{id}`

Reviews an edit proposal. It does not write the file.

```json
{
  "status": "approved",
  "detail": "reviewed"
}
```

Valid statuses are `approved` and `rejected`.

`POST /api/agent/edit-proposals/{id}/apply`

Writes an approved proposal to disk and marks it applied. Pending and rejected proposals cannot be applied.

# Settings

`GET /api/settings`

Returns provider order and enabled state.

```json
{
  "providers": [
    {"id":"gemini","name":"Gemini","model":"gemini-2.5-flash-lite","role":"primary","enabled":true,"configured":true}
  ]
}
```

`PATCH /api/settings`

Updates provider order and enabled state. At least one configured provider must stay enabled.

```json
{
  "providers": [
    {"id":"gemini","enabled":true},
    {"id":"ollama","enabled":false}
  ]
}
```

# Users

`GET /api/users`

Returns all users.

```json
[
  {
    "id": "id",
    "email": "user@example.com",
    "name": "User",
    "createdAt": "2026-06-01T00:00:00Z",
    "updatedAt": "2026-06-01T00:00:00Z"
  }
]
```

`POST /api/users`

Body:

```json
{"email":"user@example.com","name":"User"}
```

Email is required. Name is optional (defaults to empty). Returns `409` if email already exists.

# Conversations

`GET /api/conversations`

Returns newest conversations first.

```json
[
  {
    "id": "id",
    "title": "Title",
    "createdAt": "2026-06-01T00:00:00Z",
    "updatedAt": "2026-06-01T00:00:00Z"
  }
]
```

`POST /api/conversations`

Body:

```json
{"title":"Untitled"}
```

Title is optional. Empty title becomes `Untitled`.

`PATCH /api/conversations/{id}`

Body:

```json
{"title":"New title"}
```

Title is required.

`DELETE /api/conversations/{id}`

Returns `204` when deleted.

# Messages

`GET /api/conversations/{id}/messages`

Returns messages in stored order.

```json
[
  {
    "id": "id",
    "conversationId": "conversation-id",
    "role": "user",
    "content": "Hello",
    "createdAt": "2026-06-01T00:00:00Z"
  }
]
```

`POST /api/conversations/{id}/messages`

Request type:

```text
multipart/form-data
```

Fields:

| Name | Use |
| --- | --- |
| `content` | Required message text |
| `files` | Optional repeated file field |

Limits:

| File | Limit |
| --- | --- |
| Text and other files | 512 KB |
| PNG, JPEG, WebP | 2 MB |

Image input uses Gemini.

Messages whose first line is `propose edit <path>`, `propose change <path>`, or `create proposal <path>` create an edit proposal instead of calling a model.

Terminal clients can use the same agent surface with explicit commands such as `:help`, `:new`, `:rename <title>`, `:share`, `:delete confirm`, `:attach <path>`, `:agent status`, `:diag`, `:symbols [query]`, `:refs <identifier>`, `:mcp`, `:mcp calls`, `:mcp subscriptions`, `:mcp events`, `:mcp read <resource-id-or-uri>`, `:mcp subscribe <resource-id-or-uri>`, `:mcp unsubscribe <subscription-id>`, `:mcp prompt <prompt-id> [json]`, `:mcp call <tool-id> [json]`, `:subagent [id] [query]`, `:subagent plan <goal>`, `:subagent plans`, `:search <query>`, `:read <path>`, `:loop <goal>`, `:loop auto <goal>`, `:loop developer <goal>`, `:loop continue <id>`, `:loop cancel <id>`, `:check <command>`, `:checks`, `:approve <command>`, `:approvals`, `:run <command>`, `:runs`, `:auto-approve [categories...]`, `:trace <event> <state> [detail]`, `:hook-run <id> <state> [detail]`, `:hook <id> [command]`, `:skill <id> [command]`, `:proposal list`, `:proposal create <path> <content>`, `:proposal approve <id>`, `:proposal reject <id>`, `:proposal apply <id>`, and `:quit`. The TUI picker accepts a number or title search, and long transcripts scroll with the terminal viewport keys.

The remaining message body is the proposed full file content. Fenced content is accepted.

````text
propose edit README.md
```md
# Linea
```
````

`POST /api/chat/temp`

Streams a temporary chat response without saving a conversation. The request is `multipart/form-data` with the same `content` and optional repeated `files` fields as saved messages.

Clients may send a `history` field containing a JSON array of prior temporary messages:

```json
[
  {"role":"user","content":"Hello"},
  {"role":"assistant","content":"Hi"}
]
```

Temporary chat supports the same stream events as saved message creation.

# Stream

Message creation returns server-sent events.

Events:

| Event | Data |
| --- | --- |
| `user` | Saved user message |
| `search` | Search result |
| `provider` | Active model provider |
| `chunk` | Assistant text chunk |
| `done` | Saved assistant message |
| `error` | Error message |

`chunk` may include provider data:

```json
{
  "content": "Hello",
  "provider": {"name":"ollama","model":"qwen2.5-coder:1.5b"}
}
```

`done` may include provider data:

```json
{
  "id": "id",
  "conversationId": "conversation-id",
  "role": "assistant",
  "content": "Hello",
  "createdAt": "2026-06-01T00:00:00Z",
  "provider": {"name":"ollama","model":"qwen2.5-coder:1.5b"}
}
```

# Errors

Errors use JSON for normal requests:

```json
{"error":"Message content is required."}
```

Streaming errors use an `error` event:

```json
{"error":"Could not generate a response."}
```

Clients should keep the saved user message when a stream error happens.

# Client Rules

Create a conversation before sending a message.

Keep drafts on the client until a message is sent.

Use the `done` event as the saved assistant message.

Use `provider` and `chunk.provider` for live model display.

Do not depend on PostgreSQL from a client.

Do not call model providers from a client.
