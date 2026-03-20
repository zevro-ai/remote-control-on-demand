# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Git Conventions

**Commits:** [Conventional Commits](https://www.conventionalcommits.org/) — enforced by commitlint in CI.

- AI-generated commits must always pass commitlint.
- AI-generated commits may only use the prefixes `feat:` or `fix:`.
- Prefer `feat:` for new behavior and `fix:` for regressions, bugs, and repair work.

**Branches:** `{type}/{issue-number}-short-description` when working on a GitHub issue, e.g. `feat/42-session-auto-cleanup`. Without an issue: `{type}/short-description`, e.g. `fix/crash-on-empty-config`.

## Project Overview

Remote Control On Demand (RCOD) — a Go application for remotely managing `claude rc` sessions and chatting with Codex or Claude inside multiple git repositories. The primary binary exposes Telegram controls and an optional HTTP dashboard/API.

## Build, Run & Test

```bash
# Build
cd app && npm ci && npm run build && cd ..
go build -o rcod ./cmd/rcod

# Run (auto-detects config.yaml in current directory)
./rcod

# Run with custom config
./rcod --config /path/to/config.yaml

# Development run
cd app && npm ci && npm run build && cd ..
go run ./cmd/rcod

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...
```

**Testing Requirement:** Always add unit tests when implementing new features or fixing bugs. All PRs must pass existing tests in CI.

## Architecture

The app is split across a few core areas under `internal/`:

- **`config/`** — YAML config loading/validation + interactive TUI onboarding wizard
- **`session/`** — Claude RC lifecycle management, ring buffer output capture, URL detection, persistence
- **`process/`** — Spawns `claude rc` subprocesses with process group isolation and graceful shutdown
- **`bot/`** — Legacy Telegram bot for Claude RC control only
- **`codexbot/`** — Primary Telegram bot for Claude RC control plus Codex chat
- **`codex/`** — Persistent Codex chat session manager
- **`claudechat/`** — Persistent Claude chat session manager
- **`httpapi/`** — Optional HTTP API, WebSocket hub, and bundled dashboard SPA

**Entry point:** `cmd/rcod/main.go` — parses flags, loads config (or runs wizard), restores Claude RC plus chat session state, optionally starts the HTTP server, and handles OS signal shutdown.

**Key data flows:** `claude rc` output → ring buffer + URL scanner → notification channel → Telegram; Codex/Claude chat requests → manager → Telegram/API response; dashboard/API updates → WebSocket hub.

## Key Patterns

- **Process group isolation:** Each Claude RC session spawns `claude rc` with `Setpgid=true`. Kill sends SIGTERM to `-pid` (whole group) with 5s `WaitDelay` before force-kill.
- **Auto-restart:** Session manager tracks restart count per session. On crash, if `auto_restart` enabled and under `max_restarts`, sleeps `restart_delay_seconds` then respawns.
- **URL detection:** `urlscanner.go` wraps `io.Writer`, fires a callback exactly once (via `atomic.Bool`) when a `claude.ai` URL appears in output.
- **Persistent chat state:** `cmd/rcod` restores Codex and Claude chat sessions from disk on startup.
- **Dual command mode:** Telegram commands work with direct arguments (`/start my-project`, `/new my-project`) or interactively via inline keyboard pickers.

## Adding a New Telegram Command

1. Decide whether the command belongs in the legacy Claude-only bot (`internal/bot/bot.go`) or the primary combined bot (`internal/codexbot/bot.go`).
2. Add the handler method on the relevant `Bot` struct.
3. Register it in `registerHandlers()`.
4. Add it to `registerCommands()` if it should appear in the Telegram menu.
5. If it uses inline interactions, add the callback case in `handleCallback()`.

## Configuration

YAML format in `config.yaml` (gitignored). See `config.example.yaml` for template. Key fields:
- `telegram.token` / `telegram.allowed_user_id` — required when Telegram control is enabled
- `api.port` / `api.token` — optional HTTP dashboard/API settings
- `rc.base_folder` — required, directory containing git repos
- `rc.permission_mode` — controls Codex and Claude chat sandbox/permission mode
- `rc.auto_restart`, `rc.max_restarts`, `rc.restart_delay_seconds` — restart behavior
- `rc.notifications.progress_update_interval`, `rc.notifications.idle_timeout`, `rc.notifications.patterns` — Telegram notifications and heartbeats

At least one of Telegram or API must be configured.

## Dependencies

Go 1.25.6+. Key libraries: `gopkg.in/telebot.v4` (Telegram API), `gopkg.in/yaml.v3` (config), `github.com/coder/websocket` (dashboard/WebSocket transport), `golang.org/x/sys` + `golang.org/x/term` (process/terminal control).

## Planning

When producing an implementation plan:

- Keep it concrete and executable against the current repository state.
- Include verification steps alongside the implementation steps.
- If you write a plan into `plans/`, treat that file as the source of truth. Do not assume there is a generated or "hardened" variant elsewhere in the repo.
- Before implementing from a saved plan, re-read the latest version of that exact file and call out any stale assumptions or obvious risks first.
