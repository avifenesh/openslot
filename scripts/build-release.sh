#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
version=${1:?usage: scripts/build-release.sh v0.1.0 [output-directory]}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]] || { echo 'Invalid release version' >&2; exit 1; }
output=${2:-dist}
mkdir -p "$output"
output=$(cd "$output" && pwd)
python3 scripts/collect-licenses.py
commit=$(git rev-parse HEAD)
if [[ -n $(git status --porcelain) && ${OPENSLOT_ALLOW_DIRTY_BUILD:-0} != 1 ]]; then
 echo 'Release builds require a clean checkout. Set OPENSLOT_ALLOW_DIRTY_BUILD=1 only for local validation.' >&2
 exit 1
fi
cache_root=${XDG_CACHE_HOME:-$HOME/.cache}/openslot
mkdir -p "$cache_root"
staging=$(mktemp -d "$cache_root/release.XXXXXXXX")
trap 'rm -rf "$staging"' EXIT
# Frontend assets are embedded; this build never downloads runtime dependencies.
(cd frontend && pnpm build)
platforms=${OPENSLOT_RELEASE_PLATFORMS:-'linux/amd64 linux/arm64 darwin/amd64 darwin/arm64'}
assets=()
for target in $platforms; do
 platform=${target%/*};arch=${target#*/}
 case "$target" in linux/amd64|linux/arm64|darwin/amd64|darwin/arm64) ;; *) echo "Unsupported release target: $target" >&2; exit 1 ;; esac
 stage=$staging/${platform}_${arch};mkdir -p "$stage"
 CGO_ENABLED=0 GOOS=$platform GOARCH=$arch go build -p "${OPENSLOT_BUILD_PARALLELISM:-2}" -trimpath -ldflags "-s -w -X github.com/calnode/calnode/internal/buildinfo.Version=$version -X github.com/calnode/calnode/internal/buildinfo.Commit=$commit" -o "$stage/openslot" ./cmd/calnode
 cp LICENSE NOTICE TRADEMARK.md FORK.md README.md .env.example scripts/install.sh "$stage/"
 cat internal/licenseinfo/dependencies.txt internal/licenseinfo/vendored.txt > "$stage/THIRD_PARTY_NOTICES.txt"
 mkdir -p "$stage/docs";cp docs/*.md "$stage/docs/"
 asset=openslot_${version}_${platform}_${arch}.tar.gz
 tar -czf "$output/$asset" -C "$stage" openslot LICENSE NOTICE THIRD_PARTY_NOTICES.txt TRADEMARK.md FORK.md README.md .env.example install.sh docs
 assets+=("$asset")
 printf 'Built %s\n' "$asset"
done
cp scripts/install.sh "$output/install.sh"
(cd "$output" && sha256sum "${assets[@]}" install.sh > SHA256SUMS)
printf 'Release artifacts: %s\nCommit: %s\n' "$output" "$commit"
