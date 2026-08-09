#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'install.sh must run as root\n' >&2
  exit 1
fi

package_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
version=$(cat "$package_dir/VERSION")
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_dir="/usr/local/lib/aster-backups/$timestamp"
new_runtime="/usr/local/lib/aster.new.$$"

install -d -m 0755 /usr/local/lib /usr/local/bin /usr/local/sbin /usr/local/lib/aster-backups
install -d -m 0750 "$backup_dir"

if [ -e /usr/local/bin/aster ]; then
  install -m 0755 /usr/local/bin/aster "$backup_dir/aster-command"
  if [ ! -e /usr/local/bin/aster-legacy ] && [ ! -d /usr/local/lib/aster ]; then
    install -m 0755 /usr/local/bin/aster /usr/local/bin/aster-legacy
  fi
fi
install -d -m 0755 "$new_runtime"
cp -R "$package_dir/runtime/." "$new_runtime/"
install -d -m 0755 "$new_runtime/extensions" "$new_runtime/skills"
cp -R "$package_dir/extensions/." "$new_runtime/extensions/"
cp -R "$package_dir/skills/." "$new_runtime/skills/"
install -m 0644 "$package_dir/PI_RUNTIME" "$new_runtime/PI_RUNTIME"
install -m 0644 "$package_dir/VERSION" "$new_runtime/VERSION"

if [ -d /usr/local/lib/aster ]; then
  mv /usr/local/lib/aster "$backup_dir/runtime-installed"
fi
mv "$new_runtime" /usr/local/lib/aster

install -d -m 0750 /etc/aster /etc/aster/agent
install -d -m 0700 /var/lib/aster /var/lib/aster/pi-sessions
install -d -m 0755 /etc/aster/agent/bin
install -m 0755 "$package_dir/managed-bin/fd" /etc/aster/agent/bin/fd
install -m 0755 "$package_dir/managed-bin/rg" /etc/aster/agent/bin/rg
if [ ! -e /etc/aster/agent/settings.json ]; then
  install -m 0640 "$package_dir/config/settings.json" /etc/aster/agent/settings.json
fi
if [ ! -e /etc/aster/agent/models.json ]; then
  install -m 0640 "$package_dir/config/models.json" /etc/aster/agent/models.json
fi
if [ ! -e /etc/aster/aster.env ]; then
  install -m 0600 "$package_dir/config/aster.env.example" /etc/aster/aster.env
fi

install -m 0755 "$package_dir/aster-launcher" /usr/local/bin/aster
install -m 0755 "$package_dir/rollback.sh" /usr/local/sbin/aster-rollback

if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet aster.service; then
  systemctl disable --now aster.service
  printf 'Stopped legacy aster.service; Aster 0.5 is an interactive Pi-compatible CLI.\n'
fi

printf '%s\n' "$backup_dir" > /usr/local/lib/aster/LAST_BACKUP
/usr/local/bin/aster --version
printf 'Installed Aster %s. Run: aster\n' "$version"
printf 'Rollback: aster-rollback\n'
