#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'hobot-rollback must run as root\n' >&2
  exit 1
fi

backup_root=/usr/local/lib/aster-backups
backup_dir=${1:-}
if [ -z "$backup_dir" ]; then
  backup_dir=$(find "$backup_root" -mindepth 1 -maxdepth 1 -type d | sort | tail -n 1)
fi
if [ -z "$backup_dir" ] || [ ! -d "$backup_dir" ]; then
  printf 'No usable Hobot Code backup found under %s\n' "$backup_root" >&2
  exit 1
fi
if [ ! -e "$backup_dir/aster-command" ] && [ ! -e "$backup_dir/hobot-command" ]; then
  printf 'Backup has no usable Hobot Code or Aster command: %s\n' "$backup_dir" >&2
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
if [ -e "$backup_dir/hobot-command" ]; then
  install -m 0755 "$backup_dir/hobot-command" /usr/local/bin/hobot
else
  install -m 0755 "$backup_dir/aster-command" /usr/local/bin/hobot
fi
if [ -e "$backup_dir/aster-command" ]; then
  install -m 0755 "$backup_dir/aster-command" /usr/local/bin/aster
else
  install -m 0755 "$backup_dir/hobot-command" /usr/local/bin/aster
fi

printf 'Restored Hobot Code command from %s\n' "$backup_dir"
/usr/local/bin/hobot --version 2>/dev/null || true
