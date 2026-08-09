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
install_user=${HOBOT_CODE_INSTALL_USER:-${SUDO_USER:-root}}
install_group=$(id -gn "$install_user")
if [ -n "${HOBOT_CODE_INSTALL_HOME:-}" ]; then
  install_home=$HOBOT_CODE_INSTALL_HOME
else
  install_home=$(getent passwd "$install_user" | cut -d: -f6)
fi
if [ -z "$install_home" ] || [ ! -d "$install_home" ]; then
  printf 'Cannot resolve home directory for %s\n' "$install_user" >&2
  exit 1
fi
config_root="$install_home/.config/hobot-code"
agent_dir="$config_root/agent"
state_root="$install_home/.local/state/hobot-code"
legacy_config=/etc/hobot-code
legacy_state=/var/lib/hobot-code

for user_path in "$config_root" "$state_root"; do
  if [ -L "$user_path" ]; then
    printf 'Refusing to install through symbolic link: %s\n' "$user_path" >&2
    exit 1
  fi
done

active_pids=$(pgrep -f '^/usr/local/lib/hobot-code/hobot' || true)
if [ -n "$active_pids" ]; then
  printf 'Stop active Hobot Code processes before upgrading: %s\n' "$active_pids" >&2
  exit 1
fi

copy_missing_tree() {
  source_tree=$1
  target_tree=$2
  (cd "$source_tree" && find . -mindepth 1 -type d -print) |
    while IFS= read -r relative_path; do
      mkdir -p "$target_tree/$relative_path"
    done
  (cd "$source_tree" && find . -mindepth 1 -type f -print) |
    while IFS= read -r relative_path; do
      if [ ! -e "$target_tree/$relative_path" ]; then
        cp -p "$source_tree/$relative_path" "$target_tree/$relative_path"
      fi
    done
}

install -d -m 0755 /usr/local/lib /usr/local/bin /usr/local/sbin /usr/local/lib/hobot-code-backups
install -d -m 0750 "$backup_dir"

if [ -e /usr/local/bin/hobot ]; then
  install -m 0755 /usr/local/bin/hobot "$backup_dir/hobot-command"
fi
install -d -m 0755 "$new_runtime" "$new_runtime/bin" "$new_runtime/default-config"
cp -R "$package_dir/runtime/." "$new_runtime/"
install -d -m 0755 "$new_runtime/extensions" "$new_runtime/skills" "$new_runtime/knowledge" "$new_runtime/prompts"
cp -R "$package_dir/extensions/." "$new_runtime/extensions/"
cp -R "$package_dir/skills/." "$new_runtime/skills/"
cp -R "$package_dir/knowledge/." "$new_runtime/knowledge/"
cp -R "$package_dir/prompts/." "$new_runtime/prompts/"
install -m 0644 "$package_dir/PI_RUNTIME" "$new_runtime/PI_RUNTIME"
install -m 0644 "$package_dir/VERSION" "$new_runtime/VERSION"
install -m 0755 "$package_dir/managed-bin/fd" "$new_runtime/bin/fd"
install -m 0755 "$package_dir/managed-bin/rg" "$new_runtime/bin/rg"
for config_name in settings.json models.json permissions.json memory.json goals.json hooks.json notifications.json lsp.json; do
  install -m 0644 "$package_dir/config/$config_name" "$new_runtime/default-config/$config_name"
done
install -m 0644 "$package_dir/config/hobot.env.example" "$new_runtime/default-config/hobot.env.example"

if [ -d "$legacy_config" ]; then
  cp -a "$legacy_config" "$backup_dir/legacy-etc-hobot-code"
fi
if [ -d "$legacy_state" ]; then
  cp -a "$legacy_state" "$backup_dir/legacy-var-hobot-code"
fi

install -d -m 0700 "$config_root" "$agent_dir" "$state_root" "$state_root/sessions" "$state_root/memory" "$state_root/goals" "$state_root/audit"

if [ -d "$legacy_config/agent" ]; then
  for config_name in settings.json models.json auth.json permissions.json memory.json goals.json hooks.json notifications.json lsp.json; do
    if [ -f "$legacy_config/agent/$config_name" ] && [ ! -e "$agent_dir/$config_name" ]; then
      install -m 0600 "$legacy_config/agent/$config_name" "$agent_dir/$config_name"
    fi
  done
fi
if [ -f "$legacy_config/hobot.env" ] && [ ! -e "$config_root/hobot.env" ]; then
  install -m 0600 "$legacy_config/hobot.env" "$config_root/hobot.env"
fi

if [ -f "$config_root/hobot.env" ]; then
  env_migration="$config_root/hobot.env.migrate.$$"
  sed \
    -e '\|^HOBOT_CODING_AGENT_DIR=/etc/hobot-code/agent$|d' \
    -e '\|^HOBOT_CODING_AGENT_SESSION_DIR=/var/lib/hobot-code/sessions$|d' \
    "$config_root/hobot.env" > "$env_migration"
  chmod 0600 "$env_migration"
  mv "$env_migration" "$config_root/hobot.env"
fi

for config_name in settings.json models.json permissions.json memory.json goals.json hooks.json notifications.json lsp.json; do
  if [ ! -e "$agent_dir/$config_name" ]; then
    install -m 0600 "$package_dir/config/$config_name" "$agent_dir/$config_name"
  fi
done
if [ ! -e "$config_root/hobot.env" ]; then
  install -m 0600 "$package_dir/config/hobot.env.example" "$config_root/hobot.env"
fi

if [ ! -e "$state_root/.system-layout-migrated" ]; then
  for state_name in memory goals audit; do
    if [ -d "$legacy_state/$state_name" ]; then
      copy_missing_tree "$legacy_state/$state_name" "$state_root/$state_name"
    fi
  done
  : > "$state_root/.system-layout-migrated"
fi

chown -R "$install_user:$install_group" "$config_root" "$state_root"
find "$config_root" -type d -exec chmod 0700 {} \;
find "$state_root" -type d -exec chmod 0700 {} \;
find "$config_root" -type f -exec chmod 0600 {} \;
find "$state_root" -type f -exec chmod 0600 {} \;

if [ -d /usr/local/lib/hobot-code ]; then
  mv /usr/local/lib/hobot-code "$backup_dir/runtime-installed"
fi
mv "$new_runtime" /usr/local/lib/hobot-code

install -m 0755 "$package_dir/hobot-launcher" /usr/local/bin/hobot
install -m 0755 "$package_dir/rollback.sh" /usr/local/sbin/hobot-rollback

rm -rf "$legacy_config" "$legacy_state"

if [ -d "$backup_dir/runtime-installed" ]; then
  printf '%s\n' "$backup_dir" > /usr/local/lib/hobot-code/LAST_BACKUP
fi
/usr/local/bin/hobot --version
printf 'Installed Hobot Code %s. Run: hobot\n' "$version"
printf 'User config: %s\n' "$config_root"
printf 'User state: %s\n' "$state_root"
printf 'Rollback: hobot-rollback\n'
