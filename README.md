# OpenSlot

Free, self-hostable scheduling. Share a booking link, put it on your website, or let your existing agent manage it through MCP or the REST API.

OpenSlot is a modified fork of [Calnode](https://github.com/Calnode/calnode), released under [Apache-2.0](LICENSE). It is an independent project. See [FORK.md](FORK.md) for attribution, inherited features, and changes.

## What you can do

- Set working hours and create meeting types with different durations.
- Share public booking pages. Guests need no account.
- Embed an inline calendar or a popup booking button on another website.
- Connect an existing agent to find availability, create, read, reschedule, and cancel bookings.
- Create separate API keys for agents and revoke them from the app.
- Connect Google, Microsoft, or CalDAV calendars through the inherited integrations.
- Use a simpler dashboard with setup steps, upcoming meetings, and meeting links, in light or dark mode.

There are no software subscription tiers. Hosting, a domain, email delivery, and your own agent provider may have their own costs. The scheduler works without a model or an AI subscription.

## Run locally

Requires Go 1.26 or newer, Node.js 22.12 or newer, pnpm 10.32.1, and Python 3 for the first-run configuration helper.

```sh
make build
make run
```

Open **http://127.0.0.1:8788/admin/** and create the owner account. Then choose your hours and create a meeting type. Connect a calendar to include meetings booked elsewhere in conflict checks.

The launch helper creates `.env` once, with randomly generated persistent encryption keys, and listens only on `127.0.0.1`. It never replaces an existing `.env`. The database and uploaded files live in `data/`. Keep `.env` and `data/` in a secure backup. An inherited deployment can retain its existing configuration.

The Go executable embeds the built frontend. After a frontend change, run `make build` and restart the server. Restarting an old executable does not update the UI.

## Connect your agent

Open **Your agent** and choose a connector. OpenSlot generates the configuration format for Codex, Claude Code, Claude, Google Antigravity, Cursor, Gemini CLI, VS Code/Copilot, or another HTTP MCP client. Model-provider adapters cover OpenAI, Anthropic, Amazon Bedrock through AgentCore, GLM/Z.ai, and Grok/xAI.

Create a separate OpenSlot key for each agent. Copy or download its configuration, then run **Check OpenSlot connection** to verify the key and discover scheduling tools. This check reads the tool catalog. It does not send a model request, change bookings, or prove the external agent has connected. A key's last-used time includes this check.

Provider API keys stay in your agent environment. OpenSlot stores only a hash of each OpenSlot key and shows its plaintext value once. Keys inherit your account's role. They can read and change data allowed by that role, so use a member account when that is the access the agent should have. Revoke a key to stop further requests.

The provider examples read model IDs from your environment instead of pinning a model that may be unavailable in your account. Codex reads `OPENSLOT_API_KEY` from its launch environment. API examples list the additional environment variables and SDK dependency on the setup screen. OpenAI approval requests continue through your agent's approval flow; xAI does not support OpenAI's `require_approval` parameter.

Claude's app connector uses the existing OAuth sign-in flow. Review and revoke those grants under **Agent authorizations**. The Bedrock adapter is an AgentCore harness tool entry; agents using Bedrock through Codex or Claude Code use that client's adapter.

Cloud-hosted clients need a publicly reachable HTTPS deployment. Localhost works only for clients on the same computer. The setup screen flags addresses that are visibly local, but it cannot prove external DNS or firewall reachability.

The same key works with the REST API. The MCP and REST interfaces share the scheduling engine. A browser check and protocol tests cover authenticated tool discovery, invalid keys, and revoked keys. Generated JSON, TOML, and Python examples are parsed during validation; live model-provider accounts are not used by the test suite.

Provider setup pages are linked from every adapter. Formats were checked against [Codex](https://developers.openai.com/codex/mcp), [Anthropic](https://platform.claude.com/docs/en/agents-and-tools/mcp-connector), [Antigravity](https://www.antigravity.google/docs/mcp), [OpenAI](https://developers.openai.com/api/docs/guides/tools-connectors-mcp), [AgentCore](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/harness-tools.html), [Z.ai](https://docs.z.ai/guides/capabilities/mcp-call), and [xAI](https://docs.x.ai/developers/tools/remote-mcp) documentation on 2026-10-12.

## Connect your apps

Open **App connections** for Google Calendar, Outlook/Microsoft 365, CalDAV calendars, Gmail, and Notion. Calendar connections belong to each host. Gmail and Notion are workspace connections managed by administrators.

### Google Calendar, Outlook, and CalDAV

Use the calendar controls to connect accounts, choose which calendars block availability, and select the calendar that receives bookings. Google and Microsoft require configured OAuth applications. CalDAV supports iCloud, Fastmail, Nextcloud, and custom servers with app-specific credentials.

### Gmail

Configure Google OAuth in **Settings**, enable the Gmail API in Google Cloud, and register the exact Gmail callback URL shown in **App connections**. Connect Gmail and approve send access. The flow requests `gmail.send` plus account identity, uses PKCE and single-use state bound to the signed-in administrator, and stores the refresh token encrypted.

Connected Gmail becomes the workspace sender for booking notifications and authentication emails. Calendar attachments are preserved. Disconnecting restores the SMTP/Resend sender configured under **Settings → Email**. A Gmail error is reported rather than silently switching the sending account. The email settings test still tests the SMTP/Resend configuration being edited.

The Gmail **Check** button refreshes credentials and verifies account identity without sending email. Actual delivery and token refresh are covered by tests against a simulated Google API. Real-account authorization and mail delivery require your credentials and Google consent.

### Notion

Create a Notion internal integration with read, insert, and update content access. Share a parent page with it. Enter the integration token and parent page URL in **App connections**, then choose **Connect and create bookings database**. This creates a dedicated database under that page; it does not alter an existing database's schema.

New bookings and changes queue background synchronization of meeting titles, guest names and emails, start/end times, booking status, and pending payment state. Retries update the same page using the stable OpenSlot booking ID. Page content and other properties added by users are preserved. Disconnection stops further sync and keeps existing Notion pages. A new connection has a new generation so old queued jobs cannot copy bookings into a different destination.

Use **Sync existing bookings** to opt into copying booking history. The confirmation explains what will be copied. **Check** verifies access and required database properties. Delivery errors appear on the connection card. Requests are paced for Notion's standard 180-per-minute connection budget. Provider Retry-After responses defer jobs without blocking the worker or spending attempts on unsent requests. Tests simulate Notion creation, updates, cancellation, retry recovery, and disconnection; no real workspace is used in automated checks.

App credentials are encrypted in the local database. Model-provider credentials remain in the external agent.

## Embed on your website

Open **Share & embed**, select a meeting, then copy the generated snippet. For a deployment at `https://schedule.example.com`:

```html
<script src="https://schedule.example.com/embed.js" async></script>
<openslot-booking slug="intro-call"></openslot-booking>
```

Or use a popup:

```html
<script src="https://schedule.example.com/embed.js" async></script>
<button data-openslot-popup="intro-call">Find a time</button>
```

The widget uses Shadow DOM to keep the booking interface separate from the host site's styles. Popup buttons also work when inserted after the script loads. Listen for the bubbling `openslot:booked` event to respond to a successful booking.

The embed never needs an API key. Configure `EMBED_ALLOWED_ORIGINS` to restrict which browser origins can use the public booking API. These endpoints remain public; CORS is not authorization.

## Calendar sync, email, and deployment

The local app can accept bookings without calendar or email credentials. It checks its own bookings and availability rules. External busy times require a connected calendar; confirmation emails require SMTP or Resend configuration.

- **Calendars** connects calendar accounts. Configure Google OAuth under **Settings** first, or Microsoft credentials through the environment.
- **Settings → Email** configures outgoing mail. A booking confirmation in the browser is not proof an email was delivered.
- Set `BASE_URL` to the externally reachable HTTPS address when deploying. Set `BIND_HOST` for your network or proxy setup. Keep encryption keys stable.
- Use `.env.example` and the inherited `docs/` references for provider configuration. These references retain upstream names and may describe advanced inherited features outside the main OpenSlot flow.
- An optional separate `PUBLIC_BASE_URL` is inherited for public links. The simplified share UI currently assumes a single public/admin origin.

The locally verified flows cover owner setup, booking creation and cancellation, cross-origin embeds, conflict handling, MCP authentication, API key revocation, and responsive themes. Live OAuth connections and email delivery require operator credentials and have not been verified against real accounts in this fork.

## Development checks

```sh
cd frontend
pnpm install --frozen-lockfile
pnpm test:browsers
cd ..
make build
make check
```

Browser binaries are installed into `frontend/.playwright` with cache cleanup disabled so other projects' browsers are untouched. Browser tests launch a temporary local server, use synthetic accounts and no real provider credentials, and remove their database afterward. Logs and screenshots go in ignored `artifacts/`.

The backend is Go with SQLite. The admin UI is Svelte 5. The public booking pages and embed are framework-free. Internal Go imports, database identifiers, `CALNODE_*` environment variables, and legacy embed aliases remain compatible with the upstream foundation.
