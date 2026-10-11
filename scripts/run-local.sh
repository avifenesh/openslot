#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
if [[ ! -x ./openslot ]]; then
  printf 'Build OpenSlot first: make build\n' >&2
  exit 1
fi

# The application reads .env itself. Never source credentials as shell code.
# Exclusive creation preserves an existing installation and its encryption keys.
if [[ ! -e .env ]]; then
  python3 - <<'PY'
import os
import secrets

fd = os.open('.env', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as config:
    config.write('PORT=8788\nBIND_HOST=127.0.0.1\nBASE_URL=http://127.0.0.1:8788\n')
    config.write('DATABASE_URL=sqlite://./data/openslot.db\nDATA_DIR=./data\n')
    config.write('EMAIL_FROM_NAME=OpenSlot\n')
    config.write('CALNODE_ENCRYPTION_KEY=' + secrets.token_hex(32) + '\n')
    config.write('CALNODE_RECOVERY_SECRET=' + secrets.token_hex(32) + '\n')
PY
  printf 'Created local configuration. Keep .env and data/ together when backing up.\n'
fi

mkdir -p data
exec ./openslot
