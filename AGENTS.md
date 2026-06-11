# AGENTS.md

# Linea

Linea is a local-first AI assistant.

## Purpose

* Chat with users using LLMs
* Search the web when needed
* Read attached files
* Maintain conversation history
* Provide a clean, minimal interface

## Current Stage

* Post-MVP

The MVP is complete. Current work covers agentic delivery, platform expansion, and autonomy.

## Target Platform

Current target:

* Apple Silicon Macs (M-series)
* Homebrew distribution
* Local web application
* Terminal (TUI)
* Native macOS application (.dmg)

Active targets:

* Agentic delivery
* macOS app polish
* Android application

New targets should reuse the existing backend and core application logic.

## Product Shape

Linea is a local AI assistant.

Interface:

* Local web application
* Browser-based chat interface
* Served by the Go daemon
* Terminal interface
* Native macOS wrapper
* iOS app
* Android app

CLI (unified `linea` binary):

* `linea` — start the web server
* `linea daemon` — start as background daemon
* `linea install` — install LaunchAgent
* `linea uninstall` — uninstall LaunchAgent
* `linea status` — show daemon status
* `linea tui` — terminal chat interface
* `linea migrate` — apply database migrations
* `linea check` — run health checks
* `linea version` — print version
* `linea help` — show help

Installation:

* Homebrew

Model providers:

* Gemini
* Cerebras
* SambaNova
* Ollama fallback

Conversation storage:

* PostgreSQL

Interfaces should reuse the same backend and core application logic whenever possible.

## Authentication

MVP:

* No user accounts
* No login required
* Local-first experience

Post-MVP:

* Optional user accounts
* Optional cloud synchronization
* Optional multi-device support

Authentication and sync are active targets.

## Stack

Backend:

* Go
* PostgreSQL

Frontend:

* React

AI:

* Gemini
* OpenAI-compatible chat endpoints
* Ollama

## Priorities

1. Reliability
2. Simplicity
3. Maintainability
4. Small scope
5. Fast startup
6. Low resource usage

## MVP Features

* Chat
* Conversation history
* Model fallback
* Web search
* File attachments
* Image input through Gemini
* Streaming responses

## Post-MVP Features

Build agentic delivery in small, reviewable steps.

Initial scope:

* Rules
* Local tools
* Hooks
* Skills
* Edit proposals
* Bounded agent loops
* Workspace search, file read, diagnostics, and symbols
* Optional LSP-backed diagnostics, symbols, and references with local fallback
* TUI agent commands
* Local MCP status, configured tool calls, resources, prompts, and subscriptions
* Bounded subagent runs
* Model and provider status
* Better fallback handling
* macOS app polish
* Explicit opt-in developer loops for broader non-destructive local work

Active scope:

* Android app
* Optional sync
* Deeper subagent orchestration
* Background autonomous jobs
* Unrestricted terminal autonomy

## Non-goals

* Complex memory systems
* Enterprise features
* Plugin marketplaces
* Workflow builders
* Team collaboration features
* Broad system access outside explicit per-session opt-in
* Unbounded tool execution outside explicit developer mode

Subagents are allowed when the task benefits from isolation.

## Agentic Delivery

The agentic layer should stay local-first, permissioned, and inspectable.

Start with one agent loop:

1. Understand the request.
2. Make a short plan.
3. Use allowed tools.
4. Ask only when blocked.
5. Run checks.
6. Show what changed.

Loop modes:

* Guided loops pause at approval, input, edit, and retry boundaries.
* Auto loops may continue across safe local steps after explicit activation.
* Auto loops may use diagnostics, command output, and file reads as evidence.
* Auto loops may create and apply bounded edit proposals.
* Auto loops may infer and run allowlisted check commands from project files.
* Auto loops may rerun failed checks after auto-applied fixes until the loop iteration cap is reached.
* Developer loops are opt-in and visibly separate from normal loops.
* Developer loops may run non-destructive local commands without the static allowlist.
* Developer loops may infer install, build, lint, format, test, and inspection commands from project files.
* Developer loops may use command output, diagnostics, file reads, and generated edit proposals in a fix/retry cycle.
* Full-trust workspace access is opt-in through `LINEA_AGENT_DEVELOPER_MODE=1` and `LINEA_AGENT_WORKSPACE_TRUST=full`.
* Generated app previews should use temporary package sessions unless the user asks to edit the current project.
* Temporary app sessions should run their checks inside the temp package before showing a preview.
* Normal auto loops must stop before destructive actions, credential reads, secret exposure, privilege escalation, billing/payment actions, or commands that intentionally modify broad system state.
* Developer loops may run commands inferred from project files within a bounded inspect/edit/build/test/fix cycle.
* Developer loops may escalate to full terminal autonomy when explicitly confirmed per-session.

Rules:

* Store project rules in plain files.
* Keep rules short.
* Prefer explicit allowlists.
* Never bypass destructive-action approval.
* Never expose secrets.

Tools:

* Read files
* Search files
* Edit files
* Run approved commands
* Run non-destructive developer commands in developer mode
* Read diagnostics

Command approval:

* Commands are classified by category (read/write/inspect/destructive/unknown).
* Auto-approve categories (read, write, inspect, destructive) can be toggled via UI or `LINEA_AUTO_APPROVE_CATEGORIES` env var.
* Commands matching an auto-approved category skip per-command approval but still respect the allowlist.
* `CheckCommand` reason strings distinguish `"auto-approved by category"` from `"auto-approved (unrestricted mode)"`.
* Audit log (JSON lines at `LINEA_AUDIT_LOG_PATH`, default `~/.cache/linea/audit.jsonl`) persists approvals and runs across restarts. Rotates at 10 MB. Set to blank to disable.
* `LoadAuditLog()` is called explicitly by the server after `NewRuntime`; tests never auto-load, preventing state leaking between tests.

Edit proposals:

* Chat may create explicit edit proposals.
* Show proposed diffs before review.
* Approval and rejection update review state only.
* Guided proposal application must be explicit separate work.
* Auto loops may apply their own generated proposals after explicit auto activation.

Hooks:

* Before tool calls
* After file edits
* Before commits
* After checks

Skills:

* Keep skills focused on one workflow.
* Prefer skills for repeated work.
* Keep skill inputs and outputs clear.

MCP:

* Use configured stdio MCP tools, resources, prompts, and subscriptions only when they give clear value.
* Prefer local MCP servers first.
* Keep permissions narrow.
* Infer simple MCP arguments only when the goal and schema make them clear.
* Stop at the MCP boundary when required arguments cannot be inferred safely.
* Persistent MCP servers must not have a hard timeout; the old 30s timeout was removed because it killed long-lived servers.
* `cmd.Cancel` is not set before `cmd.Start()` to avoid nil pointer risk.

LSP:

* Use configured LSP, or auto-detected `gopls`, for diagnostics, symbols, references, and code navigation when it preserves the existing user-facing contract.
* Keep local parser/search fallbacks available.
* Do not make core behavior depend on one editor.

Subagents:

* Use subagents for review, search, testing, docs, or isolated work.
* Merge results through the main agent.

## Design Principles

* Prefer simple solutions.
* Prefer fewer dependencies.
* Prefer explicit code over clever code.
* Keep architecture understandable by one developer.
* Build the smallest thing that solves the problem.
* Optimize for maintainability over novelty.
* Favor boring technology when it solves the problem well.

## Architecture Principles

Design the system so that the core application logic is independent from the user interface.

Preferred layering:

* Domain
* Services
* Infrastructure
* Interface

Possible interfaces:

* Web UI
* TUI
* macOS application
* Android application

Business logic should not depend on any specific interface.

When possible:

* Keep domain logic reusable.
* Avoid UI-specific logic in core packages.
* Avoid coupling Gemini integration directly to UI code.
* Design for future interface expansion without building it now.

## Project Structure

* Keep packages focused and small.
* Avoid circular dependencies.
* Favor composition over inheritance.
* Keep business logic independent from UI concerns.
* Prefer clear package boundaries.

## Performance

* Keep startup time low.
* Avoid unnecessary background processes.
* Minimize memory usage where reasonable.
* Prefer streaming responses when supported.

## Dependencies

* Add dependencies only when they provide clear value.
* Prefer Go standard library when reasonable.
* Remove unused dependencies.
* Avoid introducing frameworks without strong justification.

## Testing

* Add tests for new behavior.
* Fix broken tests when encountered.
* Do not remove tests to make builds pass.
* Prefer deterministic tests.
* Test core business logic first.

## Documentation

* Update documentation when behavior changes.
* Keep setup instructions current.
* Keep examples working.
* Document important architectural decisions.

## Security

* Never commit secrets.
* Use environment variables for credentials.
* Follow least-privilege principles.
* Avoid logging sensitive information.

## Decision Making

When multiple solutions exist:

1. Choose the simplest.
2. Choose the option with fewer dependencies.
3. Choose the option easiest to maintain.
4. Choose the option that keeps MVP scope small.
5. Choose the option that preserves future extensibility.

## Working Agreement

Work autonomously and continue until the task is complete.

Only stop for:

* destructive actions
* billing/payment actions
* secrets/credentials
* unclear product decisions

Otherwise:

* make reasonable assumptions
* fix related issues you discover
* keep code clean
* update documentation
* run relevant tests
* complete obvious follow-up work
* leave the repository in a better state than you found it

Developer-mode work may continue through inspect, edit, build, test, diagnose, and retry cycles. Full terminal autonomy is available per-session in developer mode.

## Success Criteria

Linea should continue to allow a user to:

* Install the application using Homebrew
* Launch the application locally
* Open the chat interface
* Send messages
* Receive responses from a configured model
* Search the web when needed
* Upload files
* Continue previous conversations

without requiring unnecessary configuration, accounts, or complexity.

Post-MVP success means Linea can also:

* Explain which model answered
* Show when a fallback is used
* Recover clearly when a local model is not running
* Run local tasks with explicit tool boundaries
* Run opt-in developer loops for local coding work
* Run full terminal autonomy in per-session developer mode
* Keep user-visible behavior simple
