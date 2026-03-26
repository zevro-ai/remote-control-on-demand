# macOS `launchd` deployment

This guide covers the supported ways to run RCOD under `launchd` on macOS.

- `daemon` mode: recommended for always-on Macs such as a Mac mini; installs a system-wide `LaunchDaemon`, but the RCOD process itself runs as a normal macOS user
- `agent` mode: installs a per-user `LaunchAgent`; simpler, but it only runs while that user has an active login session

In both modes, RCOD itself should run without root privileges.

## Recommended layouts

### System-wide `LaunchDaemon` (recommended)

- binary: `/usr/local/bin/rcod` on Intel, or `/opt/homebrew/bin/rcod` on Apple Silicon
- config: `/usr/local/etc/rcod/config.yaml` on Intel, or `/opt/homebrew/etc/rcod/config.yaml` on Apple Silicon
- state: `/Users/<user>/Library/Application Support/rcod/`
- logs: `/Users/<user>/Library/Logs/rcod/`
- plist: `/Library/LaunchDaemons/ai.zevro.rcod.plist`
- runtime user: an existing non-root macOS account that already has access to your repositories and credentials

### Per-user `LaunchAgent`

- binary: `~/.local/bin/rcod` when built from source, or `/usr/local/bin/rcod` / `/opt/homebrew/bin/rcod` with `--skip-build`
- config: `~/.config/rcod/config.yaml`
- state: `~/Library/Application Support/rcod/`
- logs: `~/Library/Logs/rcod/`
- plist: `~/Library/LaunchAgents/ai.zevro.rcod.agent.plist`
- runtime user: your current account

## Why `daemon` mode is recommended

For a headless or semi-headless Mac mini, `daemon` mode is the better default because:

- it starts at boot, even before anyone logs in
- it can restart automatically after crashes
- it still runs RCOD as a normal user via `launchd` `UserName`, not as root

Use `agent` mode only when you explicitly want the service lifecycle tied to your login session.

## Prerequisites

- macOS host with `launchd`
- Go installed if you want the helper to build from a source checkout
- `claude` available in the runtime user's `PATH`
- `codex` available too if you use Codex chat flows
- a valid `config.yaml`
- the runtime user must have read/write access to `rc.base_folder`

The installer helper supports both source builds and prebuilt binaries:

- default mode: build `rcod` from the current repository checkout
- `--skip-build`: reuse an already installed `rcod` binary

For the examples below, pick the prefix that matches your machine:

```bash
RCOD_PREFIX=/usr/local      # Intel
RCOD_PREFIX=/opt/homebrew   # Apple Silicon
```

## Option 1: system-wide `LaunchDaemon` running as a normal user

Use this for an always-on Mac mini or any machine where RCOD should start at boot without an interactive Terminal session.

### 1. Prepare the config

Pick a stable config path and create it:

```bash
sudo install -d -m 0755 "$RCOD_PREFIX/etc/rcod"
sudo install -m 0600 config.yaml "$RCOD_PREFIX/etc/rcod/config.yaml"
```

Edit `"$RCOD_PREFIX/etc/rcod/config.yaml"` with your Telegram token, allowed user ID, repository base folder, and optional API settings.

### 2. Install the daemon

From a repository checkout:

```bash
sudo scripts/install-rcod-launchd.sh \
  --mode daemon \
  --service-user "$USER" \
  --config "$RCOD_PREFIX/etc/rcod/config.yaml"
```

The helper will:

- build `cmd/rcodbot` into the recommended macOS prefix unless `--skip-build` is used
- create or reuse the state and log directories under the target user's `~/Library`
- render `/Library/LaunchDaemons/ai.zevro.rcod.plist`
- bootstrap the daemon in the `system` launchd domain
- kickstart it immediately after install

If you already have a prebuilt `rcod` binary, reuse it instead:

```bash
sudo scripts/install-rcod-launchd.sh \
  --mode daemon \
  --service-user "$USER" \
  --config "$RCOD_PREFIX/etc/rcod/config.yaml" \
  --skip-build \
  --bin "$RCOD_PREFIX/bin/rcod"
```

The helper defaults to `/opt/homebrew` on Apple Silicon and `/usr/local` on Intel, but `--bin` and `--config` let you override either path explicitly.

### 3. Day-2 operations

```bash
sudo launchctl print system/ai.zevro.rcod
sudo launchctl kickstart -k system/ai.zevro.rcod
sudo launchctl bootout system/ai.zevro.rcod
sudo launchctl bootstrap system /Library/LaunchDaemons/ai.zevro.rcod.plist
tail -f "/Users/<service-user>/Library/Logs/rcod/rcod.stdout.log" "/Users/<service-user>/Library/Logs/rcod/rcod.stderr.log"
```

## Option 2: per-user `LaunchAgent`

Use this when you want to keep everything in your user account and you are fine with the service starting only after login.

### 1. Prepare the config

```bash
mkdir -p "$HOME/.config/rcod"
cp config.yaml "$HOME/.config/rcod/config.yaml"
chmod 600 "$HOME/.config/rcod/config.yaml"
```

### 2. Install the agent

```bash
scripts/install-rcod-launchd.sh \
  --mode agent \
  --config "$HOME/.config/rcod/config.yaml"
```

That will build to `~/.local/bin/rcod`, render `~/Library/LaunchAgents/ai.zevro.rcod.agent.plist`, and bootstrap it into your `gui/<uid>` launchd domain.

If you prefer to reuse an already installed binary:

```bash
scripts/install-rcod-launchd.sh \
  --mode agent \
  --config "$HOME/.config/rcod/config.yaml" \
  --skip-build \
  --bin /opt/homebrew/bin/rcod
```

### 3. Day-2 operations

```bash
launchctl print "gui/$(id -u)/ai.zevro.rcod.agent"
launchctl kickstart -k "gui/$(id -u)/ai.zevro.rcod.agent"
launchctl bootout "gui/$(id -u)/ai.zevro.rcod.agent"
launchctl bootstrap "gui/$(id -u)" "$HOME/Library/LaunchAgents/ai.zevro.rcod.agent.plist"
tail -f "$HOME/Library/Logs/rcod/rcod.stdout.log" "$HOME/Library/Logs/rcod/rcod.stderr.log"
```

## Dashboard/API notes

If you enable the built-in HTTP dashboard/API on macOS:

- bind it to localhost unless you really need remote access
- keep `api.token` enabled if the dashboard is reachable beyond localhost
- if you publish it on your LAN or internet, put it behind a reverse proxy and upstream auth

Safe reverse-proxy targets are typically:

- `http://127.0.0.1:8080`
- `http://[::1]:8080`

Common macOS-friendly reverse-proxy options include Caddy, nginx, or Traefik.

## Upgrades

After upgrading from source, rerun the installer:

```bash
git pull
sudo scripts/install-rcod-launchd.sh --mode daemon --service-user "$USER" --config "$RCOD_PREFIX/etc/rcod/config.yaml"
```

For `agent` mode:

```bash
git pull
scripts/install-rcod-launchd.sh --mode agent --config "$HOME/.config/rcod/config.yaml"
```

If you upgrade a prebuilt binary in place, rerun the installer with `--skip-build` so the plist stays aligned with your chosen binary and state paths.

## Troubleshooting

### Service starts but RCOD cannot read the config

Make sure the runtime user can read the file:

```bash
ls -l "$RCOD_PREFIX/etc/rcod/config.yaml"
```

In `daemon` mode, the helper will align ownership to the configured service user if the file already exists.

### Service starts but cannot access repositories

The runtime user needs access to `rc.base_folder` and any Git, SSH, or package-manager credentials your workflow uses.

### Agent mode does not survive logout

That is expected. Use `daemon` mode if the service should stay online without an active login session.

### Claude or Codex CLI not found

The templates inject a conservative `PATH` that includes:

- `/opt/homebrew/bin`
- `/usr/local/bin`
- `/usr/bin`
- `/bin`
- `/usr/sbin`
- `/sbin`
- `~/.local/bin`

Verify the runtime user can resolve the tool you need:

```bash
sudo -u "$USER" env PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin which claude
```

Or for Codex:

```bash
sudo -u "$USER" env PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin which codex
```
