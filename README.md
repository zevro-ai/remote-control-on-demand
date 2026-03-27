# RCOD: Remote Control On Demand

[![Build](https://github.com/zevro-ai/remote-control-on-demand/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/zevro-ai/remote-control-on-demand/actions/workflows/build.yml)
[![Release](https://github.com/zevro-ai/remote-control-on-demand/actions/workflows/release.yml/badge.svg)](https://github.com/zevro-ai/remote-control-on-demand/actions/workflows/release.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)

Manage [Claude Code](https://docs.anthropic.com/en/docs/claude-code) sessions remotely via Telegram. RCOD runs on your machine or server, starts `claude rc` inside selected git repositories, and sends status, logs, crashes, and Claude session links back to your phone.

This repo also includes the `rcodbot` Telegram entrypoint that lets you chat with Codex directly inside a selected repository.

Built by [zevro.ai](https://zevro.ai).

![RCOD demo](./docs/assets/rcod-demo.gif)

## What RCOD Does

- Starts and stops `claude rc` sessions from Telegram
- Recursively discovers repositories under a configured projects folder
- Streams Claude session URLs and selected output events back to Telegram
- Sends periodic progress heartbeats on long-running sessions
- Restarts crashed sessions automatically when configured
- Applies per-project overrides from `.rcod.yaml`
- Adds inline Telegram actions for status, logs, restart, stop, and Claude links
- Paginates repository pickers and supports unique partial project matches
- Always launches Claude with `--permission-mode bypassPermissions`

## RCOD Bot Chat

The `cmd/rcodbot` entrypoint exposes Telegram as a chat interface for Codex:

- create a Codex chat session for a repo with `/new` or `/folders`
- switch between chat sessions with `/sessions` and `/use`
- send a normal Telegram message to talk to Codex in the active repo
- close old chat threads with `/close`
- Codex respects `providers.codex.chat.permission_mode` from `config.yaml`; default is `workspace-write`, `bypassPermissions` opts into full local access, and other accepted values map to Codex sandbox modes

Build and run it with:

```bash
cd app && npm ci && npm run build
go build -o rcod ./cmd/rcodbot
./rcod -config config.yaml
```

For repo-local `rcodbot` helpers:

```bash
scripts/start-rcodbot-native.sh config.yaml
scripts/status-rcodbot-native.sh
scripts/reset-rcodbot-native.sh config.yaml
scripts/install-rcodbot-native-launchd.sh config.yaml
```

## How It Works

```text
You (Telegram) -> RCOD bot -> claude rc (inside your git repo)
       <- status, logs, Claude link <-
```

## Prerequisites

| Requirement | Notes |
| --- | --- |
| Go 1.25+ | Needed only when building from source |
| Claude Code CLI | Install with `npm install -g @anthropic-ai/claude-code` |
| Codex CLI | Needed for `cmd/rcodbot`; install it and keep `codex` in your `PATH` |
| Telegram bot token | Create one via [@BotFather](https://t.me/BotFather) |
| Telegram user ID | Get it from [@userinfobot](https://t.me/userinfobot) |

`claude` must be available in your `PATH`.
For `rcodbot`, `codex` must also be available in your `PATH`.

## Quick Start

```bash
git clone https://github.com/zevro-ai/remote-control-on-demand.git
cd remote-control-on-demand
cd app && npm ci && npm run build && cd ..
go build -o rcod ./cmd/rcodbot
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

providers:
  claude:
    runtime:
      auto_restart: true
      max_restarts: 3
      restart_delay_seconds: 5
    chat:
      permission_mode: "workspace-write"
  codex:
    chat:
      permission_mode: "workspace-write"
```

See [config.example.yaml](./config.example.yaml) for a fuller example with notifications.

### Dashboard Auth

The dashboard/API can run in three auth modes:

- no API auth at all when `api.port` is enabled without `api.token` or `api.auth`
- bearer token auth with `api.token`
- external browser login with `api.auth`, using either generic OIDC (for Authentik and similar providers) or GitHub OAuth

External auth uses an RCOD-managed session cookie after login. Browser API calls and WebSocket connections then reuse that cookie automatically. Token auth remains available for scripts and can also stay enabled alongside external auth if you want both machine access and interactive login.

External auth requires:

- `api.auth.session_secret` with at least 32 random characters
- exactly one provider under `api.auth.oidc` or `api.auth.github`
- a callback URL pointing back to RCOD, usually `https://your-host/api/auth/callback`

OIDC is the recommended path for Authentik and other self-hosted identity providers. GitHub login is supported separately for teams that want a lightweight OAuth flow without a dedicated IdP.

Example OIDC/Authentik-style config:

```yaml
api:
  port: 8080
  token: ""
  auth:
    session_secret: "replace-with-at-least-32-random-characters"
    oidc:
      issuer_url: "https://auth.example.com/application/o/rcod/"
      client_id: "rcod"
      client_secret: "replace-me"
      redirect_url: "https://rcod.example.com/api/auth/callback"
      scopes: ["openid", "profile", "email"]
      allowed_emails:
        - "you@example.com"
      allowed_groups:
        - "rcod-admins"
```

Example GitHub config:

```yaml
api:
  port: 8080
  auth:
    session_secret: "replace-with-at-least-32-random-characters"
    github:
      client_id: "Iv1.xxxxx"
      client_secret: "replace-me"
      redirect_url: "https://rcod.example.com/api/auth/callback"
      allowed_users:
        - "octocat"
      allowed_orgs:
        - "zevro-ai"
```

### Global Config Fields

| Field | Description |
| --- | --- |
| `telegram.token` | Bot token from @BotFather |
| `telegram.allowed_user_id` | Only this Telegram user can control the bot |
| `api.port` | Enables the HTTP dashboard/API when greater than `0` |
| `api.token` | Optional bearer token for API clients and legacy dashboard auth |
| `api.auth.session_secret` | Required for external dashboard sessions; must be at least 32 characters |
| `api.auth.oidc.*` | Generic OIDC provider config for Authentik and similar identity providers |
| `api.auth.github.*` | GitHub OAuth config for external dashboard login |
| `rc.base_folder` | Directory RCOD scans for git repositories |
| `providers.claude.runtime.auto_restart` | Enables automatic restart for crashed `claude rc` runtime sessions |
| `providers.claude.runtime.max_restarts` | Maximum restart attempts before giving up |
| `providers.claude.runtime.restart_delay_seconds` | Delay between restart attempts |
| `providers.claude.runtime.notifications.*` | Optional runtime notification settings and progress heartbeats |
| `providers.claude.chat.permission_mode` | Claude chat permission mode for `rcodbot` |
| `providers.codex.chat.permission_mode` | Codex chat access mode: `workspace-write` (default), `read-only`, `danger-full-access`, or `bypassPermissions` |

RCOD intentionally always starts Claude with `claude rc --permission-mode bypassPermissions`. This is not configurable.

Legacy `rc.permission_mode`, `rc.auto_restart`, `rc.max_restarts`, `rc.restart_delay_seconds`, and `rc.notifications` are still accepted as fallbacks for older configs.

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

## Telegram Commands

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

## Running as a Service

RCOD supports both:

- Debian/Ubuntu `systemd` deployments, including a system-wide unit that still runs as a non-root user and a per-user `systemctl --user` mode
- Debian/Ubuntu `.deb` packages built by GoReleaser/nFPM for standard install and upgrade flows
- macOS `launchd` deployments with a recommended boot-time `LaunchDaemon` that still runs as a normal macOS user, plus an optional per-user `LaunchAgent`

Start with:

- [docs/deployment/deb.md](./docs/deployment/deb.md)
- [docs/deployment/launchd.md](./docs/deployment/launchd.md)
- [docs/deployment/systemd.md](./docs/deployment/systemd.md)
- [packaging/systemd/rcod.service](./packaging/systemd/rcod.service)
- [packaging/systemd/rcod.user.service](./packaging/systemd/rcod.user.service)
- [scripts/install-rcod-systemd.sh](./scripts/install-rcod-systemd.sh)
- [scripts/install-rcod-launchd.sh](./scripts/install-rcod-launchd.sh)
- [packaging/launchd/ai.zevro.rcod.plist](./packaging/launchd/ai.zevro.rcod.plist)
- [packaging/launchd/ai.zevro.rcod.agent.plist](./packaging/launchd/ai.zevro.rcod.agent.plist)

## Session Persistence

RCOD automatically saves the state of all active sessions to a `sessions.json` file (located in the same directory as your `config.yaml`). 

- **Bot Restarts:** If the bot is restarted (e.g., during an update or system reboot), it will automatically re-attach to any `claude rc` processes that are still running.
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
go build -o rcod ./cmd/rcodbot
go test ./...
go vet ./...
gofmt -w cmd internal
```

## Security Notes

- `config.yaml` contains your Telegram bot token and should stay local
- `api.auth.session_secret`, OIDC client secrets, and GitHub client secrets should be treated like any other credential
- Only `telegram.allowed_user_id` can control the bot
- Dashboard/API auth can be enforced either with `api.token` or an external login provider under `api.auth`
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
