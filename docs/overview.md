# Overview

## Architecture

Linea uses a daemon-client architecture with a Go backend serving a web frontend over HTTP.

## Platforms

Linea runs on macOS, Android, and iOS.

## Features

| Feature     | Scope                                        |
| :---------- | :------------------------------------------- |
| Local-first | PostgreSQL or in-memory storage              |
| Models      | Gemini, OpenAI-compatible, vLLM, MLX, Ollama |
| Interfaces  | Web, CLI (TUI), macOS, Android               |
| Tools       | MCP integrations and local tool execution    |
| API         | Authenticated HTTP API                       |

## CLI

```text
linea                Start the web server
linea tui            Terminal chat interface
linea daemon         Run as background daemon
linea check          Run health checks
linea status         Show daemon status
linea migrate        Apply database migrations
linea version        Print version
linea help           Show help
```

## Configuration

Configuration can be provided through environment variables or `~/.config/linea/linea.env`.

See the documentation for the complete list of options.

## License

MIT.
