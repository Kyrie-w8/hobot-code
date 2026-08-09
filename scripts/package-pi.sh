#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=${1:-$(cat "$root_dir/VERSION")}
. "$root_dir/pi-runtime/pi.lock"
. "$root_dir/pi-runtime/tools.lock"

cache_dir=${HOBOT_CODE_PI_CACHE_DIR:-$root_dir/dist/pi-cache}
archive="$cache_dir/pi-linux-arm64-$PI_VERSION.tar.gz"
fd_archive="$cache_dir/fd-linux-arm64-$FD_VERSION.tar.gz"
rg_archive="$cache_dir/ripgrep-linux-arm64-$RIPGREP_VERSION.tar.gz"
stage_dir="$root_dir/dist/hobot-code-$version-linux-arm64"
output="$root_dir/dist/hobot-code-$version-linux-arm64.tar.gz"
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/hobot-code-pi-package.XXXXXX")
trap 'rm -rf "$temp_dir"' EXIT HUP INT TERM
tool_bundle_dir=${HOBOT_CODE_TOOL_BUNDLE_DIR:-}

mkdir -p "$cache_dir" "$root_dir/dist"

verify_file() {
  destination=$1
  expected=$2
  label=$3
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

download_and_verify() {
  url=$1
  destination=$2
  expected=$3
  label=$4
  if [ ! -f "$destination" ]; then
    curl -fL --retry 3 --retry-delay 2 "$url" -o "$destination"
  fi
  verify_file "$destination" "$expected" "$label"
}

download_and_verify "$PI_LINUX_ARM64_URL" "$archive" "$PI_LINUX_ARM64_SHA256" "Pi archive"
if [ -z "$tool_bundle_dir" ]; then
  download_and_verify "$FD_LINUX_ARM64_URL" "$fd_archive" "$FD_LINUX_ARM64_SHA256" "fd archive"
  download_and_verify "$RIPGREP_LINUX_ARM64_URL" "$rg_archive" "$RIPGREP_LINUX_ARM64_SHA256" "ripgrep archive"
fi

tar -xzf "$archive" -C "$temp_dir"
rm -rf "$stage_dir"
mkdir -p "$stage_dir/runtime" "$stage_dir/extensions" "$stage_dir/skills" "$stage_dir/knowledge" "$stage_dir/prompts" "$stage_dir/config" "$stage_dir/licenses" "$stage_dir/managed-bin"
cp -R "$temp_dir/pi/." "$stage_dir/runtime/"
mv "$stage_dir/runtime/pi" "$stage_dir/runtime/hobot"
node -e 'const fs=require("fs"); const value=JSON.parse(fs.readFileSync(process.argv[1], "utf8")); value.version=process.argv[3]; fs.writeFileSync(process.argv[2], `${JSON.stringify(value, null, 2)}\n`);' \
  "$root_dir/pi-runtime/package.json" "$stage_dir/runtime/package.json" "$version"
install -m 0644 "$root_dir/CHANGELOG.md" "$stage_dir/runtime/CHANGELOG.md"
install -m 0644 "$root_dir/pi-runtime/pi.lock" "$stage_dir/PI_RUNTIME"
install -m 0644 "$root_dir/pi-runtime/tools.lock" "$stage_dir/TOOLS_RUNTIME"
cp -R "$root_dir/extensions/." "$stage_dir/extensions/"
cp -R "$root_dir/skills/." "$stage_dir/skills/"
cp -R "$root_dir/knowledge/." "$stage_dir/knowledge/"
install -m 0644 "$root_dir/prompts/rdk-expert.md" "$stage_dir/prompts/rdk-expert.md"
install -m 0644 "$root_dir/packaging/pi/settings.json" "$stage_dir/config/settings.json"
install -m 0644 "$root_dir/packaging/pi/models.json" "$stage_dir/config/models.json"
install -m 0644 "$root_dir/packaging/pi/permissions.json" "$stage_dir/config/permissions.json"
install -m 0644 "$root_dir/packaging/pi/memory.json" "$stage_dir/config/memory.json"
install -m 0644 "$root_dir/packaging/pi/goals.json" "$stage_dir/config/goals.json"
install -m 0644 "$root_dir/packaging/pi/hooks.json" "$stage_dir/config/hooks.json"
install -m 0644 "$root_dir/packaging/pi/notifications.json" "$stage_dir/config/notifications.json"
install -m 0644 "$root_dir/packaging/pi/lsp.json" "$stage_dir/config/lsp.json"
install -m 0600 "$root_dir/packaging/pi/hobot.env.example" "$stage_dir/config/hobot.env.example"
install -m 0755 "$root_dir/packaging/pi/hobot-launcher" "$stage_dir/hobot-launcher"
install -m 0755 "$root_dir/scripts/install-pi.sh" "$stage_dir/install.sh"
install -m 0755 "$root_dir/scripts/rollback-pi.sh" "$stage_dir/rollback.sh"
install -m 0644 "$root_dir/LICENSES/pi-mono-MIT.txt" "$stage_dir/licenses/pi-mono-MIT.txt"
install -m 0644 "$root_dir/LICENSE" "$stage_dir/licenses/hobot-code-MIT.txt"

if [ -n "$tool_bundle_dir" ]; then
  verify_file "$tool_bundle_dir/fd" "$FD_LINUX_ARM64_BINARY_SHA256" "fd binary"
  verify_file "$tool_bundle_dir/rg" "$RIPGREP_LINUX_ARM64_BINARY_SHA256" "ripgrep binary"
  verify_file "$tool_bundle_dir/fd-MIT.txt" "$FD_MIT_SHA256" "fd MIT license"
  verify_file "$tool_bundle_dir/fd-APACHE-2.0.txt" "$FD_APACHE_SHA256" "fd Apache license"
  verify_file "$tool_bundle_dir/ripgrep-MIT.txt" "$RIPGREP_MIT_SHA256" "ripgrep MIT license"
  verify_file "$tool_bundle_dir/ripgrep-UNLICENSE.txt" "$RIPGREP_UNLICENSE_SHA256" "ripgrep unlicense"
  install -m 0755 "$tool_bundle_dir/fd" "$stage_dir/managed-bin/fd"
  install -m 0755 "$tool_bundle_dir/rg" "$stage_dir/managed-bin/rg"
  install -m 0644 "$tool_bundle_dir/fd-MIT.txt" "$stage_dir/licenses/fd-MIT.txt"
  install -m 0644 "$tool_bundle_dir/fd-APACHE-2.0.txt" "$stage_dir/licenses/fd-APACHE-2.0.txt"
  install -m 0644 "$tool_bundle_dir/ripgrep-MIT.txt" "$stage_dir/licenses/ripgrep-MIT.txt"
  install -m 0644 "$tool_bundle_dir/ripgrep-UNLICENSE.txt" "$stage_dir/licenses/ripgrep-UNLICENSE.txt"
else
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
fi

printf '%s\n' "$version" > "$stage_dir/VERSION"
COPYFILE_DISABLE=1 tar --no-xattrs -C "$root_dir/dist" -czf "$output" "hobot-code-$version-linux-arm64"
printf '%s\n' "$output"
