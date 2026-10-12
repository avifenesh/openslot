# Inbox scheduling

Include a dedicated Gmail mailbox in an email's To or CC field. OpenSlot queues the request. Your existing agent reads that one email through MCP, finds available times, and sends a slot offer to the original sender. Guests finish booking on the normal booking page.

## Setup

1. Configure Google OAuth in Settings and enable the Gmail API in the Google Cloud project. Add the Gmail callback URL shown under App connections.
2. Connect the dedicated mailbox under App connections. Grant `gmail.readonly` and `gmail.send`. An existing send-only connection needs to be reconnected. Gmail also becomes the workspace email sender.
3. Open Inbox scheduling. Enter the exact connected mailbox address, choose an active public meeting type and an IANA timezone, then save. Aliases and body mentions are not triggers.
4. Create an administrator's OpenSlot API key under Your agent. Set `OPENSLOT_API_KEY` in the worker environment. Provider credentials stay with the existing agent.
5. Start one of the workers below. Queue a sample from Inbox scheduling first. A sample follows the same MCP flow and renders the reply in activity, but cannot send email.
6. Enable automatic sending and save. Only messages received from this point onward are eligible. Saving settings cancels requests queued under the previous configuration.

Use a service manager to keep the worker running. The foreground process stops on SIGINT or SIGTERM. `--once` processes at most one request and exits. `--model` selects a model available to the local agent account; `--binary` selects the installed CLI executable.

### Codex

```sh
./openslot agent --url https://schedule.example.com --engine codex
```

Requires a current Codex CLI with `exec --ignore-user-config`, MCP HTTP support and per-server automatic tool approvals. It uses existing Codex authentication, a fresh ephemeral session, a read-only sandbox, disabled shell/browser/plugins, and an explicit allowlist of the four inbox tools. General user configuration is not imported into an email-processing session. The worker supports Codex's default authenticated provider; for a custom provider runtime use the webhook bridge.

### Claude Code

```sh
./openslot agent --url https://schedule.example.com --engine claude
```

Requires a current Claude Code CLI with restricted mode. It uses existing Claude Code authentication, `--restricted`, no built-in tools, a strict MCP configuration, and an allowlist for the inbox server's tools. Session persistence is disabled.

### External agent webhook

```sh
./openslot agent --url https://schedule.example.com --engine webhook \
  --webhook-url https://agent.example.com/inbox
```

Set `OPENSLOT_WEBHOOK_TOKEN` in the worker environment if the endpoint requires bearer authentication. The receiving agent may use OpenAI, Anthropic, Bedrock, GLM, Grok, or any other provider. Model keys never pass through OpenSlot.

The worker sends a JSON POST:

```json
{
  "id": "request-id",
  "mcp_url": "https://schedule.example.com/mcp/inbox",
  "token": "temporary-task-token",
  "expires_at": 1800000000,
  "instruction": "Instructions for the scheduling agent"
}
```

`Idempotency-Key` is the request ID. Return a 2xx status after accepting the task. The agent connects to `mcp_url` using `Authorization: Bearer <token>` and completes one of the terminal tools below. The worker checks the durable receipt; accepting the webhook alone does not mark the request complete. A remote agent must be able to reach the supplied MCP URL, so use a public HTTPS OpenSlot URL for remote runtimes. Plain HTTP is allowed only on loopback.

## Task MCP tools

| Tool | Action |
| --- | --- |
| `read_email_request` | Read the one assigned email, configured meeting type, timezone and current time. Attachments are excluded. |
| `find_email_slots` | Query live availability for that meeting type, with `from` and `to` dates. Calendar outages fail closed. |
| `send_slot_offer` | Supply exact returned `starts`. OpenSlot checks them again, renders the email and booking link, and replies to the original sender in the Gmail thread. |
| `ignore_email_request` | Finish without replying to unrelated messages or acknowledgements. |

The token cannot read other mail, change configuration, create bookings, or invoke the general scheduling MCP. The worker's administrator key is stripped from local agent subprocess environments. Untrusted email content is not allowed to choose recipients, email headers, free-form outbound text or meeting types.

## Delivery and recovery

- The listener scans every minute. It searches from activation and skips known message IDs, so delayed Gmail indexing does not lose a request. Deduplication uses mailbox identity plus message ID and survives reconnects.
- Only exact parsed To/CC matches qualify. Sent, draft, spam, trash, self-sent, mailing-list and auto-submitted messages are excluded. The message's From address is the reply recipient; Reply-To cannot redirect it.
- Text and HTML email are supported, without attachments. Bodies over 64 KiB and MIME API responses over 2 MiB are skipped to bound model input and memory. A deleted, malformed or oversized individual message does not block later messages.
- A worker has ten minutes to finish. Its task lease lasts fifteen minutes, leaving time for cancellation and receipt checks. An abandoned processing task is reclaimable after expiration. Workers retry failed claim requests after thirty seconds.
- Before Gmail delivery, the request is durably marked `sending`. A failure without provider confirmation becomes `uncertain`. Neither state is automatically retried, because Gmail may already have accepted the message. Check Gmail Sent mail before acting manually. A process crash during delivery leaves the same inspectable `sending` state.
- Offers do not reserve availability. Slots are rechecked immediately before sending and again by the normal booking flow when the guest books.
- Message text is encrypted at rest with the application's data key. Activity shows delivery state and the generated reply, not the incoming message body. Provider access can be revoked under App connections, and pausing invalidates outstanding task leases.

## Verification

Hermetic tests cover Gmail pagination, padded MIME bodies, deleted and oversized messages, To/CC matching, activation filtering, reconnect deduplication, threaded replies, lease revocation, stale slot rejection, uncertain-delivery handling, webhook completion and local process credential isolation. Browser tests use a separate SQLite database and synthetic requests.

Installed-runtime smoke tests are opt-in and use synthetic mail without Gmail credentials:

```sh
OPENSLOT_TEST_ENGINE=codex go test ./internal/handler -run '^TestInboxInstalledAgentRuntime$' -count=1
OPENSLOT_TEST_ENGINE=claude go test ./internal/handler -run '^TestInboxInstalledAgentRuntime$' -count=1
```

These runs use the installed agent's existing model authentication. They do not validate real Gmail account consent or provider-side delivery.
