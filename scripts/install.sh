#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'install.sh must run as root\n' >&2
  exit 1
fi

enable_service=false
if [ "${1:-}" = "--enable-service" ]; then
  enable_service=true
fi

package_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
install -d -m 0755 /usr/local/bin /usr/local/share/aster/prompts /usr/local/share/aster/skills
install -d -m 0755 /etc/aster /etc/aster/boards /etc/aster/providers /var/lib/aster/workspace /var/lib/aster/sessions
install -m 0755 "$package_dir/bin/aster" /usr/local/bin/aster
install -m 0644 "$package_dir/prompts/system.md" /usr/local/share/aster/prompts/system.md
cp -R "$package_dir/skills/." /usr/local/share/aster/skills/
cp -R "$package_dir/config/boards/." /etc/aster/boards/
cp -R "$package_dir/config/providers/." /etc/aster/providers/

if [ ! -e /etc/aster/config.json ]; then
  install -m 0640 "$package_dir/config/config.json" /etc/aster/config.json
fi
if [ ! -e /etc/aster/aster.env ]; then
  install -m 0600 "$package_dir/config/aster.env.example" /etc/aster/aster.env
fi
if [ ! -e /etc/aster/launcher.json ]; then
  install -m 0644 "$package_dir/config/launcher.json" /etc/aster/launcher.json
fi

if command -v systemctl >/dev/null 2>&1; then
  install -m 0644 "$package_dir/config/aster.service" /etc/systemd/system/aster.service
  systemctl daemon-reload
  if [ "$enable_service" = true ]; then
    systemctl enable --now aster.service
  fi
fi

/usr/local/bin/aster --version
printf 'Installed Aster. Run: aster\n'
