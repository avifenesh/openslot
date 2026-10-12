#!/bin/sh
# Install a checksummed OpenSlot release without Go, Node.js or root privileges.
set -eu
repo=avifenesh/openslot
version=latest
install_dir=${HOME}/.local/bin
archive=
checksums=
work_dir=
usage() {
 cat <<'USAGE'
Install OpenSlot:
  sh install.sh [--version v0.1.0] [--dir ~/.local/bin]
  sh install.sh --version v0.1.0 --archive RELEASE.tar.gz --checksums SHA256SUMS

Downloads the matching release, verifies SHA-256, and installs openslot.
Configuration, databases and existing services are not changed.
USAGE
}
die() { printf 'openslot installer: %s\n' "$*" >&2; exit 1; }
while [ "$#" -gt 0 ]; do
 case "$1" in
  --help|-h) usage; exit 0 ;;
  --version|--dir|--archive|--checksums)
   [ "$#" -ge 2 ] || die "$1 requires a value"
   case "$1" in --version) version=$2 ;; --dir) install_dir=$2 ;; --archive) archive=$2 ;; --checksums) checksums=$2 ;; esac
   shift 2 ;;
  *) die "unknown option: $1" ;;
 esac
done
case "$(uname -s)" in Linux) platform=linux ;; Darwin) platform=darwin ;; *) die 'Use a Linux or macOS release, or build from source on this platform.' ;; esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) die 'No prebuilt release for this architecture.' ;; esac
for utility in tar awk mkdir mktemp chmod mv; do command -v "$utility" >/dev/null 2>&1 || die "$utility is required"; done
if command -v sha256sum >/dev/null 2>&1; then checksum_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then checksum_tool=shasum
else die 'sha256sum or shasum is required to verify the download'; fi
cache_root=${XDG_CACHE_HOME:-${HOME}/.cache}/openslot
mkdir -p "$cache_root"
work_dir=$(mktemp -d "$cache_root/install.XXXXXXXX")
cleanup() { [ -z "$work_dir" ] || rm -rf -- "$work_dir"; }
trap cleanup 0
trap 'exit 1' HUP INT TERM
if [ -z "$archive" ]; then
 command -v curl >/dev/null 2>&1 || die 'curl is required to download a release'
 if [ "$version" = latest ]; then
  release_url=$(curl -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 120 -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") || die 'Could not find the latest public release. Download assets from GitHub and use --archive/--checksums for a private repository.'
  version=${release_url##*/}
 fi
else
 [ -f "$archive" ] || die 'archive does not exist'
 [ "$version" != latest ] || die '--version is required with a local archive'
 [ -n "$checksums" ] && [ -f "$checksums" ] || die '--checksums must name the matching SHA256SUMS file'
fi
case "$version" in v*) ;; *) version=v$version ;; esac
printf '%s\n' "$version" | awk '/^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$/ {ok=1} END {exit !ok}' || die 'version must be a release tag such as v0.1.0'
asset=openslot_${version}_${platform}_${arch}.tar.gz
if [ -z "$archive" ]; then
 archive=$work_dir/$asset
 checksums=$work_dir/SHA256SUMS
 base=https://github.com/$repo/releases/download/$version
 curl -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 120 "$base/$asset" -o "$archive" || die 'Release download failed; nothing was installed.'
 curl -fsSL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 120 "$base/SHA256SUMS" -o "$checksums" || die 'Checksum download failed; nothing was installed.'
fi
expected=$(awk -v name="$asset" '$2 == name {print $1}' "$checksums")
[ "${#expected}" -eq 64 ] || die 'Release checksum is missing or duplicated.'
case "$expected" in *[!0-9a-fA-F]*) die 'Invalid release checksum.' ;; esac
if [ "$checksum_tool" = sha256sum ]; then actual=$(sha256sum "$archive" | awk '{print $1}'); else actual=$(shasum -a 256 "$archive" | awk '{print $1}'); fi
[ "$actual" = "$expected" ] || die 'Checksum mismatch; nothing was installed.'
# Stream only the named executable. No archive pathname is extracted to disk.
tar -xOf "$archive" openslot > "$work_dir/openslot" || die 'Archive does not contain openslot.'
chmod 755 "$work_dir/openslot"
reported=$("$work_dir/openslot" version) || die 'The downloaded executable cannot run on this machine.'
case "$reported" in "openslot $version ("*) ;; *) die 'Executable version does not match the release.' ;; esac
mkdir -p "$install_dir"
staged=$(mktemp "$install_dir/.openslot.XXXXXXXX")
if ! cat "$work_dir/openslot" > "$staged" || ! chmod 755 "$staged" || ! mv -f "$staged" "$install_dir/openslot"; then
 rm -f "$staged"
 die 'Could not install the executable.'
fi
printf 'Installed %s\nStart: "%s"\n' "$reported" "$install_dir/openslot"
printf 'If a service is already running, restart it with: "%s" service restart\n' "$install_dir/openslot"
case ":$PATH:" in *":$install_dir:"*) ;; *) printf 'Add %s to PATH, or use the full executable path above.\n' "$install_dir" ;; esac
