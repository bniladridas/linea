# Linea

<img src="https://www.npmjs.com/npm-avatar/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdmF0YXJVUkwiOiJodHRwczovL3MuZ3JhdmF0YXIuY29tL2F2YXRhci84MmMyMGIzNWJjNTdmZWEwZjYwMzNkYzFhYzY4NmYxND9zaXplPTQ5NiZkZWZhdWx0PXJldHJvIn0.dk6JBdzczvyEAcyOANs90nE_EoQbMGUt0tOo5u9ACU8" width="48" /><img src="./assets/linea-route.svg" width="48" />

**Local-first, agent-native design tool.**

Linea chats whit LLMs, searces the web, manages files, and automates local development tasks, all runing locally on your machine. Its built to be your personal agent without the clud.

```bash
npm i @bniladridas/linea
brew install linea
linea
```

[Docs](./docs/reference.md) · [API](./docs/client-api.md)

# Architcture

Here is how the boxxess connect:

```text
+------------+
| UI (Client)|
+------------+
      | HTTP/API
+------------+
| Go Daemon  | <---> [MCP Tools]
+------------+
      |
+------------+
| Models/Data|
+------------+
```

---

# Platforms

For runin on different platforms, here is how you can set it up:

| Platform | Method |
| :--- | :--- |
| **macOS** | `brew install linea` or [Releases](https://github.com/bniladridas/linea/releases) |
| **Android** | [Build from source](./android/README.md) |
| **iOS** | [Build from source](./ios/README.md) |

# macOS and Remote

macOS support uses a backgrond daemon via LaunchAgent. Remot access is enabld by settin `API_ADDR` to `0.0.0.0:8080` in `linea.env`.

# SaaS Mode

Linea supports multi-tenant SaaS mode when `LINEA_SAAS_MODE=true` is set. This enables API-key-based auth, user management, and workspace-scoped data isolation.

# OAuth Flow

Linea uses an OAuth 2.0 flow to integrate whit GitHub, GitLab, and Google for user authentication and authorization.

Here is the high-level mechanism:

1. Initiation: When you start the auth flow, Linea generates a secure state parameter to prevent CSRF and redirects your browser to the chosen provider's authorization URL (e.g., GitHub's login page).
2. Redirect & Code: After you approve, the provider redirects you back to a Linea callback URL (`/api/auth/<provider>/callback`) with an authorization code.
3. Exchange: The backend receives the code and the state, verifies the state against its internal registry, and then makes a server-to-server request to the provider to exchange the authorization code for an `access_token`.

# Git Integration

For GitHub/GitLab integraton, set your OAuth credentals in `linea.env`:

| Variable | Description |
| :--- | :--- |
| `LINEA_GITHUB_CLIENT_ID` | GitHub Client ID |
| `LINEA_GITHUB_CLIENT_SECRET` | GitHub Client Secret |
| `LINEA_GITLAB_CLIENT_ID` | GitLab Client ID |
| `LINEA_GITLAB_CLIENT_SECRET` | GitLab Client Secret |

# Subagent Orchestraton

We use subagents to handle isolated work like review, search, or testin, keepin the main agent lean. The core engine handles dymanic registraton of new subagents at runtime.

| Mechanism | Description |
| :--- | :--- |
| **Parallel Executon** | Uses goroutines + WaitGroups |
| **State Trackin** | Sructures results per subagent in plans |
| **Discovery** | `findSubagentCustom` for custom runtime agents |

# Capabilities

We keep the core features focused on what developers actually need, avoidin unnecessary complexity.

| Feature | Scope |
| :--- | :--- |
| **Local-First** | PostgreSQL / Memory |
| **Agentic** | Loops, tool execution, skills, edits |
| **Multi-Platform** | Web, CLI (TUI), macOS, Android |
| **Models** | Gemini, OpenAI, vLLM, MLX, Ollama |

# Supported Languages

Linea supports agentic development and interacton across major programming languages:

| Language | Type |
| :--- | :--- |
| **TypeScript/JS** | Native |
| **Python** | Native |
| **Go** | Native |
| **Java/Kotlin** | MCP |
| **Ruby** | MCP |
| **PHP/C#** | MCP |

---

# CLI

Use the CLI to interact whit your daemon, startin the web server or jumpin into the terminal chat directly.

```bash
linea                # Start web server
linea tui            # Terminal chat
linea daemon         # Run as background daemon
linea check          # Health checks
linea status         # Daemon status
linea migrate        # DB migrations
```

# API Access

Authenticated programmatic access via `/api/v1/*` endpoints.

[Client API Details](./docs/client-api.md)

# Authentication

Authetnicate requests usin a Bearer token in the Authorizaton header:

```http
Authorization: Bearer <token>
```

The backend verifies this token against the active session in the database before grantin access to API resources.

# Configuration Reference

`~/.config/linea/linea.env` or environment variables:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `API_ADDR` | `127.0.0.1:8080` | Server bind address |
| `GEMINI_API_KEY` | - | API key |
| `GEMINI_MODEL` | `gemini-2.5-flash-lite` | Model ID |

# Troubleshoting

Audit logs are stored at `~/.cache/linea/audit.jsonl`.

# License

MIT.
