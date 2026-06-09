# Reference

This file keeps setup details out of the main README.

# Requirements

| Need | Version |
| --- | --- |
| Go | 1.25+ |
| Node.js | 22+ |
| PostgreSQL | 16+ |
| Model access | Gemini, Cerebras, SambaNova, or Ollama |

# Install

Homebrew formula: `Formula/linea.rb`

```sh
brew tap bniladridas/linea
brew install linea
```

# Config

Linea reads `~/.config/linea/linea.env`. Shell variables override file values.

Provider order and fallback toggles are saved in `~/.config/linea/settings.json`.

```sh
API_ADDR=127.0.0.1:8080
LINEA_RULES_FILE=AGENTS.md
LINEA_SKILLS_DIR=
LINEA_WORKSPACE_DIR=
LINEA_MCP_CONFIG=
LINEA_COMMAND_ALLOWLIST=
DATABASE_URL=postgres://linea:linea@localhost:5432/linea?sslmode=disable
BRAVE_SEARCH_API_KEY=
SEARXNG_URL=
GEMINI_API_KEY=
GEMINI_MODEL=gemini-2.5-flash-lite
SAMBANOVA_API_KEY=
SAMBANOVA_BASE_URL=https://api.sambanova.ai/v1
SAMBANOVA_ENABLED=false
SAMBANOVA_MODEL=gpt-oss-120b
CEREBRAS_API_KEY=
CEREBRAS_BASE_URL=https://api.cerebras.ai/v1
CEREBRAS_ENABLED=true
CEREBRAS_MODEL=gpt-oss-120b
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=qwen2.5-coder:1.5b
OLLAMA_FALLBACK=true
STATIC_DIR=
WEB_ORIGIN=http://localhost:5173
```

| Name | Use |
| --- | --- |
| `LINEA_RULES_FILE` | Agent rules file. |
| `LINEA_AGENT_DEVELOPER_MODE` | Enables full-trust developer workspace behavior when set to `1`. Developer loops can still run bounded non-destructive commands without this flag. |
| `LINEA_AGENT_WORKSPACE_TRUST` | Set to `full` with `LINEA_AGENT_DEVELOPER_MODE=1` to let workspace APIs accept absolute paths. |
| `LINEA_PREVIEW_CACHE_DIR` | Stores generated preview snapshots on disk. Previews survive server restarts and cache clears. Empty uses the user cache directory (`~/Library/Caches/linea/previews` on macOS). |
| `LINEA_SKILLS_DIR` | Reads markdown skills from this directory. Empty means planned skills only. |
| `LINEA_WORKSPACE_DIR` | Enables read-only agent workspace tools. Empty means off. |
| `LINEA_LSP_COMMAND` | Uses an LSP command such as `gopls` for Go diagnostics, symbols, and references. Empty auto-detects `gopls` when available and otherwise uses the local parser fallback. Set `off` to force the fallback. |
| `LINEA_MCP_CONFIG` | Reads local MCP server and tool names from a JSON config. Supports per-server `allowedTools` for tool-level access control. MCP processes run with a sanitized environment. Empty means none. |
| `LINEA_COMMAND_ALLOWLIST` | Comma-separated exact commands allowed for agent checks. Commands are classified by category (read, inspect, write, destructive) and flagged when destructive. Empty means none. |
| `DATABASE_URL` | PostgreSQL. Empty means memory. |
| `BRAVE_SEARCH_API_KEY` | Optional Brave Search API key. When set, Brave results are tried before free fallbacks. |
| `SEARXNG_URL` | Optional self-hosted SearXNG base URL. When set, SearXNG results are tried before DuckDuckGo fallback. |
| `GEMINI_API_KEY` | Gemini primary. |
| `GEMINI_MODEL` | Gemini model. |
| `SAMBANOVA_API_KEY` | SambaNova fallback. |
| `SAMBANOVA_BASE_URL` | OpenAI-compatible base URL. |
| `SAMBANOVA_ENABLED` | Use SambaNova when ready. |
| `SAMBANOVA_MODEL` | SambaNova model. |
| `CEREBRAS_API_KEY` | Cerebras fallback. |
| `CEREBRAS_BASE_URL` | OpenAI-compatible base URL. |
| `CEREBRAS_ENABLED` | Use Cerebras. |
| `CEREBRAS_MODEL` | Cerebras model. |
| `OLLAMA_BASE_URL` | Ollama endpoint. |
| `OLLAMA_MODEL` | Ollama model. |
| `OLLAMA_FALLBACK` | Use local fallback. |
| `STATIC_DIR` | React assets. Empty means embedded. |
| `WEB_ORIGIN` | Frontend dev origin. |

Common command allowlist values:

```sh
LINEA_COMMAND_ALLOWLIST=make test,npm run build,go test ./...
```

Office or LAN access must be enabled explicitly. The default `API_ADDR=127.0.0.1:8080` is local-only. To expose Linea on a trusted office network, use a non-loopback bind such as:

```sh
API_ADDR=0.0.0.0:8080
WEB_ORIGIN=http://employee-machine.local:8080
```

Only use LAN mode behind a trusted network, VPN, or reverse proxy with access controls. Linea currently streams chat over HTTP/SSE, not WebSockets, so office routing needs normal HTTP streaming support rather than WebSocket upgrade support.

# Source

```sh
cp .env.example .env
cd backend
go run ./cmd/server -migrate
cd ../frontend
npm ci
npm run build
cd ../backend
go run ./cmd/server
```

Frontend only:

```sh
cd frontend
npm run dev
```

Vite runs at `http://localhost:5173`.

# Checks

| Check | Command |
| --- | --- |
| Setup | `linea -check` |
| Local setup | `make check` |
| Local tests | `make test` |
| Homebrew formula | `make install-check` |
| Release install | `make release-check` |
| Server | `linea -check-server http://127.0.0.1:8080` |
| Terminal chat | `linea -tui` (`:new`, `:rename <title>`, `:share`, `:delete confirm`, `:attach <path>`, `:help`, `:agent status`, `:diag`, `:symbols [query]`, `:refs <identifier>`, `:mcp`, `:mcp calls`, `:mcp subscriptions`, `:mcp events`, `:mcp read <resource-id-or-uri>`, `:mcp subscribe <resource-id-or-uri>`, `:mcp unsubscribe <subscription-id>`, `:mcp prompt <prompt-id> [json]`, `:mcp call <tool-id> [json]`, `:subagent [id] [query]`, `:subagent plan <goal>`, `:subagent plans`, `:search <query>`, `:read <path>`, `:loop <goal>`, `:loop auto <goal>`, `:loop developer <goal>`, `:loop continue <id>`, `:loop cancel <id>`, `:check <command>`, `:approve <command>`, `:run <command>`, `:trace <event> <state> [detail]`, `:hook-run <id> <state> [detail]`, `:hook <id> [command]`, `:skill <id> [command]`, `:proposal list`, `:proposal create <path> <content>`, `:proposal approve <id>`, `:proposal reject <id>`, `:proposal apply <id>`, `:quit`) |
| Terminal smoke | `make tui-check` |
| Terminal picker | Number or title search |
| Hand-rolled TUI | `linea -tui-beta` |
| Agent status | `linea -agent-status` |
| Agent API | `make agent-check` |
| Agent autonomy | `make agent-autonomy-check` |
| Agent API memory | `make agent-check-memory` |
| UI | `make ui-check` |
| UI with message | `make ui-check-full` |
| UI agent review | `make ui-check-agent` with `LINEA_WORKSPACE_DIR` enabled. Set `LINEA_AGENT_REVIEW_FILE` when needed. |
| Models | `make model-check` |
| macOS package | `make macos-package` |
| macOS app smoke | `make macos-check` |
| macOS app UI | `make macos-ui-check` |

Auto agent loops can gather workspace evidence, run bounded subagent plans for diagnostics/search context, apply their own generated edit proposals after explicit auto activation, run inferred project checks from project files, infer simple MCP arguments, and run multi-action MCP plans. Developer loops can also run non-destructive explicit commands without the static allowlist and infer install, lint, format, and inspection commands. Developer command checks still block destructive commands, shell-wrapped commands, broad system mutation, external-state commands such as `git push`, global package installs, credential reads, and secret-looking output. MCP subscriptions keep configured stdio servers alive, record resource notifications, and shut down when the last subscription for a server is removed. With `LINEA_AGENT_DEVELOPER_MODE=1` and `LINEA_AGENT_WORKSPACE_TRUST=full`, workspace tools accept absolute paths. Secret files and secret-looking output are filtered by default. Guided loops and explicit command runs still stop at approval boundaries.

# Client Docs

API contract: [client-api.md](./client-api.md).

macOS app: [macos.md](./macos.md).

# Release

Push a tag named `v*`.

The release workflow builds the frontend, builds the Apple Silicon binary, packages the DMG, and uploads checksums.

After a release, `formula-sha.yml` opens a pull request for `Formula/linea.rb`.

After the formula pull request is merged, `release-install-check.yml` runs `make install-check`.

Run `make release-check` for a local end-to-end release check.

UI checks need Chrome. Message checks need one working text model.

# API

| Method | Path |
| --- | --- |
| `GET` | `/healthz` |
| `GET` | `/api/status` |
| `GET` | `/api/agent` |
| `GET` | `/api/agent/run-summary` |
| `GET` | `/api/agent/runs` |
| `POST` | `/api/agent/runs` |
| `GET` | `/api/agent/subagents` |
| `GET` | `/api/agent/subagent-runs` |
| `GET` | `/api/agent/subagent-plans` |
| `POST` | `/api/agent/subagents/run` |
| `POST` | `/api/agent/subagents/{id}/run` |
| `GET` | `/api/agent/mcp-servers` |
| `GET` | `/api/agent/mcp-tools` |
| `GET` | `/api/agent/mcp-resources` |
| `POST` | `/api/agent/mcp-resources/read` |
| `GET` | `/api/agent/mcp-prompts` |
| `POST` | `/api/agent/mcp-prompts/get` |
| `GET` | `/api/agent/mcp-calls` |
| `GET` | `/api/agent/mcp-subscriptions` |
| `GET` | `/api/agent/mcp-events` |
| `POST` | `/api/agent/mcp-calls` |
| `POST` | `/api/agent/mcp-resources/subscribe` |
| `POST` | `/api/agent/mcp-subscriptions/{id}/unsubscribe` |
| `GET` | `/api/agent/traces` |
| `POST` | `/api/agent/traces` |
| `GET` | `/api/agent/hook-runs` |
| `POST` | `/api/agent/hook-runs` |
| `POST` | `/api/agent/hooks/{id}/run` |
| `GET` | `/api/agent/skill-runs` |
| `POST` | `/api/agent/skills/{id}/run` |
| `GET` | `/api/agent/loops` |
| `POST` | `/api/agent/loops` |
| `POST` | `/api/agent/loops/{id}/continue` |
| `POST` | `/api/agent/loops/{id}/cancel` |
| `POST` | `/api/agent/unrestricted` |
| `POST` | `/api/agent/background-jobs` |
| `POST` | `/api/agent/background-jobs/{id}/cancel` |
| `GET` | `/api/agent/previews/{id}/{name...}` |
| `GET` | `/api/agent/command-approvals` |
| `POST` | `/api/agent/command-approvals` |
| `GET` | `/api/agent/command-checks` |
| `POST` | `/api/agent/command-checks` |
| `GET` | `/api/agent/command-runs` |
| `POST` | `/api/agent/command-runs` |
| `GET` | `/api/agent/workspace/file` |
| `GET` | `/api/agent/workspace/search` |
| `PATCH` | `/api/agent/workspace` |
| `GET` | `/api/agent/workspace/diagnostics` |
| `GET` | `/api/agent/workspace/symbols` |
| `GET` | `/api/agent/workspace/references` |
| `GET` | `/api/agent/edit-proposals` |
| `POST` | `/api/agent/edit-proposals` |
| `PATCH` | `/api/agent/edit-proposals/{id}` |
| `POST` | `/api/agent/edit-proposals/{id}/apply` |
| `GET` | `/api/settings` |
| `PATCH` | `/api/settings` |
| `POST` | `/api/chat/temp` |
| `GET` | `/api/conversations` |
| `POST` | `/api/conversations` |
| `PATCH` | `/api/conversations/{id}` |
| `DELETE` | `/api/conversations/{id}` |
| `GET` | `/api/conversations/{id}/messages` |
| `POST` | `/api/conversations/{id}/messages` |

Messages use `multipart/form-data` with `content` and optional `files`.

Text files are sent as text. PNG, JPEG, and WebP images are sent to Gemini.

Stream events are `user`, `search`, `provider`, `chunk`, `done`, and `error`.
