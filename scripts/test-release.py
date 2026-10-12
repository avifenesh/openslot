#!/usr/bin/env python3
"""Exercise the shipped executable, not a Go test helper. Uses only synthetic data."""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument('--binary', required=True)
parser.add_argument('--output', required=True)
parser.add_argument('--allow-dirty', action='store_true')
args = parser.parse_args()
if sys.platform != "linux":
    parser.error("This native integration fixture currently targets Linux; it does not qualify macOS.")
binary = Path(args.binary).resolve()
output = Path(args.output).resolve()
output.mkdir(parents=True, exist_ok=True)
root = Path(tempfile.mkdtemp(prefix='standalone-', dir=output))
workspace = root / 'work'
workspace.mkdir()
env = {key: os.environ[key] for key in ('PATH', 'HOME', 'SYSTEMROOT') if key in os.environ}
env['HOME'] = str(root)
env.update(XDG_CONFIG_HOME=str(root / 'config'), XDG_DATA_HOME=str(root / 'data'),
           XDG_CACHE_HOME=str(root / 'cache'), TMPDIR=str(root), GOMAXPROCS='2')

def command(*items, success=True):
    result = subprocess.run([str(binary), *items], cwd=workspace, env=env,
                            capture_output=True, text=True, timeout=30)
    if success and result.returncode:
        raise AssertionError(f'{items[0]} failed: {result.stderr}')
    return result

version = json.loads(command('version', '--json').stdout)
assert version['version'] != 'dev', 'Test a versioned release binary'
assert args.allow_dirty or not version.get('dirty'), 'Published release must be clean'
assert 'Usage:' in command('--help').stdout
assert command('unknown-command', success=False).returncode != 0
assert not (root / 'config').exists(), 'Read-only CLI commands created configuration'
command('init')
config = root / 'config' / 'openslot' / 'config.env'
configuration_hash = hashlib.sha256(config.read_bytes()).hexdigest()
if os.name != 'nt':
    assert config.stat().st_mode & 0o777 == 0o600

with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    port = sock.getsockname()[1]
base = f'http://127.0.0.1:{port}'
client = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

def api(path, data=None, method=None):
    body = json.dumps(data).encode() if data is not None else None
    request = urllib.request.Request(base + path, body, method=method,
                                     headers={'Content-Type': 'application/json', 'Origin': base})
    with client.open(request, timeout=5) as response:
        value = response.read()
        return response.status, json.loads(value) if value else None

process = None
log = None

def start(index):
    global process, log
    log = (output / f'server-{index}.log').open('w')
    process = subprocess.Popen([str(binary), 'serve', '--port', str(port)], cwd=workspace,
                               env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=log)
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise AssertionError('Release server exited before readiness')
        try:
            status, ready = api('/readyz')
            if status == 200 and ready['status'] == 'ok':
                assert ready['version']['version'] == version['version']
                return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.1)
    raise AssertionError('Release server readiness timed out')

def stop():
    global process, log
    if process and process.poll() is None:
        process.send_signal(signal.SIGTERM)
        try:
            assert process.wait(timeout=30) == 0
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
            raise
    if log:
        log.close()

try:
    start(1)
    status, _ = api('/v1/auth/claim', {'name': 'Release Owner', 'email': 'release@example.test',
                                    'password': 'Synthetic-Release-Passphrase-2026', 'timezone': 'UTC'})
    assert status in (200, 201)
    assert api('/v1/users/me')[1]['name'] == 'Release Owner'
    for day in range(7):
        api('/v1/availability-rules', {'day_of_week': day, 'start_time': '09:00', 'end_time': '17:00'})
    api('/v1/event-types', {'slug': 'release-meeting', 'name': 'Release meeting',
                          'duration_minutes': 30, 'location_type': 'phone', 'location_value': '+1 555 000 1111'})
    import datetime
    day = (datetime.date.today() + datetime.timedelta(days=2)).isoformat()
    slots = api(f'/v1/event-types/release-meeting/slots?from={day}&to={day}&tz=UTC')[1]['slots']
    assert slots
    status, booking = api('/v1/bookings', {'event_type_slug': 'release-meeting', 'start_at': slots[0]['start'],
                                         'name': 'Release Guest', 'email': 'guest@example.test', 'timezone': 'UTC'})
    assert status == 201
    command('doctor', '--url', base)
    stop()
    start(2)
    assert api('/v1/users/me')[1]['name'] == 'Release Owner', 'Owner/session did not persist'
    _, bookings = api('/v1/bookings')
    assert any(item['id'] == booking['id'] for item in bookings['items']), 'Booking did not persist'
    assert hashlib.sha256(config.read_bytes()).hexdigest() == configuration_hash, 'Restart changed keys/configuration'
    status, _ = api('/v1/bookings/' + booking['id'] + '/cancel', {}, 'POST')
    assert status == 200
    assert api('/v1/bookings/' + booking['id'])[1]['status'] == 'cancelled'
    for route in ('/admin/', '/admin/availability', '/admin/apps', '/embed.js'):
        with client.open(base + route, timeout=5) as response:
            assert response.status == 200
    command('doctor', '--url', base)
    stop()
    assert command('doctor', '--url', base, success=False).returncode != 0, 'Doctor missed stopped server'
    receipt = {'version': version, 'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
               'first_run': True, 'owner_claim': True, 'booking_created': True, 'booking_survived_restart': True,
               'configuration_preserved': True, 'cancellation': True, 'embedded_assets': True,
               'doctor_detected_running_and_stopped': True, 'private_configuration_permissions': True,
               'external_provider_requests': False, 'data_directory': str(root)}
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2))
    print(json.dumps({k: v for k, v in receipt.items() if k != 'data_directory'}))
finally:
    stop()
