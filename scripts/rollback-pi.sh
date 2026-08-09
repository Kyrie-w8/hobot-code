#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'hobot-rollback must run as root\n' >&2
  exit 1
fi

backup_root=/usr/local/lib/hobot-code-backups
lock_dir=/usr/local/lib/hobot-code.install.lock
backup_dir=${1:-}
rollback_complete=0
runtime_swapped=0
previous_runtime="/usr/local/lib/hobot-code-before-rollback.$$"
previous_launcher="/usr/local/bin/hobot.before-rollback.$$"

if ! mkdir "$lock_dir" 2>/dev/null; then
  printf 'Another Hobot Code install or rollback is already running: %s\n' "$lock_dir" >&2
  exit 1
fi
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  set +e
  if [ "$status" -ne 0 ] && [ "$rollback_complete" -ne 1 ] && [ "$runtime_swapped" -eq 1 ]; then
    printf 'Rollback failed; restoring the runtime that was active before rollback.\n' >&2
    if [ -d /usr/local/lib/hobot-code ]; then
      mv /usr/local/lib/hobot-code "/usr/local/lib/hobot-code-failed-rollback.$$"
    fi
    if [ -d "$previous_runtime" ]; then
      mv "$previous_runtime" /usr/local/lib/hobot-code
    fi
    if [ -f "$previous_launcher" ]; then
      mv "$previous_launcher" /usr/local/bin/hobot
    fi
  fi
  rm -rf "/usr/local/lib/hobot-code.rollback.$$"
  rm -f "/usr/local/bin/hobot.rollback.$$" "$previous_launcher"
  rmdir "$lock_dir" 2>/dev/null || true
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

active_pids=$(pgrep -f '(^|[[:space:]])(/usr/local/bin/hobot|/usr/local/lib/hobot-code/hobot)([[:space:]]|$)' || true)
if [ -n "$active_pids" ]; then
  printf 'Stop active Hobot Code processes before rollback: %s\n' "$active_pids" >&2
  exit 1
fi
if [ -z "$backup_dir" ]; then
  backup_dir=$(find "$backup_root" -mindepth 1 -maxdepth 1 -type d -exec test -d '{}/runtime-installed' ';' -print | sort | tail -n 1)
fi
if [ -z "$backup_dir" ] || [ ! -d "$backup_dir" ]; then
  printf 'No usable Hobot Code backup found under %s\n' "$backup_root" >&2
  exit 1
fi
backup_dir=$(CDPATH= cd -- "$backup_dir" && pwd -P)
case "$backup_dir" in
  "$backup_root"/*) ;;
  *)
    printf 'Refusing backup outside %s: %s\n' "$backup_root" "$backup_dir" >&2
    exit 1
    ;;
esac
if [ ! -d "$backup_dir/runtime-installed" ] || [ ! -e "$backup_dir/hobot-command" ]; then
  printf 'Backup is incomplete: %s\n' "$backup_dir" >&2
  exit 1
fi

staged_runtime="/usr/local/lib/hobot-code.rollback.$$"
staged_launcher="/usr/local/bin/hobot.rollback.$$"
cp -R "$backup_dir/runtime-installed" "$staged_runtime"
install -m 0755 "$backup_dir/hobot-command" "$staged_launcher"
"$staged_runtime/hobot" --version >/dev/null

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
if [ -d /usr/local/lib/hobot-code ]; then
  mv /usr/local/lib/hobot-code "$previous_runtime"
fi
if [ -e /usr/local/bin/hobot ]; then
  install -m 0755 /usr/local/bin/hobot "$previous_launcher"
fi
runtime_swapped=1
mv "$staged_runtime" /usr/local/lib/hobot-code
mv "$staged_launcher" /usr/local/bin/hobot

if [ -d "$backup_dir/legacy-etc-hobot-code" ]; then
  cp -a "$backup_dir/legacy-etc-hobot-code" /etc/hobot-code
fi
if [ -d "$backup_dir/legacy-var-hobot-code" ]; then
  cp -a "$backup_dir/legacy-var-hobot-code" /var/lib/hobot-code
fi

printf 'Restored Hobot Code from %s\n' "$backup_dir"
/usr/local/bin/hobot --version
rollback_complete=1
if [ -d "$previous_runtime" ]; then
  mv "$previous_runtime" "/usr/local/lib/hobot-code-failed-$timestamp"
fi
