# Development

Requires Go 1.26+, Node.js 22.12+, and pnpm 10.32.1. The legacy local configuration helper also uses Python 3. These are build dependencies, not requirements for the released executable.

```sh
make build
make run
```

The frontend is embedded into the Go executable. Rebuild both layers after UI changes.

```sh
cd frontend
pnpm install --frozen-lockfile
pnpm test:browsers
cd ..
make check
```

Browser tests use project-local Chromium in `frontend/.playwright` and synthetic data. They do not connect your real provider accounts. Set `OPENSLOT_TEST_BINARY` to test a specific installed/release executable and `OPENSLOT_TEST_ARTIFACTS` to select an evidence directory.

The full check covers Go tests, public booking JavaScript, frontend types, real-browser component styles, and end-to-end workflows. Installed Codex/Claude smoke tests are opt-in; see the inbox guide.

## Build release archives

Start from a clean, reviewed commit:

```sh
scripts/build-release.sh v0.1.0
```

This builds Linux and macOS archives for amd64 and arm64, with embedded assets, version/commit metadata, license notices, docs and the installer. `dist/SHA256SUMS` lists their hashes. Only Linux amd64 can be executed by the current local release-validation environment; the other targets must not be described as runtime-tested without separate evidence.

To build one target during development:

```sh
OPENSLOT_RELEASE_PLATFORMS=linux/amd64 scripts/build-release.sh v0.1.0
```

`OPENSLOT_ALLOW_DIRTY_BUILD=1` is for local validation only. Published assets must come from the merged clean commit. Do not include `.env`, SQLite files, uploaded data, or validation screenshots in release archives.

Release packaging also needs Python 3 to collect dependency notices. Run `go mod download all` at the repository root and `pnpm install --frozen-lockfile` in `frontend/` before collecting notices with `python3 scripts/collect-licenses.py`. Commit updated notice files before a release build. End users do not need Python.
