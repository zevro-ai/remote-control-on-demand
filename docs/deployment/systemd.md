# Debian/Ubuntu systemd deployment

This guide covers two supported ways to run RCOD under `systemd` on Debian/Ubuntu:

- `system` mode: install the unit globally, but run the process as a non-root service user
- `user` mode: install a per-user service with `systemctl --user`

In both modes, the RCOD process itself runs without root privileges.

## Recommended layouts

### System-wide service

- binary: `/usr/local/bin/rcod`
- config: `/etc/rcod/config.yaml`
- state: `/var/lib/rcod/`
- service unit: `/etc/systemd/system/rcod.service`
- runtime user: dedicated `rcod` user, or an existing Unix account that already has access to your repositories

### User service

- binary: `~/.local/bin/rcod`
- config: `~/.config/rcod/config.yaml`
- state: `~/.local/state/rcod/`
- service unit: `~/.config/systemd/user/rcod.service`
- runtime user: your current account

## Why `--state-dir` matters

RCOD now supports `--state-dir`, so the config file and runtime state no longer have to live in the same directory.

Example:

```bash
/usr/local/bin/rcod --config /etc/rcod/config.yaml --state-dir /var/lib/rcod
```

That keeps secrets/config under `/etc` and mutable session state under `/var/lib`.

## Prerequisites

- Go installed if you want the helper script to build from source
- `claude` available in the runtime user's `PATH`
- a valid `config.yaml`
- the runtime user must have read/write access to the repositories under `rc.base_folder`

If you installed RCOD from the Debian/Ubuntu `.deb` package, Go is not required. Use the packaged helper with `--skip-build` instead.

For system mode, remember that the service user also needs access to:

- the configured `rc.base_folder`
- any local SSH/Git credentials your workflow depends on
- the installed Claude CLI

## Option 1: system-wide service running as a non-root user

Use this when you want RCOD to start on boot without requiring a logged-in desktop session.

### 1. Prepare config and permissions

Create the config directory and place your config there:

```bash
sudo install -d -m 0750 /etc/rcod
sudo install -m 0640 config.yaml /etc/rcod/config.yaml
```

If you plan to run the service as a dedicated `rcod` user, make sure that user can read the config and access your repositories. A common pattern is:

```bash
sudo chgrp rcod /etc/rcod/config.yaml
sudo chmod 0640 /etc/rcod/config.yaml
```

### 2. Install the service

From the repository root:

```bash
sudo scripts/install-rcod-systemd.sh \
  --mode system \
  --service-user rcod \
  --config /etc/rcod/config.yaml \
  --state-dir /var/lib/rcod \
  --bin /usr/local/bin/rcod
```

The helper will:

- build `cmd/rcodbot`
- install the binary
- create or reuse the service user
- create the state directory
- render `/etc/systemd/system/rcod.service`
- run `systemctl daemon-reload`
- run `systemctl enable rcod.service`
- restart the service if it is already active, otherwise start it

If you installed RCOD from a `.deb` package, use the packaged helper instead:

```bash
sudo /usr/lib/rcod/install-rcod-systemd.sh \
  --skip-build \
  --mode system \
  --service-user rcod \
  --config /etc/rcod/config.yaml \
  --state-dir /var/lib/rcod
```

### 3. Day-2 operations

```bash
sudo systemctl status rcod
sudo systemctl restart rcod
sudo systemctl stop rcod
sudo journalctl -u rcod -f
```

## Option 2: per-user systemd service

Use this when you want RCOD to run as your own user account and you prefer user-level service management.

### 1. Prepare config

```bash
mkdir -p ~/.config/rcod
cp config.yaml ~/.config/rcod/config.yaml
chmod 600 ~/.config/rcod/config.yaml
```

### 2. Install the user service

```bash
scripts/install-rcod-systemd.sh \
  --mode user \
  --config "$HOME/.config/rcod/config.yaml" \
  --state-dir "$HOME/.local/state/rcod" \
  --bin "$HOME/.local/bin/rcod"
```

The helper will render `~/.config/systemd/user/rcod.service`, enable it, and then either restart or start it depending on whether it is already active.

If you prefer to reuse the packaged binary for a user service, run:

```bash
/usr/lib/rcod/install-rcod-systemd.sh \
  --skip-build \
  --mode user \
  --config "$HOME/.config/rcod/config.yaml" \
  --state-dir "$HOME/.local/state/rcod" \
  --bin /usr/bin/rcod
```

### 3. Keep it alive after logout

If you want the user service to survive logout and start on boot, enable lingering:

```bash
sudo loginctl enable-linger "$USER"
```

### 4. Day-2 operations

```bash
systemctl --user status rcod
systemctl --user restart rcod
systemctl --user stop rcod
journalctl --user -u rcod -f
```

## Manual installation

If you do not want to use the helper script, use the templates directly:

- system unit: [packaging/systemd/rcod.service](../../packaging/systemd/rcod.service)
- user unit: [packaging/systemd/rcod.user.service](../../packaging/systemd/rcod.user.service)

Replace the placeholders before installing the unit.

## Dashboard/API notes

If you enable the built-in HTTP dashboard/API:

- make sure `api.port` is set in `config.yaml`
- expose the port with a firewall rule or reverse proxy only if you actually need remote access
- if you publish it beyond localhost, keep `api.token` enabled unless you have a stronger upstream auth setup

Example reverse-proxy targets:

- `http://127.0.0.1:8080`
- `http://[::1]:8080`

## Upgrades

Re-run the installer after pulling a newer version:

```bash
git pull
sudo scripts/install-rcod-systemd.sh --mode system --service-user rcod --config /etc/rcod/config.yaml
```

Or for user mode:

```bash
git pull
scripts/install-rcod-systemd.sh --mode user --config "$HOME/.config/rcod/config.yaml"
```

If you upgrade via the packaged `.deb`, see [deb.md](./deb.md) for the package-based flow.

## Troubleshooting

### Service cannot read the config

Check ownership and mode:

```bash
ls -l /etc/rcod/config.yaml
```

The runtime user must be able to read it.

### Service starts but cannot access repositories

The configured runtime user must have access to `rc.base_folder` and any Git credentials it needs.

### `systemctl --user` service stops after logout

Enable lingering:

```bash
sudo loginctl enable-linger "$USER"
```

### Claude CLI not found

Verify that the runtime user can resolve `claude`:

```bash
sudo -u rcod env PATH=/usr/local/bin:/usr/bin:/bin which claude
```
