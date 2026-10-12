#!/usr/bin/env python3
"""Collect installed dependency notices for the executable (build-time only).

The frontend inventory deliberately includes build dependencies: packages used
in client code are often declared as devDependencies. No network access occurs.
Vendored LiveKit notices are kept separately with the pinned vendored SDK.
"""
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parent.parent
DEST = ROOT / 'internal/licenseinfo/dependencies.txt'
NAMES = ('license', 'licence', 'notice', 'copying', 'copyright', 'ofl')


def notices(directory):
    result = []
    for root, dirs, files in os.walk(directory):
        dirs[:] = sorted(d for d in dirs if d not in ('.git', 'node_modules', 'testdata'))
        for name in sorted(files):
            lower = name.lower()
            if any(lower == n or lower.startswith(n + '.') or lower.startswith(n + '-') for n in NAMES):
                path = Path(root) / name
                if path.is_symlink():
                    continue
                result.append((str(path.relative_to(directory)), path.read_text(errors='replace').strip()))
    return result


def npm_notices(root):
    packages = []
    for modules in sorted(root.glob('.pnpm/*/node_modules')):
        for package in sorted(modules.iterdir()):
            entries = sorted(package.iterdir()) if package.name.startswith('@') else [package]
            for entry in entries:
                if entry.is_symlink() or not (entry / 'package.json').is_file():
                    continue
                info = json.loads((entry / 'package.json').read_text())
                label = f"npm {info['name']} {info['version']}"
                files = notices(entry)
                if files:
                    packages.append((label, files))
                else:
                    supplement = ROOT / 'internal/licenseinfo/supplements' / (info['name'].replace('/', '_') + '-' + info['version'] + '.txt')
                    if not supplement.is_file():
                        raise RuntimeError(f'Missing license text for {label}; collect notices from its pinned source into {supplement}')
                    packages.append((label, [('upstream notices omitted from npm package', supplement.read_text().strip())]))
    return packages


def render(packages):
    blocks = []
    for label, files in sorted(packages):
        blocks.append('\n' + '=' * 72 + '\n' + label + '\n' + '=' * 72)
        for name, content in files:
            blocks.append('\n--- ' + name + ' ---\n' + content)
    return '\n'.join(line.expandtabs(4).rstrip() for line in '\n'.join(blocks).splitlines()).rstrip() + '\n'


def main():
    raw = subprocess.check_output(['go','list','-m','-json','all'], cwd=ROOT, text=True)
    decoder = json.JSONDecoder()
    modules = []
    while raw.strip():
        mod, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        if mod.get('Main'):
            continue
        location = mod.get('Replace', mod).get('Dir')
        if not location:
            raise RuntimeError(f"Download module first: {mod['Path']}")
        files = notices(Path(location))
        if not files:
            raise RuntimeError(f"Missing module license: {mod['Path']}")
        modules.append((f"Go module {mod['Path']} {mod.get('Version','')}", files))
    goroot = Path(subprocess.check_output(['go','env','GOROOT'],text=True).strip())
    modules.append(('Go standard library', [('LICENSE', (goroot/'LICENSE').read_text().strip())]))
    packages = npm_notices(ROOT/'frontend/node_modules')
    if not packages:
        raise RuntimeError('Run pnpm install in frontend first')
    (DEST.parent / 'project.txt').write_text((ROOT/'LICENSE').read_text() + '\n' + (ROOT/'NOTICE').read_text())
    DEST.write_text('Third-party notices for OpenSlot. The inventory includes frontend build dependencies.\n' + render(modules + packages))
    print(f'Collected {len(modules)} Go and {len(packages)} npm entries in {DEST.relative_to(ROOT)}')

if __name__ == '__main__':
    main()
