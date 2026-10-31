# OpenSlot

Free, self-hosted scheduling in one executable. Share a booking link, embed a calendar on your website, or let your existing agent arrange a meeting by email.

No software subscription, model account, or database server is required. Hosting, email delivery, and external providers may have their own costs.

## Install and run

```sh
curl -fsSL https://raw.githubusercontent.com/avifenesh/openslot/main/scripts/install.sh | sh
~/.local/bin/openslot
```

Open **http://127.0.0.1:8788/admin/**. Create your owner account, set your hours, and add a meeting type. Your booking link is ready.

The installer verifies the release checksum. The executable includes the web interface, SQLite support, and timezone data. First run creates persistent configuration and encryption keys. You do not need Go, Node.js, pnpm, Python, or Docker.

[Download releases](https://github.com/avifenesh/openslot/releases) · [Installation and upgrades](docs/getting-started.md) · [Configuration](docs/configuration.md)

For a Linux service that starts at sign-in:

```sh
openslot service install --start
openslot doctor
```

## What it does

- **Booking links and embeds.** Guests book without an account. Inline calendars and popup buttons work on other websites.
- **Availability.** Weekly hours, date overrides, buffers, notice periods, timezone handling, and conflict checks.
- **Meeting management.** Different meeting types, intake questions, rescheduling, cancellation, teams, and host routing.
- **Calendar connections.** Google Calendar, Microsoft 365, iCloud, Fastmail, and other CalDAV servers.
- **Email and Notion.** Booking notifications through Gmail, SMTP, or Resend, plus a dedicated Notion bookings database.
- **Video and payments.** Zoom meetings or LiveKit video rooms with recording, and Stripe payments for paid meeting types.
- **Your agent.** MCP and REST tools for Codex, Claude Code, Antigravity, Cursor, Gemini CLI, VS Code, and provider-based agents.
- **Inbox scheduling.** CC a dedicated Gmail address. Your agent checks availability and replies with valid times. Guests choose and book.

Calendar/email services require their own account setup. OpenSlot does not supply a shared OAuth application or hosted model. The scheduler works without them; connected calendars add external busy times, and a configured sender enables email delivery.

For Google, open **Settings → Google OAuth** and follow the setup guide. Each installation uses a Google project owned by its operator. Import Google's downloaded client JSON, then choose **Connect Google Calendar** or **Connect Gmail**. Local installs accept Desktop app clients; hosted servers use Web application clients. No OpenSlot developer credentials are included in the executable.

## Bring your agent

Open **Your agent**, choose your client, and create an OpenSlot key. Copy the generated setup into that agent. Your model-provider credentials stay there.

OpenAI, Anthropic, Bedrock, GLM, and Grok can be used through supported agent adapters or the external webhook worker. The connection check verifies OpenSlot's tools and key; it does not prove that a remote model account is configured.

For automatic email scheduling, open **Inbox scheduling** and follow the [inbox guide](docs/inbox-agent.md). Start with **Run sample**: it exercises the agent workflow without sending email.

## Put it on your website

Open **Share & embed** and copy the generated snippet. For example:

```html
<script src="https://schedule.example.com/embed.js" async></script>
<openslot-booking slug="intro-call"></openslot-booking>
```

The embed needs no API key. Use a reachable HTTPS deployment when sharing with other people; localhost is only available on your own computer.

## Operations and validation

- [Back up, restore, upgrade, or remove the service](docs/getting-started.md)
- [Provider setup and public deployment](docs/configuration.md)
- [Validation results and account-dependent checks](docs/validation.md)
- [Remaining product gaps](docs/roadmap.md)
- [Build from source and run tests](docs/development.md)

## License and origin

Apache-2.0. OpenSlot is an independent modified fork of [Calnode](https://github.com/Calnode/calnode), whose contributors built the underlying scheduling platform. The license and notices are preserved. Run `openslot licenses` for bundled dependency notices. [Fork details](FORK.md).
