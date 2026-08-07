#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'uninstall.sh must run as root\n' >&2
  exit 1
fi

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now aster.service 2>/dev/null || true
  rm -f /etc/systemd/system/aster.service
  systemctl daemon-reload
fi
rm -f /usr/local/bin/aster
rm -rf /usr/local/share/aster
printf 'Removed Aster binaries. Configuration and sessions under /etc/aster and /var/lib/aster were preserved.\n'
