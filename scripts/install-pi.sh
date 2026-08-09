#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'install.sh must run as root\n' >&2
  exit 1
fi

package_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
version=$(cat "$package_dir/VERSION")
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_dir="/usr/local/lib/hobot-code-backups/$timestamp"
new_runtime="/usr/local/lib/hobot-code.new.$$"

install -d -m 0755 /usr/local/lib /usr/local/bin /usr/local/sbin /usr/local/lib/hobot-code-backups
install -d -m 0750 "$backup_dir"

if [ -e /usr/local/bin/hobot ]; then
  install -m 0755 /usr/local/bin/hobot "$backup_dir/hobot-command"
fi
install -d -m 0755 "$new_runtime"
cp -R "$package_dir/runtime/." "$new_runtime/"
install -d -m 0755 "$new_runtime/extensions" "$new_runtime/skills" "$new_runtime/knowledge" "$new_runtime/prompts"
cp -R "$package_dir/extensions/." "$new_runtime/extensions/"
cp -R "$package_dir/skills/." "$new_runtime/skills/"
cp -R "$package_dir/knowledge/." "$new_runtime/knowledge/"
cp -R "$package_dir/prompts/." "$new_runtime/prompts/"
install -m 0644 "$package_dir/PI_RUNTIME" "$new_runtime/PI_RUNTIME"
install -m 0644 "$package_dir/VERSION" "$new_runtime/VERSION"

if [ -d /usr/local/lib/hobot-code ]; then
  mv /usr/local/lib/hobot-code "$backup_dir/runtime-installed"
fi
mv "$new_runtime" /usr/local/lib/hobot-code

install -d -m 0750 /etc/hobot-code /etc/hobot-code/agent
install -d -m 0700 /var/lib/hobot-code /var/lib/hobot-code/sessions /var/lib/hobot-code/memory
install -d -m 0700 /var/lib/hobot-code/goals /var/lib/hobot-code/audit
install -d -m 0755 /etc/hobot-code/agent/bin
install -m 0755 "$package_dir/managed-bin/fd" /etc/hobot-code/agent/bin/fd
install -m 0755 "$package_dir/managed-bin/rg" /etc/hobot-code/agent/bin/rg
if [ ! -e /etc/hobot-code/agent/settings.json ]; then
  install -m 0640 "$package_dir/config/settings.json" /etc/hobot-code/agent/settings.json
fi
if [ ! -e /etc/hobot-code/agent/models.json ]; then
  install -m 0640 "$package_dir/config/models.json" /etc/hobot-code/agent/models.json
fi
if [ ! -e /etc/hobot-code/agent/permissions.json ]; then
  install -m 0640 "$package_dir/config/permissions.json" /etc/hobot-code/agent/permissions.json
fi
if [ ! -e /etc/hobot-code/agent/memory.json ]; then
  install -m 0640 "$package_dir/config/memory.json" /etc/hobot-code/agent/memory.json
fi
if [ ! -e /etc/hobot-code/agent/goals.json ]; then
  install -m 0640 "$package_dir/config/goals.json" /etc/hobot-code/agent/goals.json
fi
if [ ! -e /etc/hobot-code/agent/hooks.json ]; then
  install -m 0640 "$package_dir/config/hooks.json" /etc/hobot-code/agent/hooks.json
fi
if [ ! -e /etc/hobot-code/agent/notifications.json ]; then
  install -m 0640 "$package_dir/config/notifications.json" /etc/hobot-code/agent/notifications.json
fi
if [ ! -e /etc/hobot-code/agent/lsp.json ]; then
  install -m 0640 "$package_dir/config/lsp.json" /etc/hobot-code/agent/lsp.json
fi
if [ ! -e /etc/hobot-code/hobot.env ]; then
  install -m 0600 "$package_dir/config/hobot.env.example" /etc/hobot-code/hobot.env
fi

install -m 0755 "$package_dir/hobot-launcher" /usr/local/bin/hobot
install -m 0755 "$package_dir/rollback.sh" /usr/local/sbin/hobot-rollback

if [ -d "$backup_dir/runtime-installed" ]; then
  printf '%s\n' "$backup_dir" > /usr/local/lib/hobot-code/LAST_BACKUP
fi
/usr/local/bin/hobot --version
printf 'Installed Hobot Code %s. Run: hobot\n' "$version"
printf 'Rollback: hobot-rollback\n'
