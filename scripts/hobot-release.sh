#!/bin/sh
set -eu

repository=${HOBOT_CODE_REPOSITORY:-bryant-w/hobot-code}
release_base=${HOBOT_CODE_RELEASE_BASE_URL:-https://github.com/$repository/releases}
action=install
requested_version=
force=0

usage() {
  cat <<'EOF'
Hobot Code release installer

Usage:
  hobot-install.sh [--version <version>] [--force]
  hobot-release update [--version <version>] [--check] [--force]

Options:
  --version <version>  Install an exact released version.
  --check              Report whether an update is available without installing.
  --force              Reinstall when the requested version is already installed.
  -h, --help           Show this help.

Environment:
  HOBOT_CODE_INSTALL_USER  Target OS user when the installer runs as root.
EOF
}

if [ "${1:-}" = install ] || [ "${1:-}" = update ]; then
  action=$1
  shift
fi
check_only=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      if [ "$#" -lt 2 ]; then
        printf '%s\n' '--version requires a value' >&2
        exit 2
      fi
      requested_version=$2
      shift 2
      ;;
    --check)
      check_only=1
      shift
      ;;
    --force)
      force=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown option: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

validate_version() {
  candidate=$1
  if ! printf '%s\n' "$candidate" | awk '
    /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)([+][0-9A-Za-z-]+([.][0-9A-Za-z-]+)*)?$/ { ok=1 }
    /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-[0-9A-Za-z-]+([.][0-9A-Za-z-]+)*([+][0-9A-Za-z-]+([.][0-9A-Za-z-]+)*)?$/ { ok=1 }
    END { exit ok ? 0 : 1 }
  '; then
    printf 'Release version is not valid SemVer: %s\n' "$candidate" >&2
    exit 1
  fi
}

case "$repository" in
  */*) ;;
  *)
    printf 'HOBOT_CODE_REPOSITORY must use owner/repository syntax: %s\n' "$repository" >&2
    exit 1
    ;;
esac
repository_name=${repository#*/}
case "$repository_name" in
  */*)
    printf 'HOBOT_CODE_REPOSITORY must contain exactly one slash: %s\n' "$repository" >&2
    exit 1
    ;;
esac
case "$repository" in
  *[!A-Za-z0-9._/-]*|*//*|/*|*/)
    printf 'HOBOT_CODE_REPOSITORY contains unsupported characters: %s\n' "$repository" >&2
    exit 1
    ;;
esac

kernel=$(uname -s)
architecture=$(uname -m)
if [ "$kernel" != Linux ] || { [ "$architecture" != aarch64 ] && [ "$architecture" != arm64 ]; }; then
  if [ "${HOBOT_CODE_ALLOW_UNSUPPORTED:-0}" != 1 ]; then
    printf 'Hobot Code supports Linux ARM64 RDK boards; detected %s/%s.\n' "$kernel" "$architecture" >&2
    exit 1
  fi
fi

board_model=
for model_path in /sys/firmware/devicetree/base/model /proc/device-tree/model; do
  if [ -r "$model_path" ]; then
    board_model=$(tr -d '\000' < "$model_path" 2>/dev/null || true)
    [ -n "$board_model" ] && break
  fi
done
case $(printf '%s' "$board_model" | tr '[:upper:]' '[:lower:]') in
  *x5*|*s100*|*s600*) ;;
  *)
    if [ "${HOBOT_CODE_ALLOW_UNSUPPORTED:-0}" != 1 ]; then
      printf 'This does not appear to be an RDK X5, S100, or S600: %s\n' "${board_model:-unknown board}" >&2
      exit 1
    fi
    ;;
esac

for command_name in curl tar awk sed mktemp cp; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'Required command is not installed: %s\n' "$command_name" >&2
    exit 127
  fi
done

download() {
  release_download_url=$1
  release_download_destination=$2
  release_download_maximum_bytes=$3
  case "$release_download_url" in
    https://*)
      curl --proto '=https' --tlsv1.2 -fsSL --connect-timeout 20 --retry 3 --retry-delay 2 \
        --retry-all-errors --max-filesize "$release_download_maximum_bytes" \
        "$release_download_url" -o "$release_download_destination"
      ;;
    http://127.0.0.1:*|http://localhost:*)
      if [ "${HOBOT_CODE_TESTING:-0}" != 1 ]; then
        printf 'Release downloads require HTTPS: %s\n' "$release_download_url" >&2
        exit 1
      fi
      curl -fsSL --connect-timeout 5 "$release_download_url" -o "$release_download_destination"
      ;;
    file://*)
      if [ "${HOBOT_CODE_TESTING:-0}" != 1 ]; then
        printf 'Release downloads require HTTPS: %s\n' "$release_download_url" >&2
        exit 1
      fi
      cp "${release_download_url#file://}" "$release_download_destination"
      ;;
    *)
      printf 'Release downloads require HTTPS: %s\n' "$release_download_url" >&2
      exit 1
      ;;
  esac
  release_downloaded_bytes=$(wc -c < "$release_download_destination" | tr -d ' ')
  if [ -z "$release_downloaded_bytes" ] || [ "$release_downloaded_bytes" -gt "$release_download_maximum_bytes" ]; then
    printf 'Release download exceeds %s bytes: %s\n' "$release_download_maximum_bytes" "$release_download_url" >&2
    exit 1
  fi
}

temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/hobot-release.XXXXXX")
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  rm -rf "$temporary_directory"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

if [ -n "$requested_version" ]; then
  validate_version "$requested_version"
  version=$requested_version
  release_path="$release_base/download/v$version"
else
  version_file="$temporary_directory/hobot-code-version.txt"
  download "$release_base/latest/download/hobot-code-version.txt" "$version_file" 4096
  if [ "$(wc -l < "$version_file" | tr -d ' ')" -ne 1 ]; then
    printf 'Release version metadata must contain exactly one record.\n' >&2
    exit 1
  fi
  version=$(sed -n '1p' "$version_file" | tr -d '\r')
  validate_version "$version"
  release_path="$release_base/latest/download"
fi

installed_version=
if [ -r /usr/local/lib/hobot-code/VERSION ]; then
  installed_version=$(sed -n '1p' /usr/local/lib/hobot-code/VERSION | tr -d '\r')
fi
if [ "$check_only" -eq 1 ]; then
  if [ "$installed_version" = "$version" ]; then
    printf 'Hobot Code %s is current.\n' "$version"
  elif [ -n "$installed_version" ]; then
    printf 'Hobot Code %s is available; installed version is %s.\n' "$version" "$installed_version"
  else
    printf 'Hobot Code %s is available and is not installed.\n' "$version"
  fi
  exit 0
fi
if [ "$installed_version" = "$version" ] && [ "$force" -ne 1 ]; then
  printf 'Hobot Code %s is already installed. Use --force to reinstall.\n' "$version"
  exit 0
fi

archive_name="hobot-code-$version-linux-arm64.tar.gz"
archive="$temporary_directory/$archive_name"
checksum="$archive.sha256"
download "$release_path/$archive_name" "$archive" 268435456
download "$release_path/$archive_name.sha256" "$checksum" 4096

checksum_record=$(sed -n '1p' "$checksum" | tr -d '\r')
case "$checksum_record" in
  [0-9a-f][0-9a-f]*) ;;
  *) printf 'Release checksum is malformed.\n' >&2; exit 1 ;;
esac
checksum_digest=${checksum_record%% *}
checksum_target=${checksum_record#*  }
case "$checksum_digest" in
  *[!0-9a-f]*) checksum_valid=0 ;;
  *) checksum_valid=1 ;;
esac
if [ "$checksum_valid" -ne 1 ] || [ "${#checksum_digest}" -ne 64 ] || [ "$checksum_target" != "$archive_name" ] || [ "$checksum_record" != "$checksum_digest  $checksum_target" ]; then
  printf 'Release checksum must contain exactly one SHA256 record for %s.\n' "$archive_name" >&2
  exit 1
fi
if [ "$(wc -l < "$checksum" | tr -d ' ')" -ne 1 ]; then
  printf 'Release checksum must contain exactly one record.\n' >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$temporary_directory" && sha256sum -c "$(basename "$checksum")")
elif command -v shasum >/dev/null 2>&1; then
  (cd "$temporary_directory" && shasum -a 256 -c "$(basename "$checksum")")
else
  printf 'sha256sum or shasum is required to verify Hobot Code.\n' >&2
  exit 127
fi

expected_root="hobot-code-$version-linux-arm64"
listing="$temporary_directory/archive-listing"
verbose_listing="$temporary_directory/archive-verbose"
tar -tzf "$archive" > "$listing"
tar -tvzf "$archive" > "$verbose_listing"
member_count=0
while IFS= read -r member_name || [ -n "$member_name" ]; do
  member_count=$((member_count + 1))
  canonical_name=${member_name%/}
  case "$canonical_name" in
    "$expected_root"|"$expected_root"/*) ;;
    *)
      printf 'Release archive contains a path outside %s/: %s\n' "$expected_root" "$member_name" >&2
      exit 1
      ;;
  esac
  case "/$canonical_name/" in
    *//*|*/./*|*/../*)
      printf 'Release archive contains a non-canonical path: %s\n' "$member_name" >&2
      exit 1
      ;;
  esac
done < "$listing"
if [ "$member_count" -eq 0 ]; then
  printf 'Release archive is empty.\n' >&2
  exit 1
fi
unsupported_type=$(awk 'substr($0, 1, 1) != "-" && substr($0, 1, 1) != "d" { print substr($0, 1, 1); exit }' "$verbose_listing")
if [ -n "$unsupported_type" ]; then
  printf 'Release archive contains unsupported entry type: %s\n' "$unsupported_type" >&2
  exit 1
fi

tar -xzf "$archive" -C "$temporary_directory"
package_root="$temporary_directory/$expected_root"
if [ ! -x "$package_root/install.sh" ]; then
  printf 'Release archive does not contain an executable installer.\n' >&2
  exit 1
fi

if [ "$(id -u)" -eq 0 ]; then
  HOBOT_CODE_INSTALL_CHANNEL=stable "$package_root/install.sh"
else
  if ! command -v sudo >/dev/null 2>&1; then
    printf 'Installing Hobot Code requires root privileges; sudo is not installed.\n' >&2
    exit 1
  fi
  install_user=${HOBOT_CODE_INSTALL_USER:-$(id -un)}
  install_home=${HOBOT_CODE_INSTALL_HOME:-$HOME}
  sudo env \
    "HOBOT_CODE_INSTALL_USER=$install_user" \
    "HOBOT_CODE_INSTALL_HOME=$install_home" \
    HOBOT_CODE_INSTALL_CHANNEL=stable \
    "$package_root/install.sh"
fi

if [ "$action" = update ]; then
  printf 'Updated Hobot Code to %s.\n' "$version"
fi
