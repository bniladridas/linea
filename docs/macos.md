# macOS

This is the macOS package shape.

# Goal

Ship a local macOS app for Linea.

The app should:

* start Linea locally
* open a chat interface
* use the same API contract as the web UI
* keep PostgreSQL and provider config outside the app bundle
* package as a `.dmg`

# Desktop Shell

Use a thin desktop shell.

The shell wraps the existing web UI.

The Go backend stays the source of truth.

No native rewrite yet.

# App Shape

Linea uses a shared daemon architecture. The Go binary runs as a persistent background daemon (LaunchAgent on macOS). All clients connect to it over HTTP.

Process:

* macOS app checks if the daemon is healthy at `http://localhost:8080/healthz`
* If not running, app spawns `linea daemon` from the bundled binary
* App waits for `/healthz`
* App opens the local UI in a WebView
* App does **not** stop the daemon on quit - it is shared across platforms

The daemon binary is bundled inside the app bundle at `Contents/Resources/linea`.

Daemon management:

* `linea install` - install as a LaunchAgent (auto-starts on login)
* `linea uninstall` - remove LaunchAgent and stop
* `linea status` - check if running
* `linea daemon` - start in foreground as background daemon

Config:

* read `~/.config/linea/linea.env` (honored by the daemon)
* `API_ADDR` environment variable is read by the app to find the daemon
* never store provider keys in app code
* keep existing environment variable names

Data:

* use the same `DATABASE_URL`
* keep memory storage available when no database is set

# Package

Target:

```text
dist/macos/Linea.app
dist/macos/Linea.dmg
```

First package can be unsigned for local testing.

Signing and notarization come after the app starts reliably.

Build:

```sh
make macos-package
```

The app bundle includes the Linea daemon binary at `Contents/Resources/linea`.

The launcher is a small Swift app. It starts the daemon if not running, waits for `/healthz`, opens the local UI in a WebView window, and leaves the daemon running on quit.

The window remembers its size. The menus include reload, window controls, and normal quit behavior.

Open the DMG and drag `Linea.app` to `Applications`.

# Checks

Before a DMG:

```sh
make test
make release-check
```

Manual check:

* app opens
* daemon is running (check with `linea status`)
* chat works
* web search works
* file attachment works
* image input works when Gemini quota allows it
* quitting does not stop the daemon (reopen the app and chat should still work)

# Later

Do not build Android during this step.

Do not add accounts.

Do not add sync.

Do not rewrite the UI natively.

Do not move secrets into app storage.
