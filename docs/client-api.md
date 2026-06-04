# Client API

This is the shared contract for web, macOS, and later clients.

# Base

Linea serves the API from the same Go process as the web UI.

Default base URL:

```sh
http://localhost:8080
```

Clients should treat IDs as opaque strings.

Dates are JSON strings from the Go server.

# Status

`GET /healthz`

Returns:

```json
{"status":"ok"}
```

`GET /api/status`

Returns storage, search, and provider state.

```json
{
  "storage": "postgres",
  "search": "duckduckgo",
  "providers": [
    {"name":"Gemini","model":"gemini-2.5-flash-lite","enabled":true,"role":"primary"}
  ]
}
```

`GET /api/agent`

Returns the local agent contract. It is read-only.

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
  "boundaries": ["No destructive action without approval"],
  "next": ["Add persisted agent runs"],
  "runSummary": {
    "state": "ready",
    "traceEvents": 1,
    "hookRuns": 0,
    "skillRuns": 0,
    "commandApprovals": 0,
    "commandChecks": 0,
    "commandRuns": 0,
    "editProposals": 0,
    "updatedAt": "2026-06-01T00:00:00Z"
  },
  "hookRuns": [],
  "skillRuns": [],
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

Runs a known hook. If `command` is set, the command must pass the allowlist runner.

```json
{
  "command": "make test",
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

Runs a ready local skill. If `command` is empty, Linea uses the skill `Command:` line. Commands must pass the allowlist runner.

```json
{
  "command": "make test",
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

Runs a command only when it exactly matches `LINEA_COMMAND_ALLOWLIST`. It runs inside `LINEA_WORKSPACE_DIR`.

```json
{
  "command": "make test",
  "approvalId": "approval-id"
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

`GET /api/agent/edit-proposals`

Returns edit proposals.

`POST /api/agent/edit-proposals`

Creates a preview-only edit proposal. It does not write the file.

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
