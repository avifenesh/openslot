# Validation

This report separates executable/local behavior from external-account setup. No real provider credentials or customer data are used by the automated suite.

## v0.1.0 local validation (2026-10-12)

- Full Go suite: 30 passing packages and 1,211 passing test cases, including subtests. The opt-in installed-agent runtime test was skipped; five other packages contain no tests.
- Public booking/video JavaScript: 24 tests passed.
- Frontend type checks: zero errors and zero warnings. Ten browser component-style tests passed.
- Executable smoke: private first-run configuration, owner signup, availability, meeting creation, booking, restart persistence, cancellation, readiness and stopped-server diagnostics passed.
- Installer: valid local archive installation passed; a corrupted archive was rejected without replacing the existing executable.
- CLI/service tests: help/version without side effects, invalid arguments, occupied port, relative SQLite paths, credential precedence, service replacement backup and uninstall preservation passed. The generated unit also passes the real systemd parser with paths containing spaces, dollar signs and percent signs. Linux systemd startup is also checked on the installed release.
- Fresh review: agent `.env` compatibility, SQLite URI anchoring and bundled notices were corrected and re-reviewed.

Browser and shipped-asset results are added to the release notes after checking the final executable. External accounts listed below were not exercised live.

| Functionality | Validation approach | External prerequisite |
| --- | --- | --- |
| Install, init, CLI, persistence, service | Fresh-directory executable tests, checksum/tamper tests, configuration/upgrade checks | Linux systemd for the built-in service helper |
| Availability, overrides, booking links, embeds | Go scheduling/integration tests, public JavaScript tests, browser CRUD/conflict/reschedule tests | None for local bookings |
| Accounts, sessions, roles, API keys, OAuth/MCP grants | Handler/HTTP tests and synthetic owner/member/browser flows | Configured provider for social sign-in |
| Meeting types, questions, teams and routing | Scheduling/booking/handler tests and browser editor workflows | Calendar providers for external conflicts |
| Gmail, Notion, Google/Microsoft/CalDAV calendars | API fixtures, encryption/refresh/retry tests, local setup forms | Your provider credentials and consent |
| Inbox scheduling | Synthetic addressed mail, task leases, slot revalidation, reply fixtures, webhook worker; installed-runtime smoke where available | A connected mailbox and a running external agent |
| Agent/provider adapters | Config format checks, authenticated MCP discovery, API-key revocation; no provider keys stored in OpenSlot | An authenticated existing agent/model account |
| SMTP/Resend, invites and notifications | MIME/ICS/transport fixtures, worker and delivery-state tests | A real configured sender for delivery |
| Webhooks and jobs | Signing, delivery, retries, leases, HTTP validation and UI CRUD tests | A reachable receiver for live delivery |
| Stripe payments | Handler/client/webhook fixtures and local settings validation | A Stripe account and webhook setup |
| Zoom and LiveKit/video/recording | Provider/token/access-control fixtures and local settings validation | Provider credentials and running media/storage services |
| Transcription and optional booking chat | STT/LLM client and booking-tool fixtures | Configured model/STT endpoints |
| UI | Both themes, desktop/mobile screenshots, keyboard and error states | None |

Release archives for non-native architectures are cross-built. Cross-compilation does not establish that a service was started or a browser flow ran on those operating systems. The native Linux amd64 release is validated by downloading/extracting and running the shipped executable in an isolated installation.
