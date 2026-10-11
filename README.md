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

Open **Your agent** in the sidebar. Copy the MCP endpoint and create an API key. Use a remote HTTP MCP client with this starting configuration, adapting the format to your client:

```json
{
  "mcpServers": {
    "openslot": {
      "url": "http://127.0.0.1:8788/mcp",
      "headers": {
        "Authorization": "Bearer YOUR_API_KEY"
      }
    }
  }
}
```

Keep the key in your client's secure configuration. An API key inherits the issuing user's role; it is not a booking-only permission scope. Revoking it stops further requests. OAuth is also inherited from Calnode for clients that support its authorization flow.

A locally running agent can reach this loopback URL. A cloud-hosted agent needs a reachable HTTPS deployment. The app does not send your API key to any model provider.

The same key works with the REST API:

```sh
curl http://127.0.0.1:8788/v1/event-types \
  -H 'Authorization: Bearer YOUR_API_KEY'
```

The MCP transport is stateless and supports the current SDK's 2026-07-28 protocol as well as its supported older protocol versions. Let your MCP client handle protocol headers and request metadata. The API and MCP tools use the same scheduling engine as the booking UI.

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
