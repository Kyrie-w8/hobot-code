#!/bin/sh
set -eu

version=${1:-0.5.0}
root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
. "$root_dir/pi-runtime/pi.lock"
. "$root_dir/pi-runtime/tools.lock"

cache_dir=${ASTER_PI_CACHE_DIR:-$root_dir/dist/pi-cache}
archive="$cache_dir/pi-linux-arm64-$PI_VERSION.tar.gz"
fd_archive="$cache_dir/fd-linux-arm64-$FD_VERSION.tar.gz"
rg_archive="$cache_dir/ripgrep-linux-arm64-$RIPGREP_VERSION.tar.gz"
stage_dir="$root_dir/dist/aster-$version-linux-arm64"
output="$root_dir/dist/aster-$version-linux-arm64.tar.gz"
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/aster-pi-package.XXXXXX")
trap 'rm -rf "$temp_dir"' EXIT HUP INT TERM

mkdir -p "$cache_dir" "$root_dir/dist"

download_and_verify() {
  url=$1
  destination=$2
  expected=$3
  label=$4
  if [ ! -f "$destination" ]; then
    curl -fL --retry 3 --retry-delay 2 "$url" -o "$destination"
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$destination" | awk '{print $1}')
  else
    actual=$(shasum -a 256 "$destination" | awk '{print $1}')
  fi
  if [ "$actual" != "$expected" ]; then
    printf '%s checksum mismatch: expected %s, got %s\n' "$label" "$expected" "$actual" >&2
    exit 1
  fi
}

download_and_verify "$PI_LINUX_ARM64_URL" "$archive" "$PI_LINUX_ARM64_SHA256" "Pi archive"
download_and_verify "$FD_LINUX_ARM64_URL" "$fd_archive" "$FD_LINUX_ARM64_SHA256" "fd archive"
download_and_verify "$RIPGREP_LINUX_ARM64_URL" "$rg_archive" "$RIPGREP_LINUX_ARM64_SHA256" "ripgrep archive"

tar -xzf "$archive" -C "$temp_dir"
rm -rf "$stage_dir"
mkdir -p "$stage_dir/runtime" "$stage_dir/extensions" "$stage_dir/skills" "$stage_dir/config" "$stage_dir/licenses" "$stage_dir/managed-bin"
cp -R "$temp_dir/pi/." "$stage_dir/runtime/"
mv "$stage_dir/runtime/pi" "$stage_dir/runtime/aster"
install -m 0644 "$root_dir/pi-runtime/package.json" "$stage_dir/runtime/package.json"
install -m 0644 "$root_dir/CHANGELOG.md" "$stage_dir/runtime/CHANGELOG.md"
install -m 0644 "$root_dir/pi-runtime/pi.lock" "$stage_dir/PI_RUNTIME"
install -m 0644 "$root_dir/pi-runtime/tools.lock" "$stage_dir/TOOLS_RUNTIME"
install -m 0644 "$root_dir/extensions/rdk.ts" "$stage_dir/extensions/rdk.ts"
cp -R "$root_dir/skills/." "$stage_dir/skills/"
install -m 0644 "$root_dir/packaging/pi/settings.json" "$stage_dir/config/settings.json"
install -m 0644 "$root_dir/packaging/pi/models.json" "$stage_dir/config/models.json"
install -m 0600 "$root_dir/packaging/pi/aster.env.example" "$stage_dir/config/aster.env.example"
install -m 0755 "$root_dir/packaging/pi/aster-launcher" "$stage_dir/aster-launcher"
install -m 0755 "$root_dir/scripts/install-pi.sh" "$stage_dir/install.sh"
install -m 0755 "$root_dir/scripts/rollback-pi.sh" "$stage_dir/rollback.sh"
install -m 0644 "$root_dir/LICENSES/pi-mono-MIT.txt" "$stage_dir/licenses/pi-mono-MIT.txt"
install -m 0644 "$root_dir/LICENSE" "$stage_dir/licenses/aster-MIT.txt"

tar -xzf "$fd_archive" -C "$temp_dir"
tar -xzf "$rg_archive" -C "$temp_dir"
fd_root="$temp_dir/fd-v$FD_VERSION-aarch64-unknown-linux-gnu"
rg_root="$temp_dir/ripgrep-$RIPGREP_VERSION-aarch64-unknown-linux-gnu"
install -m 0755 "$fd_root/fd" "$stage_dir/managed-bin/fd"
install -m 0755 "$rg_root/rg" "$stage_dir/managed-bin/rg"
install -m 0644 "$fd_root/LICENSE-MIT" "$stage_dir/licenses/fd-MIT.txt"
install -m 0644 "$fd_root/LICENSE-APACHE" "$stage_dir/licenses/fd-APACHE-2.0.txt"
install -m 0644 "$rg_root/LICENSE-MIT" "$stage_dir/licenses/ripgrep-MIT.txt"
install -m 0644 "$rg_root/UNLICENSE" "$stage_dir/licenses/ripgrep-UNLICENSE.txt"

printf '%s\n' "$version" > "$stage_dir/VERSION"
COPYFILE_DISABLE=1 tar --no-xattrs -C "$root_dir/dist" -czf "$output" "aster-$version-linux-arm64"
printf '%s\n' "$output"
