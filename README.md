# RCOD: Remote Control On Demand

[![Build](https://github.com/zevro-ai/remote-control-on-demand/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/zevro-ai/remote-control-on-demand/actions/workflows/build.yml)
[![Release](https://github.com/zevro-ai/remote-control-on-demand/actions/workflows/release.yml/badge.svg)](https://github.com/zevro-ai/remote-control-on-demand/actions/workflows/release.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)

Manage [Claude Code](https://docs.anthropic.com/en/docs/claude-code) sessions remotely and chat with Codex or Claude inside your repositories. RCOD runs on your machine or server, starts `claude rc` inside selected git repositories, exposes an optional web dashboard/API, and relays status plus chat responses back to Telegram.

Built by [zevro.ai](https://zevro.ai).

![RCOD demo](./docs/assets/rcod-demo.gif)

## What RCOD Does

- Starts and stops `claude rc` sessions from Telegram
- Recursively discovers repositories under a configured projects folder
- Streams Claude session URLs and selected output events back to Telegram
- Lets you chat with Codex or Claude inside a selected repository
- Sends periodic progress heartbeats on long-running sessions
- Restarts crashed sessions automatically when configured
- Applies per-project overrides from `.rcod.yaml`
- Adds inline Telegram actions for status, logs, restart, stop, and Claude links
- Paginates repository pickers and supports unique partial project matches
- Always launches Claude with `--permission-mode bypassPermissions`
- Serves an optional authenticated HTTP API and embedded dashboard

## Codex Telegram Chat

The `cmd/rcod` entrypoint exposes Telegram as a chat interface for Codex:

- create a Codex chat session for a repo with `/new`
- switch between chat sessions with `/sessions` and `/use`
- send a normal Telegram message to talk to Codex in the active repo
- close old chat threads with `/close`
- inspect the active chat with `/current`
- Codex respects `rc.permission_mode` from `config.yaml`; default is `workspace-write`, `bypassPermissions` opts into full local access, and other accepted values map to Codex sandbox modes

Build and run it with:

```bash
cd app && npm ci && npm run build
go build -o rcod ./cmd/rcod
./rcod -config config.yaml
```

For local native control helpers:

```bash
scripts/start-codexbot-native.sh config.yaml
scripts/status-codexbot-native.sh
scripts/reset-codexbot-native.sh config.yaml
scripts/install-codexbot-native-launchd.sh config.yaml
```

## How It Works

```text
Telegram or web dashboard -> RCOD -> claude rc / Codex / Claude
                        <- status, logs, links, chat replies <-
```

## Prerequisites

| Requirement | Notes |
| --- | --- |
| Go 1.25+ | Needed only when building from source |
| Claude Code CLI | Install with `npm install -g @anthropic-ai/claude-code` |
| Codex CLI | Needed for `cmd/rcod`; install it and keep `codex` in your `PATH` |
| Telegram bot token | Create one via [@BotFather](https://t.me/BotFather) |
| Telegram user ID | Get it from [@userinfobot](https://t.me/userinfobot) |

`claude` must be available in your `PATH`.
For Codex chat, `codex` must also be available in your `PATH`.

## Quick Start

```bash
git clone https://github.com/zevro-ai/remote-control-on-demand.git
cd remote-control-on-demand
cd app && npm ci && npm run build && cd ..
go build -o rcod ./cmd/rcod
./rcod
```

If `config.yaml` is missing, RCOD starts an interactive onboarding flow and writes a local config file with mode `0600`.

## Why It Feels Good

- no SSH session needed just to check whether Claude is still working
- Telegram becomes the remote control surface, not a dumb notifier
- long sessions keep sending heartbeats, so you know work is still moving
- inline actions make restart, logs, and stop a two-tap flow

## Configuration

Minimal `config.yaml`:

```yaml
telegram:
  token: "YOUR_BOT_TOKEN"
  allowed_user_id: 123456789

rc:
  base_folder: "/home/user/Projects"
  permission_mode: "workspace-write"
  auto_restart: true
  max_restarts: 3
  restart_delay_seconds: 5
```

See [config.example.yaml](./config.example.yaml) for a fuller example with notifications.

At least one control surface must be configured: Telegram, the HTTP API, or both.

### Global Config Fields

| Field | Description |
| --- | --- |
| `telegram.token` | Bot token from @BotFather |
| `telegram.allowed_user_id` | Only this Telegram user can control the bot |
| `api.port` | Optional HTTP API/dashboard port; `0` disables it |
| `api.token` | Optional bearer token for API auth |
| `rc.base_folder` | Directory RCOD scans for git repositories |
| `rc.permission_mode` | Codex bot access mode: `workspace-write` (default), `read-only`, `danger-full-access`, or `bypassPermissions` |
| `rc.auto_restart` | Enables automatic restart for crashed sessions |
| `rc.max_restarts` | Maximum restart attempts before giving up |
| `rc.restart_delay_seconds` | Delay between restart attempts |
| `rc.notifications.progress_update_interval` | Optional progress heartbeat interval, for example `10m` |
| `rc.notifications.idle_timeout` | Optional idle notification threshold |
| `rc.notifications.patterns` | Optional regex-based output notifications |

RCOD intentionally always starts Claude with `claude rc --permission-mode bypassPermissions`. This is not configurable.

### Per-Project Overrides

Create `.rcod.yaml` inside a repository under `rc.base_folder` to override defaults for that project:

```yaml
auto_restart:
  enabled: true
  max_attempts: 5
  delay: 10s
prompt: "Focus on triaging open issues first"
max_duration: 2h
notifications:
  progress_update_interval: 10m
  idle_timeout: 10m
  patterns:
    - name: "task_completed"
      regex: "(?i)(task completed|all done)"
      once: true
```

| Field | Description |
| --- | --- |
| `auto_restart.enabled` | Override global auto-restart for this project |
| `auto_restart.max_attempts` | Override restart limit |
| `auto_restart.delay` | Override restart delay, for example `30s` or `1m` |
| `prompt` | Extra project-specific context shown when the session starts |
| `max_duration` | Kill the session after a fixed duration |
| `notifications.*` | Override global notification settings for this project |

### Folder Resolution

RCOD only starts repositories that resolve inside `rc.base_folder`. Direct command arguments such as `/start team/api` are allowed, but path traversal outside the configured base folder is rejected.

## HTTP API And Dashboard

Set `api.port` in `config.yaml` to enable the embedded HTTP server. When enabled, RCOD serves:

- a Claude RC session API under `/api/sessions`
- Claude chat APIs under `/api/claude/...`
- Codex chat APIs under `/api/codex/...`
- a WebSocket endpoint at `/ws`
- the bundled dashboard SPA at `/`

Set `api.token` to require bearer auth for API and dashboard requests.

## Telegram Commands

### Claude Remote Control

| Command | Description |
| --- | --- |
| `/start` | Start a session from the discovered repository list |
| `/kill` | Stop a running session |
| `/status` | Show folder, PID, uptime, and restart count |
| `/logs` | Show the last 50 log lines |
| `/restart` | Restart a session |
| `/list` | List active sessions |
| `/folders` | Browse available repositories |
| `/help` | Show command help |

Commands also accept direct arguments, for example `/start my-project` or `/kill a1b2`. `/start` resolves exact matches first, then unique partial matches.

### Codex Chat

| Command | Description |
| --- | --- |
| `/new` | Create a Codex session for a repository |
| `/sessions` | List Codex sessions |
| `/use` | Switch the active Codex session |
| `/close` | Close a Codex session |
| `/current` | Show the active Codex session |

After creating a Codex session, a normal text message is sent to Codex in the active repository.

## Running as a Service

Service templates live in [packaging/systemd/rcod.service](./packaging/systemd/rcod.service) and [packaging/launchd/ai.zevro.rcod.plist](./packaging/launchd/ai.zevro.rcod.plist). Replace the placeholder paths with your own values before installing them.

## Session Persistence

RCOD saves runtime state next to `config.yaml`:

- `sessions.json` for Claude RC sessions
- `codex_sessions.json` for Codex chat sessions
- `claude_sessions.json` for Claude chat sessions

- **Bot Restarts:** If the bot is restarted (e.g., during an update or system reboot), it will automatically re-attach to any `claude rc` processes that are still running and restore saved Codex/Claude chat metadata.
- **URL Recovery:** Upon re-attaching, the bot re-sends the original `claude.ai` session link to Telegram so you can resume work immediately.
- **Orphan Monitoring:** The bot continues to monitor these "orphaned" processes and will notify you if they eventually exit or crash.

## Development

```bash
make build
make test
make vet
make fmt
```

If you do not use `make`, the equivalent commands are:

```bash
cd app && npm ci && npm test && npm run build && cd ..
go build -o rcod ./cmd/rcod
go test ./...
go vet ./...
gofmt -w cmd internal
```

## Security Notes

- `config.yaml` contains your Telegram bot token and should stay local
- Only `telegram.allowed_user_id` can control the bot
- Child processes inherit your user account permissions
- RCOD strips Claude session environment variables before spawning nested `claude rc` processes

See [SECURITY.md](./SECURITY.md) for reporting guidance.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## Assets

- [Demo GIF](./docs/assets/rcod-demo.gif)
- [GIF renderer](./scripts/render_demo_gif.swift)

## License

[Apache 2.0](./LICENSE)
