# Product gaps

## Addressed for the first binary release

- Install without a source checkout or a frontend toolchain.
- Persistent first-run configuration and encryption keys.
- Help, version, readiness diagnostics, and clear startup failures.
- Linux user-service management and restart after failures.
- Checksummed release archives and an installer that preserves data.
- Public booking origins in copied links and embed snippets.
- Secondary-host archive protection, named icon actions, and meeting-editor notification placement.
- Google setup guide with client JSON import, separate local Desktop and hosted Web app support, and PKCE-protected callbacks. Each installation uses its operator's own Google app.

## Next priorities

1. **Microsoft onboarding.** Bring Microsoft OAuth setup into the UI. Google setup now supports importing the operator's client file; Google project creation and approval remain with Google.
2. **Backup and restore tooling.** A command that snapshots SQLite and includes configuration/uploads, plus a verified restore flow. The current documented method stops the server and copies both directories.
3. **Inbox recovery controls.** Let an operator inspect abandoned tasks and uncertain delivery, reconcile against Sent mail, and explicitly retry where safe. Automatic retries must not duplicate an email that Gmail accepted.
4. **Inbox context and languages.** Better multi-message conversation context, preferences across timezones, and translated replies. The current worker handles one addressed message and a configured meeting type.
5. **Native service support beyond Linux.** macOS launchd integration and Windows packaging. macOS binaries can be run through an existing process manager.
6. **Secondary-host replacement.** The archive dialog can cancel a collective meeting and can reassign a primary host. Replacing only an additional host while preserving all other participants needs its own calendar/notification flow.
7. **External-account qualification.** Repeat the provider fixtures against operator-owned Google, Microsoft, CalDAV, Notion, SMTP/Resend, Zoom, Stripe, and LiveKit accounts, including expiration/revocation recovery. This requires credentials and explicit destination accounts.

The optional inherited video, recording, payments, transcription and booking-chat features remain available. Their existence in source is not evidence that a specific hosted provider account has been configured or tested.
