#!/bin/sh
set -eu

repository=${HOBOT_CODE_REPOSITORY:-bryant-w/hobot-code}
release_base=${HOBOT_CODE_RELEASE_BASE_URL:-https://github.com/$repository/releases}
installed_runtime_root=/usr/local/lib/hobot-code
installed_launcher=/usr/local/bin/hobot
process_root=/proc
action=install
requested_version=
force=0
allow_downgrade=0
daemon_was_running=0
daemon_stopped_for_update=0

if [ "${HOBOT_CODE_TESTING:-0}" = 1 ] && [ -n "${HOBOT_CODE_TEST_INSTALL_ROOT:-}" ]; then
  case "$HOBOT_CODE_TEST_INSTALL_ROOT" in
    /*) ;;
    *) printf 'HOBOT_CODE_TEST_INSTALL_ROOT must be absolute.\n' >&2; exit 2 ;;
  esac
  installed_runtime_root=$HOBOT_CODE_TEST_INSTALL_ROOT/usr/local/lib/hobot-code
  installed_launcher=$HOBOT_CODE_TEST_INSTALL_ROOT/usr/local/bin/hobot
fi
if [ "${HOBOT_CODE_TESTING:-0}" = 1 ] && [ -n "${HOBOT_CODE_TEST_PROC_ROOT:-}" ]; then
  case "$HOBOT_CODE_TEST_PROC_ROOT" in
    /*) process_root=$HOBOT_CODE_TEST_PROC_ROOT ;;
    *) printf 'HOBOT_CODE_TEST_PROC_ROOT must be absolute.\n' >&2; exit 2 ;;
  esac
fi

usage() {
  cat <<'EOF'
Hobot Code release installer

Usage:
  hobot-install.sh [--version <version>] [--force] [--allow-downgrade]
  hobot-release update [--version <version>] [--check] [--force] [--allow-downgrade]

Options:
  --version <version>  Install an exact released version.
  --check              Report whether an update is available without installing.
  --force              Reinstall when the requested version is already installed.
  --allow-downgrade    Allow an explicitly requested older version.
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
    --allow-downgrade)
      allow_downgrade=1
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

if [ "$allow_downgrade" -eq 1 ] && [ -z "$requested_version" ]; then
  printf '%s\n' '--allow-downgrade requires an explicit --version.' >&2
  exit 2
fi

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

compare_versions() {
  compare_left=$1
  compare_right=$2
  awk -v left="$compare_left" -v right="$compare_right" '
    function numeric_compare(a, b, normalized_a, normalized_b) {
      normalized_a=a; sub(/^0+/, "", normalized_a); if (normalized_a == "") normalized_a="0"
      normalized_b=b; sub(/^0+/, "", normalized_b); if (normalized_b == "") normalized_b="0"
      if (length(normalized_a) != length(normalized_b)) return length(normalized_a) < length(normalized_b) ? -1 : 1
      if (normalized_a == normalized_b) return 0
      return normalized_a < normalized_b ? -1 : 1
    }
    function compare_identifier(a, b, a_numeric, b_numeric) {
      a_numeric = a ~ /^[0-9]+$/
      b_numeric = b ~ /^[0-9]+$/
      if (a_numeric && b_numeric) return numeric_compare(a, b)
      if (a_numeric != b_numeric) return a_numeric ? -1 : 1
      if (a == b) return 0
      return a < b ? -1 : 1
    }
    function compare(version_a, version_b, build_at, dash_at, core_a, core_b, pre_a, pre_b, core_parts_a, core_parts_b, pre_parts_a, pre_parts_b, count_a, count_b, part_index, result) {
      build_at=index(version_a, "+"); if (build_at) version_a=substr(version_a, 1, build_at - 1)
      build_at=index(version_b, "+"); if (build_at) version_b=substr(version_b, 1, build_at - 1)
      dash_at=index(version_a, "-"); core_a=dash_at ? substr(version_a, 1, dash_at - 1) : version_a; pre_a=dash_at ? substr(version_a, dash_at + 1) : ""
      dash_at=index(version_b, "-"); core_b=dash_at ? substr(version_b, 1, dash_at - 1) : version_b; pre_b=dash_at ? substr(version_b, dash_at + 1) : ""
      split(core_a, core_parts_a, "."); split(core_b, core_parts_b, ".")
      for (part_index=1; part_index<=3; part_index++) {
        result=numeric_compare(core_parts_a[part_index], core_parts_b[part_index])
        if (result) return result
      }
      if (pre_a == pre_b) return 0
      if (pre_a == "") return 1
      if (pre_b == "") return -1
      count_a=split(pre_a, pre_parts_a, "."); count_b=split(pre_b, pre_parts_b, ".")
      for (part_index=1; part_index<=count_a && part_index<=count_b; part_index++) {
        result=compare_identifier(pre_parts_a[part_index], pre_parts_b[part_index])
        if (result) return result
      }
      if (count_a == count_b) return 0
      return count_a < count_b ? -1 : 1
    }
    BEGIN { print compare(left, right) }
  '
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
  release_download_error=$temporary_directory/download-error.txt
  : > "$release_download_error"
  case "$release_download_url" in
    https://*)
      if [ "$check_only" -eq 1 ]; then
        if ! curl --proto '=https' --tlsv1.2 -fsSL --connect-timeout 5 --max-time 10 \
          --max-filesize "$release_download_maximum_bytes" \
          "$release_download_url" -o "$release_download_destination" 2>"$release_download_error"; then
          printf '%s\n' 'Unable to check for Hobot Code updates within 10 seconds.' >&2
          printf '%s\n' 'The installed version was not changed. Check the board network and try again.' >&2
          if [ "${HOBOT_CODE_DEBUG:-0}" = 1 ]; then
            sed -n '1,5p' "$release_download_error" >&2
          fi
          return 1
        fi
      elif ! curl --proto '=https' --tlsv1.2 -fsSL --connect-timeout 20 --retry 3 --retry-delay 2 \
        --retry-all-errors --max-filesize "$release_download_maximum_bytes" \
        "$release_download_url" -o "$release_download_destination" 2>"$release_download_error"; then
        printf '%s\n' 'Unable to download the Hobot Code release.' >&2
        printf '%s\n' 'The installed version was not changed. Check the board network and try again.' >&2
        if [ "${HOBOT_CODE_DEBUG:-0}" = 1 ]; then
          sed -n '1,5p' "$release_download_error" >&2
        fi
        return 1
      fi
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

temporary_directory=
if temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/hobot-release.XXXXXX" 2>/dev/null); then
  :
else
  if [ -z "${HOME:-}" ]; then
    printf 'Cannot create a temporary update directory and HOME is not set.\n' >&2
    exit 1
  fi
  release_cache_home=${XDG_CACHE_HOME:-$HOME/.cache}
  case "$release_cache_home" in
    /*) ;;
    *) printf 'The Hobot Code update cache must be an absolute path: %s\n' "$release_cache_home" >&2; exit 1 ;;
  esac
  release_cache_directory=$release_cache_home/hobot-code
  if [ -L "$release_cache_directory" ]; then
    printf 'Refusing a symbolic-link update cache: %s\n' "$release_cache_directory" >&2
    exit 1
  fi
  mkdir -p "$release_cache_directory"
  chmod 0700 "$release_cache_directory"
  if [ ! -d "$release_cache_directory" ] || [ ! -w "$release_cache_directory" ]; then
    printf 'The Hobot Code update cache is not writable: %s\n' "$release_cache_directory" >&2
    exit 1
  fi
  if release_cache_owner=$(stat -c %u "$release_cache_directory" 2>/dev/null); then :; else
    release_cache_owner=$(stat -f %u "$release_cache_directory")
  fi
  if [ "$release_cache_owner" != "$(id -u)" ]; then
    printf 'The Hobot Code update cache must be owned by the current user: %s\n' "$release_cache_directory" >&2
    exit 1
  fi
  temporary_directory=$(mktemp -d "$release_cache_directory/update.XXXXXX")
fi
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if [ "$daemon_stopped_for_update" -eq 1 ]; then
    if "$installed_launcher" daemon start >/dev/null 2>&1; then
      printf 'Restored the Hobot Code background service after the interrupted update.\n' >&2
      daemon_stopped_for_update=0
    else
      printf 'Warning: the update was interrupted and the background service could not be restarted. Run: hobot daemon start\n' >&2
    fi
  fi
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
  # Pin payload downloads to the version resolved above. The mutable latest
  # pointer is metadata only and can move while an update is in progress.
  release_path="$release_base/download/v$version"
fi

installed_version=
if [ -r "$installed_runtime_root/VERSION" ]; then
  installed_version=$(sed -n '1p' "$installed_runtime_root/VERSION" | tr -d '\r')
fi
version_order=0
if [ -n "$installed_version" ]; then
  validate_version "$installed_version"
  version_order=$(compare_versions "$version" "$installed_version")
fi
if [ "$check_only" -eq 1 ]; then
  if [ "$version_order" -lt 0 ]; then
    printf 'Hobot Code release metadata reports %s, older than installed version %s; no update will be applied.\n' "$version" "$installed_version"
    printf 'The release source may be stale. Use an explicit --version only after verifying the release.\n'
  elif [ -n "$installed_version" ] && [ "$version_order" -eq 0 ]; then
    printf 'Hobot Code %s is current.\n' "$version"
  elif [ -n "$installed_version" ]; then
    printf 'Hobot Code %s is available; installed version is %s.\n' "$version" "$installed_version"
  else
    printf 'Hobot Code %s is available and is not installed.\n' "$version"
  fi
  exit 0
fi
if [ "$version_order" -lt 0 ] && [ "$allow_downgrade" -ne 1 ]; then
  printf 'Refusing to downgrade Hobot Code from %s to %s.\n' "$installed_version" "$version" >&2
  if [ -z "$requested_version" ]; then
    printf 'The latest release metadata appears stale; the installed version was left unchanged.\n' >&2
  else
    printf 'To intentionally downgrade, verify the release and add --allow-downgrade.\n' >&2
  fi
  exit 1
fi
if [ -n "$installed_version" ] && [ "$version_order" -eq 0 ] && [ "$force" -ne 1 ]; then
  printf 'Hobot Code %s is already installed. Use --force to reinstall.\n' "$version"
  exit 0
fi

inspect_installed_runtime() {
  inspected_daemon_pids=
  inspected_runtime_pids=
  inspected_bridge_pids=
  for inspected_process_path in "$process_root"/[0-9]*; do
    [ -r "$inspected_process_path/exe" ] || continue
    inspected_executable=$(readlink "$inspected_process_path/exe" 2>/dev/null || true)
    inspected_executable=${inspected_executable% (deleted)}
    inspected_pid=${inspected_process_path##*/}
    case "$inspected_executable" in
      "$installed_runtime_root/agentd")
        inspected_agentd_action=$(tr '\000' '\n' < "$inspected_process_path/cmdline" 2>/dev/null | sed -n '2p' || true)
        if [ "$inspected_agentd_action" = serve ]; then
          inspected_daemon_pids="${inspected_daemon_pids}${inspected_daemon_pids:+ }$inspected_pid"
        else
          inspected_bridge_pids="${inspected_bridge_pids}${inspected_bridge_pids:+ }$inspected_pid"
        fi
        ;;
      "$installed_runtime_root/hobot")
        inspected_runtime_pids="${inspected_runtime_pids}${inspected_runtime_pids:+ }$inspected_pid"
        ;;
    esac
  done

  if [ -n "$inspected_bridge_pids" ]; then
    inspected_runtime_pids="${inspected_runtime_pids}${inspected_runtime_pids:+ }$inspected_bridge_pids"
  fi

  if [ -n "$inspected_daemon_pids" ]; then
    if [ "$(printf '%s\n' "$inspected_daemon_pids" | awk '{ print NF }')" -ne 1 ]; then
      printf 'Multiple Hobot Code background services are running (PIDs: %s); the update was not started.\n' "$inspected_daemon_pids" >&2
      return 1
    fi
    if [ ! -x "$installed_launcher" ]; then
      printf 'The Hobot Code background service is running but its launcher is unavailable; the update was not started.\n' >&2
      return 1
    fi
    inspected_status=$("$installed_launcher" daemon status 2>/dev/null || true)
    inspected_active_tasks=$(printf '%s\n' "$inspected_status" | tr -d '\r\n' | sed -n 's/^.*"activeTasks"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*$/\1/p')
    case "$inspected_active_tasks" in
      ''|*[!0-9]*)
        printf 'The running Hobot Code service did not return a trustworthy task count; the update was not started.\n' >&2
        printf 'Check it with: hobot daemon status\n' >&2
        return 1
        ;;
    esac
    if [ "$inspected_active_tasks" -gt 0 ]; then
      printf 'Hobot Code has %s active board-side task(s); the update was not started.\n' "$inspected_active_tasks" >&2
      printf 'Let them finish or stop them explicitly, then run hobot update again. Check: hobot task list\n' >&2
      return 1
    fi
  fi

  if [ -n "$inspected_runtime_pids" ]; then
    inspected_runtime_count=$(printf '%s\n' "$inspected_runtime_pids" | awk '{ print NF }')
    inspected_runtime_label='PID'
    [ "$inspected_runtime_count" -eq 1 ] || inspected_runtime_label='PIDs'
    printf 'Hobot Code is currently in use by a foreground, persistent, automation, or Studio bridge session (%s: %s).\n' \
      "$inspected_runtime_label" "$inspected_runtime_pids" >&2
    printf 'Finish that work before updating. Useful checks: hobot persistent list; hobot task list\n' >&2
    return 1
  fi

  if [ -z "$inspected_daemon_pids" ]; then
    daemon_was_running=0
    return 0
  fi
  daemon_was_running=1
}

prepare_installed_runtime_for_update() {
  [ -n "$installed_version" ] || return 0
  inspect_installed_runtime || return 1
  [ "$daemon_was_running" -eq 1 ] || return 0
  if ! "$installed_launcher" daemon stop; then
    printf 'The idle background service could not be stopped safely; the update was not applied.\n' >&2
    return 1
  fi
  daemon_stopped_for_update=1

  # Close the race between inspection and installation while allowing the
  # daemon process a bounded interval to finish exiting after its socket closes.
  inspected_wait_attempt=0
  while :; do
    inspected_remaining=
    for inspected_process_path in "$process_root"/[0-9]*; do
      [ -r "$inspected_process_path/exe" ] || continue
      inspected_executable=$(readlink "$inspected_process_path/exe" 2>/dev/null || true)
      inspected_executable=${inspected_executable% (deleted)}
      case "$inspected_executable" in
        "$installed_runtime_root/hobot"|"$installed_runtime_root/agentd")
          inspected_remaining="${inspected_remaining}${inspected_remaining:+ }${inspected_process_path##*/}"
          ;;
      esac
    done
    [ -z "$inspected_remaining" ] && break
    inspected_wait_attempt=$((inspected_wait_attempt + 1))
    [ "$inspected_wait_attempt" -ge 5 ] && break
    sleep 1
  done
  if [ -n "$inspected_remaining" ]; then
    printf 'Hobot Code became active while preparing the update (PIDs: %s); the update was not applied.\n' "$inspected_remaining" >&2
    return 1
  fi
}

# Avoid downloading a large release when current work already blocks an update.
if [ -n "$installed_version" ]; then
  inspect_installed_runtime
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

prepare_installed_runtime_for_update

if [ "${HOBOT_CODE_TESTING:-0}" = 1 ] && [ -n "${HOBOT_CODE_TEST_INSTALL_ROOT:-}" ]; then
  privileged_staging_template=$HOBOT_CODE_TEST_INSTALL_ROOT/usr/local/lib/hobot-code.package.XXXXXX
else
  privileged_staging_template=/usr/local/lib/hobot-code.package.XXXXXX
fi
if [ "$(id -u)" -eq 0 ]; then
  install_user=${HOBOT_CODE_INSTALL_USER:-${SUDO_USER:-root}}
  install_home=${HOBOT_CODE_INSTALL_HOME:-}
else
  if ! command -v sudo >/dev/null 2>&1; then
    printf 'Installing Hobot Code requires root privileges; sudo is not installed.\n' >&2
    exit 1
  fi
  install_user=${HOBOT_CODE_INSTALL_USER:-$(id -un)}
  install_home=${HOBOT_CODE_INSTALL_HOME:-$HOME}
fi

privileged_installer='set -eu
source_archive=$1
archive_name=$2
expected_root=$3
expected_digest=$4
staging_template=$5
install_user=$6
install_home=$7
testing=$8
test_install_root=$9
stage=$(mktemp -d "$staging_template")
cleanup_stage() { status=$?; trap - EXIT HUP INT TERM; rm -rf "$stage"; exit "$status"; }
trap cleanup_stage EXIT
trap "exit 130" HUP INT TERM
chmod 0700 "$stage"
install -m 0600 "$source_archive" "$stage/$archive_name"
if command -v sha256sum >/dev/null 2>&1; then
  printf "%s  %s\n" "$expected_digest" "$archive_name" | (cd "$stage" && sha256sum -c - >/dev/null)
elif command -v shasum >/dev/null 2>&1; then
  printf "%s  %s\n" "$expected_digest" "$archive_name" | (cd "$stage" && shasum -a 256 -c - >/dev/null)
else
  printf "sha256sum or shasum is required to verify the privileged staging copy.\n" >&2
  exit 127
fi
tar -xzf "$stage/$archive_name" -C "$stage"
package_root=$stage/$expected_root
if [ ! -x "$package_root/install.sh" ]; then
  printf "Release archive does not contain an executable installer.\n" >&2
  exit 1
fi
if [ -n "$install_home" ]; then
  env HOBOT_CODE_INSTALL_USER="$install_user" HOBOT_CODE_INSTALL_HOME="$install_home" HOBOT_CODE_INSTALL_CHANNEL=stable HOBOT_CODE_TESTING="$testing" HOBOT_CODE_TEST_INSTALL_ROOT="$test_install_root" "$package_root/install.sh"
else
  env HOBOT_CODE_INSTALL_USER="$install_user" HOBOT_CODE_INSTALL_CHANNEL=stable HOBOT_CODE_TESTING="$testing" HOBOT_CODE_TEST_INSTALL_ROOT="$test_install_root" "$package_root/install.sh"
fi'

if [ "$(id -u)" -eq 0 ]; then
  /bin/sh -c "$privileged_installer" hobot-installer "$archive" "$archive_name" "$expected_root" "$checksum_digest" "$privileged_staging_template" "$install_user" "$install_home" "${HOBOT_CODE_TESTING:-0}" "${HOBOT_CODE_TEST_INSTALL_ROOT:-}"
else
  sudo /bin/sh -c "$privileged_installer" hobot-installer "$archive" "$archive_name" "$expected_root" "$checksum_digest" "$privileged_staging_template" "$install_user" "$install_home" "${HOBOT_CODE_TESTING:-0}" "${HOBOT_CODE_TEST_INSTALL_ROOT:-}"
fi

if [ "$daemon_was_running" -eq 1 ]; then
  if ! "$installed_launcher" daemon start; then
    printf 'Hobot Code %s was installed, but its background service did not restart. Run: hobot daemon start\n' "$version" >&2
    exit 1
  fi
  daemon_stopped_for_update=0
fi

if [ "$action" = update ]; then
  if [ "$daemon_was_running" -eq 1 ]; then
    printf 'Updated Hobot Code to %s and restarted its background service.\n' "$version"
  else
    printf 'Updated Hobot Code to %s. The background service remains stopped because it was not running before the update.\n' "$version"
  fi
fi
