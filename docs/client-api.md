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
