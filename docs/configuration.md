# Configuration and deployment

## Where configuration lives

A standalone first run writes `config.env` under the OS user configuration directory and stores SQLite/uploads under its user data directory. On Linux, `XDG_CONFIG_HOME` and `XDG_DATA_HOME` are honored; the defaults are `~/.config/openslot` and `~/.local/share/openslot`.

Configuration selection, in order:

1. `--config FILE` or `OPENSLOT_CONFIG`.
2. `.env` in the current directory, for existing deployments.
3. An existing user configuration file.
4. Existing server environment variables, for containers and supervisors.
5. A new standalone configuration, created only by `openslot serve` or `openslot init`.

Environment variables override file values. Relative `DATA_DIR` and `DATABASE_URL` values from a file are resolved relative to that file (the target file when using a symlink). Relative environment overrides keep their working-directory meaning. An explicit missing configuration file is an error when serving; use `init` to create it.

```sh
openslot init --config /path/to/config.env --data-dir /path/to/empty-data
openslot serve --config /path/to/config.env
```

Initialization never overwrites existing keys. It refuses to generate new keys for a nonempty data directory. Restore that installation's original configuration instead.

## Useful commands

```sh
openslot --help
openslot version --json
openslot serve --port 9000
openslot doctor --url http://127.0.0.1:9000
openslot service install --config /path/to/config.env --start
```

Help, version, licenses and invalid commands never start a server. A port conflict returns a failure before opening the database. `doctor` checks the persistent configuration and the running server's readiness/version without printing secrets.

## Public deployment

Claim the owner account locally first. Put an HTTPS reverse proxy in front of the local listener, and set the public origin in the configuration:

```dotenv
BIND_HOST="127.0.0.1"
PORT="8788"
BASE_URL="https://schedule.example.com"
```

Restart OpenSlot after changing these settings. The proxy forwards to `127.0.0.1:8788`. If the proxy runs elsewhere, choose an appropriate bind address and firewall policy. Configure `TRUSTED_PROXY_CIDRS` only for the proxy networks you operate.

`BASE_URL` is the admin and OAuth origin. `PUBLIC_BASE_URL` can provide a separate booking origin. Booking links, copied embed snippets and notification links use that public origin. The admin embed preview uses its local server so it can render before public DNS is reachable.

A localhost URL cannot be used by guests or a remotely hosted agent. Cloud MCP clients need the configured HTTPS endpoint to be reachable from their provider.

## Apps and email

| Connection | Required setup |
| --- | --- |
| Google Calendar / Gmail | Follow Settings → Google OAuth. Create your own Google app, import its downloaded client JSON, then connect your account. |
| Microsoft 365 / Outlook | Set `MICROSOFT_CLIENT_ID`, `MICROSOFT_CLIENT_SECRET` and, if needed, `MICROSOFT_TENANT`; register the calendar/auth callbacks before connecting. |
| CalDAV | Choose iCloud, Fastmail or a custom server and provide its app-specific credentials. |
| SMTP / Resend | Configure the selected sender in Settings → Email, then use its test control. |
| Notion | Create an integration with read/insert/update access and share a parent page with it. OpenSlot creates its own bookings database. |
| Zoom, Stripe, LiveKit, recording/storage | Configure the optional service in Settings. These are inherited integrations; see the linked advanced guides and validation report. |

Gmail requests read/send access and becomes the workspace sender. Inbox listening is a separate setting. Disconnecting Gmail restores the configured SMTP/Resend sender. Notion synchronizes future bookings automatically; copying existing bookings requires the explicit backfill action.

### Your own Google app

OpenSlot does not route Google authorization through a shared service or ship a developer's OAuth client. Each operator owns their Google Cloud project and connects their own accounts.

1. Create or select your project in [Google Cloud](https://console.cloud.google.com/). Enable the Google Calendar API and Gmail API.
2. Configure Google Auth platform branding and audience. In Testing mode, add each connecting account as a test user.
3. Create an OAuth client. For the default local installation, choose **Desktop app**. For a server on a public domain, choose **Web application** and register all three callback URLs displayed in OpenSlot.
4. Download the client JSON. In **Settings → Google OAuth**, select that file. OpenSlot imports and encrypts the matching credential pair. It never returns the secret in its settings response.
5. Connect Calendar and Gmail separately. Each button opens Google's account chooser and consent screen. The Apps page shows the accounts that actually connected; saved app credentials alone do not mean consent succeeded.

Desktop clients require an HTTP loopback `BASE_URL` whose port matches `PORT`, and a loopback `BIND_HOST`. Moving that installation to a public server requires a Web application client. The server rejects Desktop client imports when those local-listener conditions are not met.

To replace an app that has connected calendars, open **Enter credentials manually → Reset Google connections**. Confirming disconnects Gmail and every member's Google Calendar, including archived members. Existing Google events remain; calendar conflict checks and Gmail delivery need accounts to reconnect. Import the replacement client file afterward. Manual entry includes an explicit application-type selector for Desktop-to-Web migrations.

Google's Testing authorizations and refresh tokens expire after seven days for these scopes. Review [Google's audience rules](https://support.google.com/cloud/answer/15549945) and [verification exceptions for personal or internal use](https://support.google.com/cloud/answer/13464323) before choosing publishing settings. Public use of Gmail reading permissions can require Google's restricted-scope verification and a security assessment. Importing a client file does not bypass Google approval.

Google sign-in and Calendar connections use PKCE and short-lived, single-use browser state. Gmail also uses PKCE. Tokens stay on this installation; the browser only carries the temporary authorization code.

Model-provider keys belong to your external agent. OpenSlot's own API keys are separate and can be revoked from the UI. The optional inherited booking-chat settings are also separate from inbox scheduling.

Use [.env.example](../.env.example) for the full environment reference, [Zoom](ZOOM.md), [video](VIDEO.md), and [domains](DOMAINS.md) for advanced deployment details. Some inherited documents retain Calnode names; those names do not identify an OpenSlot hosted service.

## Existing installations

Keep the original configuration and data. Install the executable, then point it at the existing file:

```sh
openslot serve --config /existing/installation/.env
```

For a previously hand-written systemd user unit, `service install` refuses to overwrite it silently. Review it, then use `--replace` if desired. The original is saved as `openslot.service.bak`. Managed OpenSlot units can be updated normally.

```sh
openslot service install --config /existing/installation/.env --replace --start
```

Do not run two server processes against the same installation. The listening-port check prevents a duplicate listener; using different ports is a separate instance and should use a separate configuration and data directory.

## Recovery

The inherited recovery commands remain available with `--config`:

```text
openslot reset-admin EMAIL NEW-PASSWORD --config FILE
openslot rotate-key NEW-SECRET --config FILE
openslot recover-key NEW-SECRET --config FILE
```

Key rotation/recovery must be followed by updating the matching configuration value before restarting. Keep secrets out of shared shell history and logs. The platform key, recovery key, and SQLite database are distinct parts of the recovery process; see the architecture guide for the key-vault design.

Agent workers retain `OPENSLOT_API_KEY` and `OPENSLOT_WEBHOOK_TOKEN` from a local `.env`, with environment values taking precedence. Other server secrets are not loaded into the external agent process. Configure model credentials in the agent itself.
