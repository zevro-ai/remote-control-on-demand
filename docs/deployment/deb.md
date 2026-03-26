# Debian/Ubuntu `.deb` package

RCOD releases can ship a `.deb` package so Debian/Ubuntu users can install and upgrade the bot with normal package-management workflows instead of copying binaries by hand.

## What the package installs

- binary: `/usr/bin/rcod`
- systemd installer helper: `/usr/lib/rcod/install-rcod-systemd.sh`
- systemd unit templates: `/usr/share/rcod/systemd/`
- example config: `/usr/share/doc/rcod/examples/config.example.yaml`
- deployment guides: `/usr/share/doc/rcod/`
- package-owned directories: `/etc/rcod` and `/var/lib/rcod`

The package does **not** create a live `config.yaml` for you, and RCOD still runs as a normal user or dedicated service user, not as root.

## Install

Download the `.deb` artifact for your architecture from a release, then install it:

```bash
sudo dpkg -i rcod_<version>_linux_amd64.deb
```

Or with `apt` for automatic dependency handling:

```bash
sudo apt install ./rcod_<version>_linux_amd64.deb
```

## First-time setup

### 1. Create the runtime config

Start from the packaged example:

```bash
sudo install -m 0640 /usr/share/doc/rcod/examples/config.example.yaml /etc/rcod/config.yaml
```

The package already creates `/etc/rcod`. Then edit `/etc/rcod/config.yaml` with your Telegram token, allowed user ID, repository base folder, and any API settings you need.

### 2. Install the systemd unit

Use the packaged helper and tell it to reuse the already-installed binary:

```bash
sudo /usr/lib/rcod/install-rcod-systemd.sh \
  --skip-build \
  --mode system \
  --service-user rcod \
  --config /etc/rcod/config.yaml \
  --state-dir /var/lib/rcod
```

That will:

- reuse `/usr/bin/rcod`
- create or reuse the `rcod` service user
- render `/etc/systemd/system/rcod.service`
- enable the service
- restart it on upgrades if it is already active, otherwise start it

For a per-user service instead:

```bash
/usr/lib/rcod/install-rcod-systemd.sh \
  --skip-build \
  --mode user \
  --config "$HOME/.config/rcod/config.yaml" \
  --state-dir "$HOME/.local/state/rcod" \
  --bin /usr/bin/rcod
```

If you use the user-mode flow, prepare your user config first and consider `loginctl enable-linger "$USER"` if the service should survive logout.

## Upgrade

Install the newer package with `apt` or `dpkg`:

```bash
sudo apt install ./rcod_<new-version>_linux_amd64.deb
```

If you already use the packaged systemd helper flow, re-run it after the upgrade so the unit stays aligned with your config and service user choices:

```bash
sudo /usr/lib/rcod/install-rcod-systemd.sh \
  --skip-build \
  --mode system \
  --service-user rcod \
  --config /etc/rcod/config.yaml \
  --state-dir /var/lib/rcod
```

The helper will restart the service if it is already running.

## Remove and purge

Remove the package but keep local data:

```bash
sudo apt remove rcod
```

Purge the package metadata:

```bash
sudo apt purge rcod
```

Important:

- your live `/etc/rcod/config.yaml` may still remain if you created it yourself after installation
- your state files under `/var/lib/rcod` may also remain if they are not empty
- stop and disable the systemd unit before deleting config/state on purpose

## Build the package locally

The project uses GoReleaser + nFPM for `.deb` packaging. To build snapshot artifacts locally:

```bash
goreleaser release --snapshot --clean --skip=publish
```

That will produce the `.deb` package in `dist/` together with the normal release archives.

## Related docs

- [Debian/Ubuntu systemd deployment](./systemd.md)
- [README](../../README.md)
