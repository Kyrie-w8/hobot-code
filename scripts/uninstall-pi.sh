#!/bin/sh
set -eu

purge=0
assume_yes=0
usage() {
  cat <<'EOF'
Usage: hobot uninstall [--purge] [--yes]

By default, uninstall removes the program and preserves user configuration,
sessions, memory, goals, and installation backups. --purge deletes those data
after explicit confirmation; use --yes only for unattended removal.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --purge) purge=1 ;;
    --yes) assume_yes=1 ;;
    -h|--help) usage; exit 0 ;;
    *) printf 'Unknown uninstall option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

if [ "$(id -u)" -ne 0 ]; then
  if ! command -v sudo >/dev/null 2>&1; then
    printf 'Uninstalling Hobot Code requires root privileges; sudo is not installed.\n' >&2
    exit 1
  fi
  uninstall_user=${HOBOT_CODE_UNINSTALL_USER:-$(id -un)}
  uninstall_home=${HOBOT_CODE_UNINSTALL_HOME:-$HOME}
  set --
  if [ "$purge" -eq 1 ]; then set -- "$@" --purge; fi
  if [ "$assume_yes" -eq 1 ]; then set -- "$@" --yes; fi
  exec sudo env \
    "HOBOT_CODE_UNINSTALL_USER=$uninstall_user" \
    "HOBOT_CODE_UNINSTALL_HOME=$uninstall_home" \
    "HOBOT_CODE_TESTING=${HOBOT_CODE_TESTING:-0}" \
    "HOBOT_CODE_TEST_INSTALL_ROOT=${HOBOT_CODE_TEST_INSTALL_ROOT:-}" \
    "HOBOT_CODE_TEST_PROC_ROOT=${HOBOT_CODE_TEST_PROC_ROOT:-}" \
    "$0" "$@"
fi

testing=${HOBOT_CODE_TESTING:-0}
install_root=
if [ -n "${HOBOT_CODE_TEST_INSTALL_ROOT:-}" ]; then
  if [ "$testing" != 1 ]; then
    printf 'HOBOT_CODE_TEST_INSTALL_ROOT requires HOBOT_CODE_TESTING=1\n' >&2
    exit 1
  fi
  case "$HOBOT_CODE_TEST_INSTALL_ROOT" in
    /|''|*[!A-Za-z0-9_./-]*)
      printf 'HOBOT_CODE_TEST_INSTALL_ROOT is unsafe: %s\n' "$HOBOT_CODE_TEST_INSTALL_ROOT" >&2
      exit 1
      ;;
    /*) ;;
    *) printf 'HOBOT_CODE_TEST_INSTALL_ROOT must be absolute\n' >&2; exit 1 ;;
  esac
  if [ -L "$HOBOT_CODE_TEST_INSTALL_ROOT" ] || [ ! -d "$HOBOT_CODE_TEST_INSTALL_ROOT" ]; then
    printf 'HOBOT_CODE_TEST_INSTALL_ROOT must be a real directory\n' >&2
    exit 1
  fi
  install_root_logical=$(CDPATH= cd -L -- "$HOBOT_CODE_TEST_INSTALL_ROOT" && pwd -L)
  install_root=$(CDPATH= cd -P -- "$HOBOT_CODE_TEST_INSTALL_ROOT" && pwd -P)
  if [ "$install_root_logical" != "$install_root" ] || [ "$(stat -c %u "$install_root")" -ne 0 ]; then
    printf 'HOBOT_CODE_TEST_INSTALL_ROOT must not traverse links and must be owned by root\n' >&2
    exit 1
  fi
  install_root_mode=$(stat -c %a "$install_root")
  if [ $((0$install_root_mode & 022)) -ne 0 ]; then
    printf 'HOBOT_CODE_TEST_INSTALL_ROOT must not be writable by group or other users\n' >&2
    exit 1
  fi
fi

local_lib_root="$install_root/usr/local/lib"
runtime_root="$local_lib_root/hobot-code"
backup_root="$local_lib_root/hobot-code-backups"
launcher_path="$install_root/usr/local/bin/hobot"
rollback_path="$install_root/usr/local/sbin/hobot-rollback"
lock_dir="$local_lib_root/hobot-code.install.lock"
proc_root=/proc
if [ -n "${HOBOT_CODE_TEST_PROC_ROOT:-}" ]; then
  if [ "$testing" != 1 ] || [ -z "$install_root" ]; then
    printf 'HOBOT_CODE_TEST_PROC_ROOT requires an isolated test install root\n' >&2
    exit 1
  fi
  case "$HOBOT_CODE_TEST_PROC_ROOT" in /*) proc_root=$HOBOT_CODE_TEST_PROC_ROOT ;; *) printf 'HOBOT_CODE_TEST_PROC_ROOT must be absolute\n' >&2; exit 1 ;; esac
  if [ -L "$proc_root" ] || [ ! -d "$proc_root" ]; then
    printf 'HOBOT_CODE_TEST_PROC_ROOT must be a real directory\n' >&2
    exit 1
  fi
fi

uninstall_user=${HOBOT_CODE_UNINSTALL_USER:-${SUDO_USER:-root}}
if [ -n "${HOBOT_CODE_UNINSTALL_HOME:-}" ]; then
  uninstall_home=$HOBOT_CODE_UNINSTALL_HOME
else
  uninstall_home=$(getent passwd "$uninstall_user" | cut -d: -f6)
fi
case "$uninstall_home" in
  /*) ;;
  *) printf 'Cannot resolve an absolute home directory for %s.\n' "$uninstall_user" >&2; exit 1 ;;
esac
if [ "$uninstall_home" = / ] || [ -L "$uninstall_home" ] || [ ! -d "$uninstall_home" ]; then
  printf 'Refusing unsafe home directory for %s: %s\n' "$uninstall_user" "$uninstall_home" >&2
  exit 1
fi
uninstall_home_logical=$(CDPATH= cd -L -- "$uninstall_home" && pwd -L)
uninstall_home_physical=$(CDPATH= cd -P -- "$uninstall_home" && pwd -P)
if [ "$uninstall_home_logical" != "$uninstall_home_physical" ]; then
  printf 'Refusing a home directory that traverses symbolic links: %s\n' "$uninstall_home" >&2
  exit 1
fi
uninstall_home=$uninstall_home_physical
if [ -n "$install_root" ]; then
  case "$uninstall_home" in
    "$install_root"/*) ;;
    *) printf 'Isolated uninstall home must stay under HOBOT_CODE_TEST_INSTALL_ROOT\n' >&2; exit 1 ;;
  esac
fi
uninstall_uid=$(id -u "$uninstall_user")
if [ "$(stat -c %u "$uninstall_home")" != "$uninstall_uid" ]; then
  printf 'Home directory is not owned by %s: %s\n' "$uninstall_user" "$uninstall_home" >&2
  exit 1
fi

for managed_path in "$runtime_root" "$launcher_path" "$rollback_path" "$backup_root"; do
  if [ -L "$managed_path" ]; then
    printf 'Refusing to uninstall through symbolic link: %s\n' "$managed_path" >&2
    exit 1
  fi
done

config_root="$uninstall_home/.config/hobot-code"
state_root="$uninstall_home/.local/state/hobot-code"
if [ "$purge" -eq 1 ]; then
  for user_path in "$uninstall_home/.config" "$config_root" "$uninstall_home/.local" "$uninstall_home/.local/state" "$state_root"; do
    if [ -L "$user_path" ]; then
      printf 'Refusing to purge symbolic-link user path: %s\n' "$user_path" >&2
      exit 1
    fi
  done
fi

active_hobot_pids() {
  detected_pids=
  for process_path in "$proc_root"/[0-9]*; do
    [ -r "$process_path/exe" ] || continue
    executable=$(readlink "$process_path/exe" 2>/dev/null || true)
    executable=${executable% (deleted)}
    case "$executable" in
      "$runtime_root/hobot"|"$runtime_root/agentd")
        process_id=${process_path##*/}
        detected_pids="${detected_pids}${detected_pids:+ }$process_id"
        ;;
    esac
  done
  printf '%s\n' "$detected_pids"
}

active_pids=$(active_hobot_pids)
if [ -n "$active_pids" ]; then
  printf 'Stop active Hobot Code processes before uninstalling: %s\n' "$active_pids" >&2
  exit 1
fi

if [ "$assume_yes" -ne 1 ]; then
  if [ ! -t 0 ] || [ ! -r /dev/tty ]; then
    printf 'Uninstall confirmation requires a terminal; rerun with --yes.\n' >&2
    exit 1
  fi
  if [ "$purge" -eq 1 ]; then
    prompt='Remove Hobot Code and permanently delete its user data and backups? [y/N] '
  else
    prompt='Remove Hobot Code while preserving user data and backups? [y/N] '
  fi
  printf '%s' "$prompt" > /dev/tty
  IFS= read -r answer < /dev/tty || answer=
  case "$answer" in y|Y|yes|YES) ;; *) printf 'Uninstall cancelled.\n'; exit 1 ;; esac
fi

acquire_lock() {
  if mkdir "$lock_dir" 2>/dev/null; then return; fi
  lock_pid=$(sed -n '1p' "$lock_dir/pid" 2>/dev/null || true)
  case "$lock_pid" in
    ''|*[!0-9]*) ;;
    *)
      if ! kill -0 "$lock_pid" 2>/dev/null; then
        rm -rf "$lock_dir"
        if mkdir "$lock_dir" 2>/dev/null; then return; fi
      fi
      ;;
  esac
  printf 'Another Hobot Code install, update, rollback, or uninstall is running.\n' >&2
  exit 1
}
acquire_lock
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  rm -f "$lock_dir/pid"
  rmdir "$lock_dir" 2>/dev/null || true
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM
printf '%s\n' "$$" > "$lock_dir/pid"

rm -rf "$runtime_root"
rm -f "$launcher_path" "$rollback_path"

if [ "$purge" -eq 1 ]; then
  rm -rf "$config_root" "$state_root" "$backup_root"
  printf 'Uninstalled Hobot Code and purged data for %s.\n' "$uninstall_user"
else
  printf 'Uninstalled Hobot Code. User data and backups were preserved.\n'
fi
