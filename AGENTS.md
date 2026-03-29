# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Git Conventions

**Commits:** [Conventional Commits](https://www.conventionalcommits.org/) — enforced by commitlint in CI.

- AI-generated commits must always pass commitlint.
- AI-generated commits may only use the prefixes `feat:` or `fix:`.
- Prefer `feat:` for new behavior and `fix:` for regressions, bugs, and repair work.

**Branches:** `{type}/{issue-number}-short-description` when working on a GitHub issue, e.g. `feat/42-session-auto-cleanup`. Without an issue: `{type}/short-description`, e.g. `fix/crash-on-empty-config`.

## Review Loop

For repository changes that go through GitHub PRs, use the Zevro Review loop by default:

1. Open a PR with a clear English title and description.
2. Trigger review with `/review` when appropriate.
3. Treat Zevro review findings as the primary feedback loop and address them in follow-up commits.
4. After each review-driven change, rerun the relevant tests locally and summarize verification in English on the PR.
5. Repeat until the PR is approved or otherwise ready to merge.

Before every push for an open PR, rerun the relevant local test/build commands and wait for them to finish successfully. Do not rely on CI to do the first validation pass.

All public GitHub communication in this repository should be in English, including PR descriptions, issue comments, review replies, and code comments.

## Project Overview

Remote Control On Demand (RCOD) — a Go application that manages `Codex rc` sessions remotely via a Telegram bot. Users can start, stop, monitor, and restart Codex sessions across multiple git repositories from their phone.

## Build, Run & Test

```bash
# Build
go build -o rcod ./cmd/bot

# Run (auto-detects config.yaml in current directory)
./rcod

# Run with custom config
./rcod --config /path/to/config.yaml

# Development run
go run ./cmd/bot

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...
```

**Testing Requirement:** Always add unit tests when implementing new features or fixing bugs. All PRs must pass existing tests in CI.

## Architecture

The app follows a layered architecture with four core packages under `internal/`:

- **`config/`** — YAML config loading/validation + interactive TUI onboarding wizard (runs on first launch when config.yaml is missing)
- **`bot/`** — Telegram bot command handlers, inline keyboard navigation, authentication via `allowed_user_id`, notification listener goroutine
- **`session/`** — Session lifecycle management (start/stop/crash/auto-restart), ring buffer for output capture (500 lines), URL scanner that detects `Codex.ai` URLs in process output
- **`process/`** — Spawns `Codex rc` subprocesses with process group isolation (`Setpgid=true`), always using `--permission-mode bypassPermissions`, and handles graceful shutdown

**Entry point:** `cmd/bot/main.go` — parses flags, loads config (or runs wizard), wires up all components, handles OS signals for graceful shutdown.

**Key data flow:** Process output → ring buffer + URL scanner → notification channel (buffered, cap 100) → bot goroutine → Telegram message.

## Key Patterns

- **Process group isolation:** Each session spawns `Codex rc` with `Setpgid=true`. Kill sends SIGTERM to `-pid` (whole group) with 5s WaitDelay before force-kill.
- **Auto-restart:** Session manager tracks restart count per session. On crash, if `auto_restart` enabled and under `max_restarts`, sleeps `restart_delay_seconds` then respawns.
- **URL detection:** `urlscanner.go` wraps `io.Writer`, fires callback exactly once (via `atomic.Bool`) when a `Codex.ai` URL appears in output.
- **Dual command mode:** Telegram commands work with direct arguments (`/start my-project`) or interactively via inline keyboard picker.

## Adding a New Telegram Command

1. Add handler method on `Bot` struct in `internal/bot/bot.go`
2. Register in `registerHandlers()` method
3. Add to `registerCommands()` for Telegram command menu
4. If interactive (picker), add callback case in `handleCallback()`

## Configuration

YAML format in `config.yaml` (gitignored). See `config.example.yaml` for template. Key fields:
- `telegram.token` / `telegram.allowed_user_id` — required
- `rc.base_folder` — required, directory containing git repos
- `rc.auto_restart`, `rc.max_restarts`, `rc.restart_delay_seconds` — restart behavior
- `rc.notifications.progress_update_interval`, `rc.notifications.idle_timeout`, `rc.notifications.patterns` — Telegram notifications and heartbeats

## Dependencies

Go 1.21+. Key libraries: `gopkg.in/telebot.v4` (Telegram API), `gopkg.in/yaml.v3` (config), `golang.org/x/sys` + `golang.org/x/term` (process/terminal control).

## Planning

When producing an implementation plan:

- Keep it concrete and executable against the current repository state.
- Include verification steps alongside the implementation steps.
- If you write a plan into `plans/`, treat that file as the source of truth. Do not assume there is a generated or "hardened" variant elsewhere in the repo.
- Before implementing from a saved plan, re-read the latest version of that exact file and call out any stale assumptions or obvious risks first.
