# Fork provenance

OpenSlot is an independent modified distribution of [Calnode](https://github.com/Calnode/calnode).

- Upstream source: `https://github.com/Calnode/calnode.git`
- Base revision: `9502e61ecf1b6c89014adc069d71743c50cdf68f`
- Code license: Apache License 2.0, preserved in `LICENSE`.
- Upstream naming policy: preserved in `TRADEMARK.md`.

Calnode's contributors built the scheduling engine, database and migrations, calendar connectors, booking pages, API, MCP tools, authentication, email and webhook delivery, and original embed. OpenSlot does not claim these as original work.

OpenSlot changes the product identity, main navigation and dashboard; adds dedicated agent connection and share/embed screens; improves popup keyboard interaction and dynamic triggers; adds OpenSlot widget names while retaining legacy aliases; fixes host-timezone formatting; enables the SDK's current stateless MCP protocol; and adds browser verification of the resulting product flows. It also adds client-specific agent adapters, Gmail OAuth sending, and Notion booking synchronization.

The Go module path remains `github.com/calnode/calnode` to preserve internal imports. API key prefixes, environment names, database migrations, and legacy `calnode-booking`, `Calnode`, and `calnode:booked` interfaces remain compatible. These identifiers indicate compatibility, not affiliation.

Inherited optional capabilities, including video, payments, and a built-in model integration, remain in the source. The main OpenSlot experience focuses on deterministic scheduling and connecting an existing external agent. No model credentials are required. Advanced integration code retained from upstream does not imply it was qualified against real external accounts in this fork.

This repository has not published an OpenSlot container or hosted service. Upstream release images and deployment links retained in historical documentation refer to Calnode, not OpenSlot. Install the standalone executable from the OpenSlot release assets or build this source.

Upstream GitHub workflows and Dependabot scheduling are archived in `.github/upstream-workflows/` and cannot run there. The replacement workflow only checks formatting and builds on a self-hosted runner. GitHub Actions requires an attached self-hosted runner. Local checks and native executable validation are recorded in [the validation report](docs/validation.md).

OpenSlot also adds persistent first-run configuration, a checksum-verifying installer, Linux service management, and executable health/version commands. Dependency notices are included in each release and embedded in `openslot licenses`.
