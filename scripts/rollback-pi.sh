#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'hobot-rollback must run as root\n' >&2
  exit 1
fi

backup_root=/usr/local/lib/hobot-code-backups
backup_dir=${1:-}
if [ -z "$backup_dir" ]; then
  backup_dir=$(find "$backup_root" -mindepth 1 -maxdepth 1 -type d -exec test -d '{}/runtime-installed' ';' -print | sort | tail -n 1)
fi
if [ -z "$backup_dir" ] || [ ! -d "$backup_dir" ]; then
  printf 'No usable Hobot Code backup found under %s\n' "$backup_root" >&2
  exit 1
fi
if [ ! -d "$backup_dir/runtime-installed" ] || [ ! -e "$backup_dir/hobot-command" ]; then
  printf 'Backup is incomplete: %s\n' "$backup_dir" >&2
  exit 1
fi

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
if [ -d /usr/local/lib/hobot-code ]; then
  mv /usr/local/lib/hobot-code "/usr/local/lib/hobot-code-failed-$timestamp"
fi
cp -R "$backup_dir/runtime-installed" /usr/local/lib/hobot-code
install -m 0755 "$backup_dir/hobot-command" /usr/local/bin/hobot

printf 'Restored Hobot Code from %s\n' "$backup_dir"
/usr/local/bin/hobot --version 2>/dev/null || true
