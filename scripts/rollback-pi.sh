#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'aster-rollback must run as root\n' >&2
  exit 1
fi

backup_root=/usr/local/lib/aster-backups
backup_dir=${1:-}
if [ -z "$backup_dir" ]; then
  backup_dir=$(find "$backup_root" -mindepth 1 -maxdepth 1 -type d | sort | tail -n 1)
fi
if [ -z "$backup_dir" ] || [ ! -d "$backup_dir" ] || [ ! -e "$backup_dir/aster-command" ]; then
  printf 'No usable Aster backup found under %s\n' "$backup_root" >&2
  exit 1
fi

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
if [ -d /usr/local/lib/aster ]; then
  mv /usr/local/lib/aster "/usr/local/lib/aster-failed-$timestamp"
fi
if [ -d "$backup_dir/runtime" ]; then
  cp -R "$backup_dir/runtime" /usr/local/lib/aster
elif [ -d "$backup_dir/runtime-installed" ]; then
  cp -R "$backup_dir/runtime-installed" /usr/local/lib/aster
fi
install -m 0755 "$backup_dir/aster-command" /usr/local/bin/aster

printf 'Restored Aster command from %s\n' "$backup_dir"
/usr/local/bin/aster --version 2>/dev/null || true
