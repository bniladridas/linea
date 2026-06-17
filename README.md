# Linea

<img src="https://s.gravatar.com/avatar/82c20b35bc57fea0f6033dc1ac686f14?size=496&default=retro" width="48" height="48" alt="Linea" />
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/linea-route.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/linea-route-dark.svg">
  <img src="./assets/linea-route-dark.svg" width="48" height="48" alt="Linea">
</picture>

Local-first AI assistant. Chats with LLMs, searches the web, manages files, automates local dev tasks. Runs on your machine.

```bash
brew install linea
linea
```

[Docs](./docs/reference.md) · [API](./docs/client-api.md) · [Build](./docs/start.md) · [macOS](./docs/macos.md)

> Everything runs locally. No data leaves your network unless you explicitly use web search or third-party models.

---

## Setup

1. Install Go 1.21+, Node.js 18+, and PostgreSQL.
2. Create the database:
   ```
   createdb linea
   createuser linea
   psql -c "ALTER USER linea WITH PASSWORD 'linea';"
   psql -c "GRANT ALL ON DATABASE linea TO linea;"
   ```
3. Copy `.env.example` to `.env` in the repo root. Add a Gemini API key:
   ```
   GEMINI_API_KEY=your-key-here
   ```
4. Build and start:
   ```
   make start
   ```
5. Open http://localhost:8080.

Skip Postgres with `make start NODB=1`.

## Quick Start

| Method | Command |
| :--- | :--- |
| Homebrew | `brew install linea && linea` |
| npm | `npm i -g @bniladridas/linea && linea` |
| Source | `git clone ... && cd linea && make start` |

## CLI

```
linea              Start the web server
linea tui          Terminal chat interface
linea daemon       Background daemon
linea check        Health checks
linea status       Daemon status
linea migrate      Database migrations
linea version      Print version
linea help         Show help
```

## Configuration

Set variables in `~/.config/linea/linea.env` or as env vars.

| Variable | Default | Description |
| :--- | :--- | :--- |
| `API_ADDR` | `127.0.0.1:8080` | Server bind address |
| `GEMINI_API_KEY` | - | Gemini API key |
| `GEMINI_MODEL` | `gemini-2.5-flash-lite` | Model ID |

Full reference in [docs/reference.md](./docs/reference.md).

## Integrations

- [GitHub MCP](./docs/mcp.md#github) -- issues, PRs, code search
- [GitLab MCP](./docs/mcp.md#gitlab) -- issues, MRs, code search
- [Google MCP](./docs/mcp.md#google) -- Gmail, Calendar, Drive
- [OAuth setup](./docs/oauth.md) -- connect GitHub, GitLab, Google accounts

## Platforms

| Platform | Method |
| :--- | :--- |
| macOS | `brew install linea` or [Releases](https://github.com/bniladridas/linea/releases) |
| Android | [Build from source](./android/README.md) |
| iOS | [Build from source](./ios/README.md) |

## Troubleshooting

```
linea daemon --debug        # verbose output
~/.cache/linea/audit.jsonl  # audit log
```

## License

MIT.
