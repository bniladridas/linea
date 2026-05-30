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

* MVP

## Target Platform

Current target:

* Apple Silicon Macs (M-series)
* Homebrew distribution

Future targets:

* Terminal (TUI)
* Native macOS application (.dmg)
* Android application

Future targets must not influence MVP decisions unless explicitly requested.

## Product Shape

The MVP is a local AI assistant.

Interface:

* Local web application
* Browser-based chat interface
* Served by the Go backend

Installation:

* Homebrew

Model providers:

* Gemini
* Cerebras
* SambaNova
* Ollama fallback

Conversation storage:

* PostgreSQL

Future interfaces should reuse the same backend and core application logic whenever possible.

## Authentication

MVP:

* No user accounts
* No login required
* Local-first experience

Future:

* Optional user accounts
* Optional cloud synchronization
* Optional multi-device support

Authentication and sync are out of scope for the MVP.

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

## Non-goals

For the MVP:

* Multi-agent systems
* Autonomous workflows
* Complex memory systems
* Enterprise features
* Plugin marketplaces
* Workflow builders
* Team collaboration features
* Cloud synchronization
* User accounts
* Multiple interface implementations

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

## Success Criteria

For the MVP, Linea should allow a user to:

* Install the application using Homebrew
* Launch the application locally
* Open the chat interface
* Send messages
* Receive responses from a configured model
* Search the web when needed
* Upload files
* Continue previous conversations

without requiring unnecessary configuration, accounts, or complexity.
