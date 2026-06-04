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
API_ADDR=:8080
LINEA_RULES_FILE=AGENTS.md
LINEA_SKILLS_DIR=
LINEA_WORKSPACE_DIR=
LINEA_COMMAND_ALLOWLIST=
DATABASE_URL=postgres://linea:linea@localhost:5432/linea?sslmode=disable
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
| `LINEA_SKILLS_DIR` | Reads markdown skills from this directory. Empty means planned skills only. |
| `LINEA_WORKSPACE_DIR` | Enables read-only agent workspace tools. Empty means off. |
| `LINEA_COMMAND_ALLOWLIST` | Comma-separated exact commands allowed for agent checks. Empty means none. |
| `DATABASE_URL` | PostgreSQL. Empty means memory. |
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
| Homebrew formula | `make install-check` |
| Release install | `make release-check` |
| Server | `linea -check-server http://localhost:8080` |
| UI | `make ui-check` |
| UI with message | `make ui-check-full` |
| Models | `make model-check` |
| macOS package | `make macos-package` |

# Client Docs

API contract: [client-api.md](./client-api.md).

macOS app: [macos.md](./macos.md).

# Release

Push a tag named `v*`.

The release workflow builds the frontend, builds the Apple Silicon binary, packages the DMG, and uploads checksums.

After a release, `formula-sha.yml` opens a pull request for `Formula/linea.rb`.

Run `make release-check`.

UI checks need Chrome. Message checks need one working text model.

# API

| Method | Path |
| --- | --- |
| `GET` | `/healthz` |
| `GET` | `/api/status` |
| `GET` | `/api/agent` |
| `GET` | `/api/agent/traces` |
| `POST` | `/api/agent/traces` |
| `GET` | `/api/agent/hook-runs` |
| `POST` | `/api/agent/hook-runs` |
| `POST` | `/api/agent/hooks/{id}/run` |
| `GET` | `/api/agent/command-checks` |
| `POST` | `/api/agent/command-checks` |
| `GET` | `/api/agent/command-runs` |
| `POST` | `/api/agent/command-runs` |
| `GET` | `/api/agent/workspace/file` |
| `GET` | `/api/agent/workspace/search` |
| `GET` | `/api/agent/edit-proposals` |
| `POST` | `/api/agent/edit-proposals` |
| `GET` | `/api/conversations` |
| `POST` | `/api/conversations` |
| `PATCH` | `/api/conversations/{id}` |
| `DELETE` | `/api/conversations/{id}` |
| `GET` | `/api/conversations/{id}/messages` |
| `POST` | `/api/conversations/{id}/messages` |

Messages use `multipart/form-data` with `content` and optional `files`.

Text files are sent as text. PNG, JPEG, and WebP images are sent to Gemini.

Stream events are `user`, `search`, `provider`, `chunk`, `done`, and `error`.
