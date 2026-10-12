# Getting started

## Install

Download a release with the installer:

```sh
curl -fsSL https://raw.githubusercontent.com/avifenesh/openslot/main/scripts/install.sh | sh
~/.local/bin/openslot
```

The installer chooses Linux or macOS and your CPU architecture, verifies the SHA-256 checksum, and installs one executable in `~/.local/bin`. It needs `curl`, `tar`, and `sha256sum` or `shasum`. Running OpenSlot needs no Go, Node.js, Python, database server, or model account.

To pin a version or choose a directory:

```sh
curl -fsSL https://raw.githubusercontent.com/avifenesh/openslot/main/scripts/install.sh | \
  sh -s -- --version v0.1.0 --dir "$HOME/.local/bin"
```

For a manual or offline install, download the matching archive and `SHA256SUMS` from the release, then run its included installer:

```sh
tar -xzf openslot_v0.1.0_linux_amd64.tar.gz install.sh
sh install.sh --version v0.1.0 \
  --archive openslot_v0.1.0_linux_amd64.tar.gz --checksums SHA256SUMS
```

A private repository requires an authenticated GitHub download. The local-archive installation works with files downloaded through `gh release download` too. Never put a GitHub token in a copied installer command.

## Create your first booking link

1. Run `openslot`, or use `~/.local/bin/openslot` if your shell cannot find it.
2. Open **http://127.0.0.1:8788/admin/** and create the owner account.
3. Set your timezone and weekly hours under **Availability**.
4. Create a **Meeting type**. Choose its duration and location.
5. Open **Share & embed** and copy the booking link. Guests need no account.

The first run creates persistent configuration and encryption keys. Restarting uses the same database. OpenSlot initially listens only on this computer. Use [configuration and deployment](configuration.md) before sharing a link with people elsewhere.

You can book without connecting a calendar or email provider. OpenSlot checks its own bookings. Connect a calendar to include other busy events; connect Gmail or configure SMTP/Resend to send email notifications.

## Keep it running

On Linux with systemd:

```sh
openslot service install --start
openslot doctor
```

The user service starts at sign-in and restarts after a failure. On a headless server, enable user lingering through your system's login manager if it must start before anyone signs in. This does not require changing OpenSlot's data or keys.

```sh
openslot service status
openslot service restart
openslot service stop
```

On macOS or Linux without systemd, run `openslot serve` through your existing process manager. Built-in macOS service management is not included in this release.

## Connect your tools

- **App connections**: Google Calendar, Outlook, CalDAV, Gmail and Notion. OAuth providers require your own registered application and consent. CalDAV uses an app-specific password.
- **Your agent**: choose Codex, Claude Code, Antigravity or another supported app/provider. Create an OpenSlot key and copy its configuration. Model credentials stay in your agent.
- **Inbox scheduling**: include a dedicated Gmail mailbox in To or CC. An existing agent can reply with checked times. Start with a synthetic sample. Follow the [inbox guide](inbox-agent.md).

OpenSlot does not provide a shared Google OAuth application or hosted agent. The setup screens show the permissions, callback URLs and credentials needed for each connection.

## Upgrade

Rerun the installer, then restart your service:

```sh
openslot service restart
openslot version
openslot doctor
```

The installer replaces only the executable. It preserves configuration and data. Back up before upgrading: new releases may include database migrations, so an old executable is not always a safe rollback.

## Back up and restore

Run `openslot doctor` to locate the configuration and data directory. With the default Linux installation these are:

- `~/.config/openslot/config.env`
- `~/.local/share/openslot/`

Stop OpenSlot, copy both into a protected backup, then start it again. If you configured `DATABASE_URL` outside the data directory, copy that database and its SQLite sidecar files as well. Stopping first makes the SQLite database and uploaded files consistent. The configuration contains the keys needed to decrypt connected-account credentials. Keep it private and preserve the original keys.

To restore, stop the service and restore both the configuration and data. If the storage location changes, update `DATABASE_URL` and `DATA_DIR` in the restored configuration. Keep the encryption and recovery keys unchanged. Start the service and run `openslot doctor`.

Automatic backup and restore commands are not included yet. Existing R2/Litestream deployment support is separate from the standalone binary.

## Remove the service

```sh
openslot service uninstall
```

This stops and removes the user service. It keeps the executable, configuration and data. Remove those separately only when you no longer need the installation.
