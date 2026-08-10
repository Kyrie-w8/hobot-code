#!/bin/sh
set -eu
umask 022

if [ "$#" -ne 0 ]; then
  printf 'package-pi.sh reads the release version from VERSION; command-line versions are not accepted\n' >&2
  exit 2
fi

root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
node "$root_dir/scripts/validate-version.mjs"
version=$(sed -n '1p' "$root_dir/VERSION")

lock_value() {
  lock_path=$1
  lock_key=$2
  awk -F= -v key="$lock_key" '$1 == key { print substr($0, length(key) + 2) }' "$lock_path"
}

pi_lock="$root_dir/pi-runtime/pi.lock"
tools_lock="$root_dir/pi-runtime/tools.lock"
PI_VERSION=$(lock_value "$pi_lock" PI_VERSION)
PI_LINUX_ARM64_SHA256=$(lock_value "$pi_lock" PI_LINUX_ARM64_SHA256)
PI_LINUX_ARM64_URL=$(lock_value "$pi_lock" PI_LINUX_ARM64_URL)
FD_VERSION=$(lock_value "$tools_lock" FD_VERSION)
FD_LINUX_ARM64_SHA256=$(lock_value "$tools_lock" FD_LINUX_ARM64_SHA256)
FD_LINUX_ARM64_BINARY_SHA256=$(lock_value "$tools_lock" FD_LINUX_ARM64_BINARY_SHA256)
FD_MIT_SHA256=$(lock_value "$tools_lock" FD_MIT_SHA256)
FD_APACHE_SHA256=$(lock_value "$tools_lock" FD_APACHE_SHA256)
FD_LINUX_ARM64_URL=$(lock_value "$tools_lock" FD_LINUX_ARM64_URL)
RIPGREP_VERSION=$(lock_value "$tools_lock" RIPGREP_VERSION)
RIPGREP_LINUX_ARM64_SHA256=$(lock_value "$tools_lock" RIPGREP_LINUX_ARM64_SHA256)
RIPGREP_LINUX_ARM64_BINARY_SHA256=$(lock_value "$tools_lock" RIPGREP_LINUX_ARM64_BINARY_SHA256)
RIPGREP_MIT_SHA256=$(lock_value "$tools_lock" RIPGREP_MIT_SHA256)
RIPGREP_UNLICENSE_SHA256=$(lock_value "$tools_lock" RIPGREP_UNLICENSE_SHA256)
RIPGREP_LINUX_ARM64_URL=$(lock_value "$tools_lock" RIPGREP_LINUX_ARM64_URL)

if ! git -C "$root_dir" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  printf 'Release builds require a Git worktree\n' >&2
  exit 1
fi
commit=$(git -C "$root_dir" rev-parse --verify HEAD)
dirty=0
if ! git -C "$root_dir" diff --quiet --ignore-submodules -- ||
   ! git -C "$root_dir" diff --cached --quiet --ignore-submodules -- ||
   [ -n "$(git -C "$root_dir" ls-files --others --exclude-standard)" ]; then
  dirty=1
fi
if [ "$dirty" -eq 1 ] && [ "${HOBOT_CODE_ALLOW_DIRTY_BUILD:-0}" != 1 ]; then
  printf 'Refusing a production release from a dirty worktree. Commit the release or set HOBOT_CODE_ALLOW_DIRTY_BUILD=1 for a local development package.\n' >&2
  exit 1
fi

if [ -n "${SOURCE_DATE_EPOCH:-}" ]; then
  case "$SOURCE_DATE_EPOCH" in
    ''|*[!0-9]*)
      printf 'SOURCE_DATE_EPOCH must be a non-negative integer\n' >&2
      exit 1
      ;;
  esac
  built_at=$(node -e 'process.stdout.write(new Date(Number(process.argv[1]) * 1000).toISOString())' "$SOURCE_DATE_EPOCH")
else
  built_at=$(git -C "$root_dir" show -s --format=%cI "$commit")
fi

cache_dir=${HOBOT_CODE_PI_CACHE_DIR:-$root_dir/dist/pi-cache}
archive="$cache_dir/pi-linux-arm64-$PI_VERSION.tar.gz"
fd_archive="$cache_dir/fd-linux-arm64-$FD_VERSION.tar.gz"
rg_archive="$cache_dir/ripgrep-linux-arm64-$RIPGREP_VERSION.tar.gz"
stage_dir="$root_dir/dist/hobot-code-$version-linux-arm64"
output="$root_dir/dist/hobot-code-$version-linux-arm64.tar.gz"
output_part="$output.part.$$"
checksum_output="$output.sha256"
checksum_part="$checksum_output.part.$$"
temp_dir=$(mktemp -d "${TMPDIR:-/tmp}/hobot-code-pi-package.XXXXXX")
tool_bundle_dir=${HOBOT_CODE_TOOL_BUNDLE_DIR:-}
package_lock_dir="$root_dir/dist/.package-pi.lock"
package_lock_acquired=0

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  set +e
  rm -rf "$temp_dir"
  rm -f "$archive.part.$$" "$fd_archive.part.$$" "$rg_archive.part.$$"
  rm -f "$output_part" "$checksum_part"
  if [ "$package_lock_acquired" -eq 1 ]; then
    rm -f "$package_lock_dir/pid"
    rmdir "$package_lock_dir" 2>/dev/null || true
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

mkdir -p "$cache_dir" "$root_dir/dist"

acquire_package_lock() {
  if mkdir "$package_lock_dir" 2>/dev/null; then
    package_lock_acquired=1
    printf '%s\n' "$$" > "$package_lock_dir/pid"
    return
  fi
  package_lock_pid=$(sed -n '1p' "$package_lock_dir/pid" 2>/dev/null || true)
  case "$package_lock_pid" in
    ''|*[!0-9]*) ;;
    *)
      if ! kill -0 "$package_lock_pid" 2>/dev/null; then
        rm -rf "$package_lock_dir"
        if mkdir "$package_lock_dir" 2>/dev/null; then
          package_lock_acquired=1
          printf '%s\n' "$$" > "$package_lock_dir/pid"
          return
        fi
      fi
      ;;
  esac
  printf 'Another Hobot Code package build is already running: %s\n' "$package_lock_dir" >&2
  exit 1
}
acquire_package_lock

checksum_file() {
  checksum_path=$1
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$checksum_path" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$checksum_path" | awk '{print $1}'
  else
    printf 'A SHA256 utility (sha256sum or shasum) is required\n' >&2
    return 127
  fi
}

verify_file() {
  destination=$1
  expected=$2
  label=$3
  actual=$(checksum_file "$destination") || return
  if [ "$actual" != "$expected" ]; then
    printf '%s checksum mismatch: expected %s, got %s\n' "$label" "$expected" "$actual" >&2
    return 1
  fi
}

download_and_verify() {
  url=$1
  destination=$2
  expected=$3
  label=$4
  if [ -f "$destination" ] && verify_file "$destination" "$expected" "$label"; then
    return
  fi
  if [ -e "$destination" ]; then
    printf 'Replacing invalid cached file: %s\n' "$destination" >&2
  fi
  partial="$destination.part.$$"
  rm -f "$partial"
  curl --proto '=https' --tlsv1.2 -fL --connect-timeout 20 --retry 3 --retry-delay 2 --retry-all-errors "$url" -o "$partial"
  if ! verify_file "$partial" "$expected" "$label"; then
    rm -f "$partial"
    return 1
  fi
  mv -f "$partial" "$destination"
}

download_and_verify "$PI_LINUX_ARM64_URL" "$archive" "$PI_LINUX_ARM64_SHA256" "Pi archive"
if [ -z "$tool_bundle_dir" ]; then
  download_and_verify "$FD_LINUX_ARM64_URL" "$fd_archive" "$FD_LINUX_ARM64_SHA256" "fd archive"
  download_and_verify "$RIPGREP_LINUX_ARM64_URL" "$rg_archive" "$RIPGREP_LINUX_ARM64_SHA256" "ripgrep archive"
fi

"$root_dir/scripts/validate-tar-archive.sh" "$archive" pi "Pi archive"
if [ -z "$tool_bundle_dir" ]; then
  "$root_dir/scripts/validate-tar-archive.sh" "$fd_archive" "fd-v$FD_VERSION-aarch64-unknown-linux-gnu" "fd archive"
  "$root_dir/scripts/validate-tar-archive.sh" "$rg_archive" "ripgrep-$RIPGREP_VERSION-aarch64-unknown-linux-gnu" "ripgrep archive"
fi

tar -xzf "$archive" -C "$temp_dir"
if [ ! -f "$temp_dir/pi/pi" ]; then
  printf 'Pi archive does not contain the expected pi/pi executable\n' >&2
  exit 1
fi

rm -rf "$stage_dir"
mkdir -p "$stage_dir/runtime" "$stage_dir/config" "$stage_dir/licenses" "$stage_dir/managed-bin"
cp -R "$temp_dir/pi/." "$stage_dir/runtime/"
mv "$stage_dir/runtime/pi" "$stage_dir/runtime/hobot"
node -e 'const fs=require("fs"); const value=JSON.parse(fs.readFileSync(process.argv[1], "utf8")); value.version=process.argv[3]; fs.writeFileSync(process.argv[2], `${JSON.stringify(value, null, 2)}\n`);' \
  "$root_dir/pi-runtime/package.json" "$stage_dir/runtime/package.json" "$version"

if [ "$dirty" -eq 0 ]; then
  git -C "$root_dir" archive --format=tar -o "$temp_dir/source.tar" "$commit" -- README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md LICENSE docs extensions skills knowledge prompts
  tar -xf "$temp_dir/source.tar" -C "$stage_dir"
else
  install -m 0644 "$root_dir/README.md" "$stage_dir/README.md"
  install -m 0644 "$root_dir/CHANGELOG.md" "$stage_dir/CHANGELOG.md"
  install -m 0644 "$root_dir/CONTRIBUTING.md" "$stage_dir/CONTRIBUTING.md"
  install -m 0644 "$root_dir/SECURITY.md" "$stage_dir/SECURITY.md"
  install -m 0644 "$root_dir/LICENSE" "$stage_dir/LICENSE"
  for source_name in docs extensions skills knowledge prompts; do
    mkdir -p "$stage_dir/$source_name"
    cp -R "$root_dir/$source_name/." "$stage_dir/$source_name/"
  done
fi

# Pi resolves startup notices from the runtime directory. Keep that file in the
# Hobot Code version space so upstream Pi releases are never treated as unread.
install -m 0644 "$stage_dir/CHANGELOG.md" "$stage_dir/runtime/CHANGELOG.md"

install -m 0644 "$root_dir/pi-runtime/pi.lock" "$stage_dir/PI_RUNTIME"
install -m 0644 "$root_dir/pi-runtime/tools.lock" "$stage_dir/TOOLS_RUNTIME"
for config_name in settings.json models.json permissions.json memory.json goals.json hooks.json notifications.json lsp.json; do
  install -m 0644 "$root_dir/packaging/pi/$config_name" "$stage_dir/config/$config_name"
done
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
  verify_file "$fd_root/fd" "$FD_LINUX_ARM64_BINARY_SHA256" "fd binary"
  verify_file "$rg_root/rg" "$RIPGREP_LINUX_ARM64_BINARY_SHA256" "ripgrep binary"
  verify_file "$fd_root/LICENSE-MIT" "$FD_MIT_SHA256" "fd MIT license"
  verify_file "$fd_root/LICENSE-APACHE" "$FD_APACHE_SHA256" "fd Apache license"
  verify_file "$rg_root/LICENSE-MIT" "$RIPGREP_MIT_SHA256" "ripgrep MIT license"
  verify_file "$rg_root/UNLICENSE" "$RIPGREP_UNLICENSE_SHA256" "ripgrep unlicense"
  install -m 0755 "$fd_root/fd" "$stage_dir/managed-bin/fd"
  install -m 0755 "$rg_root/rg" "$stage_dir/managed-bin/rg"
  install -m 0644 "$fd_root/LICENSE-MIT" "$stage_dir/licenses/fd-MIT.txt"
  install -m 0644 "$fd_root/LICENSE-APACHE" "$stage_dir/licenses/fd-APACHE-2.0.txt"
  install -m 0644 "$rg_root/LICENSE-MIT" "$stage_dir/licenses/ripgrep-MIT.txt"
  install -m 0644 "$rg_root/UNLICENSE" "$stage_dir/licenses/ripgrep-UNLICENSE.txt"
fi

printf '%s\n' "$version" > "$stage_dir/VERSION"
node "$root_dir/scripts/release-metadata.mjs" write "$root_dir" "$stage_dir" "$commit" "$dirty" "$built_at"
node "$root_dir/scripts/write-release-manifest.mjs" "$stage_dir" "$built_at"
node "$root_dir/scripts/validate-package.mjs" --package "$stage_dir"

archive_name=$(basename "$stage_dir")
(cd "$root_dir/dist" && find "$archive_name" -print | LC_ALL=C sort) > "$temp_dir/archive-files"
if tar --version 2>/dev/null | grep -q 'GNU tar'; then
  COPYFILE_DISABLE=1 tar --no-recursion --no-xattrs --owner=0 --group=0 --numeric-owner -cf "$temp_dir/release.tar" -C "$root_dir/dist" -T "$temp_dir/archive-files"
else
  COPYFILE_DISABLE=1 tar --no-recursion --no-xattrs --uid 0 --gid 0 --uname root --gname root -cf "$temp_dir/release.tar" -C "$root_dir/dist" -T "$temp_dir/archive-files"
fi
gzip -n -9 -c "$temp_dir/release.tar" > "$output_part"
archive_digest=$(checksum_file "$output_part")
printf '%s  %s\n' "$archive_digest" "$(basename "$output")" > "$checksum_part"
rm -f "$checksum_output"
mv -f "$output_part" "$output"
mv -f "$checksum_part" "$checksum_output"
printf '%s\n' "$output"
printf '%s\n' "$checksum_output"
