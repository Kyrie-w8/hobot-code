#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'install.sh must run as root\n' >&2
  exit 1
fi

package_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
version=$(cat "$package_dir/VERSION")
install_channel=${HOBOT_CODE_INSTALL_CHANNEL:-stable}
backup_keep=${HOBOT_CODE_BACKUP_KEEP:-3}
backup_max_mib=${HOBOT_CODE_BACKUP_MAX_MIB:-768}
case "$install_channel" in
  stable) ;;
  *)
    printf 'Unsupported Hobot Code release channel: %s\n' "$install_channel" >&2
    exit 1
    ;;
esac
case "$backup_keep" in ''|*[!0-9]*) printf 'HOBOT_CODE_BACKUP_KEEP must be an integer.\n' >&2; exit 1 ;; esac
case "$backup_max_mib" in ''|*[!0-9]*) printf 'HOBOT_CODE_BACKUP_MAX_MIB must be an integer.\n' >&2; exit 1 ;; esac
if [ "$backup_keep" -lt 1 ] || [ "$backup_keep" -gt 20 ]; then
  printf 'HOBOT_CODE_BACKUP_KEEP must be between 1 and 20.\n' >&2
  exit 1
fi
if [ "$backup_max_mib" -lt 128 ] || [ "$backup_max_mib" -gt 8192 ]; then
  printf 'HOBOT_CODE_BACKUP_MAX_MIB must be between 128 and 8192.\n' >&2
  exit 1
fi
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_dir=
new_runtime=
lock_dir=/usr/local/lib/hobot-code.install.lock
new_launcher=
new_rollback=
transaction_complete=0
runtime_swapped=0
had_previous_runtime=0
launcher_replaced=0
rollback_replaced=0
user_paths_touched=0
config_root_existed=0
migration_marker_existed=0
legacy_sessions_marker_existed=0
manifest_actual_list=
manifest_expected_list=
migration_list=
install_user=${HOBOT_CODE_INSTALL_USER:-${SUDO_USER:-root}}
install_group=$(id -gn "$install_user")
install_uid=$(id -u "$install_user")
if [ -n "${HOBOT_CODE_INSTALL_HOME:-}" ]; then
  install_home=$HOBOT_CODE_INSTALL_HOME
else
  install_home=$(getent passwd "$install_user" | cut -d: -f6)
fi
case "$install_home" in
  /*) ;;
  *)
    printf 'Install home must be an absolute path: %s\n' "$install_home" >&2
    exit 1
    ;;
esac
if [ "$install_home" = / ] || [ -L "$install_home" ] || [ ! -d "$install_home" ]; then
  printf 'Cannot resolve home directory for %s\n' "$install_user" >&2
  exit 1
fi
install_home_logical=$(CDPATH= cd -L -- "$install_home" && pwd -L)
install_home_physical=$(CDPATH= cd -P -- "$install_home" && pwd -P)
if [ "$install_home_logical" != "$install_home_physical" ]; then
  printf 'Install home must not traverse symbolic links: %s\n' "$install_home" >&2
  exit 1
fi
install_home=$install_home_physical
install_home_owner=$(stat -c %u "$install_home")
if [ "$install_home_owner" != "$install_uid" ]; then
  printf 'Install home %s is owned by uid %s, expected %s for %s\n' "$install_home" "$install_home_owner" "$install_uid" "$install_user" >&2
  exit 1
fi
config_root="$install_home/.config/hobot-code"
agent_dir="$config_root/agent"
state_root="$install_home/.local/state/hobot-code"
legacy_config=/etc/hobot-code
legacy_state=/var/lib/hobot-code
if [ -d "$config_root" ]; then
  config_root_existed=1
fi
if [ -e "$state_root/.system-layout-migrated" ]; then
  migration_marker_existed=1
fi
if [ -e "$state_root/.legacy-sessions-archived" ]; then
  legacy_sessions_marker_existed=1
fi

acquire_install_lock() {
  if mkdir "$lock_dir" 2>/dev/null; then
    printf '%s\n' "$$" > "$lock_dir/pid"
    return
  fi
  lock_pid=$(sed -n '1p' "$lock_dir/pid" 2>/dev/null || true)
  case "$lock_pid" in
    ''|*[!0-9]*) ;;
    *)
      if ! kill -0 "$lock_pid" 2>/dev/null; then
        rm -rf "$lock_dir"
        if mkdir "$lock_dir" 2>/dev/null; then
          printf '%s\n' "$$" > "$lock_dir/pid"
          return
        fi
      fi
      ;;
  esac
  printf 'Another Hobot Code install or rollback is already running: %s\n' "$lock_dir" >&2
  exit 1
}
acquire_install_lock

finish_install() {
  status=$?
  trap - EXIT HUP INT TERM
  set +e
  if [ -n "$new_runtime" ]; then rm -rf "$new_runtime"; fi
  if [ -n "$new_launcher" ]; then rm -f "$new_launcher"; fi
  if [ -n "$new_rollback" ]; then rm -f "$new_rollback"; fi
  if [ -n "$manifest_actual_list" ]; then rm -f "$manifest_actual_list"; fi
  if [ -n "$manifest_expected_list" ]; then rm -f "$manifest_expected_list"; fi
  if [ -n "$migration_list" ]; then rm -f "$migration_list"; fi
  if [ "$status" -ne 0 ] && [ "$transaction_complete" -ne 1 ]; then
    printf 'Installation failed; restoring the previous Hobot Code runtime.\n' >&2
    if [ "$runtime_swapped" -eq 1 ] && [ -n "$backup_dir" ]; then
      if [ -d "$backup_dir/runtime-installed" ]; then
        if [ -d /usr/local/lib/hobot-code ]; then
          mv /usr/local/lib/hobot-code "/usr/local/lib/hobot-code-failed-$timestamp-$$"
        fi
        mv "$backup_dir/runtime-installed" /usr/local/lib/hobot-code
      elif [ "$had_previous_runtime" -eq 0 ] && [ -d /usr/local/lib/hobot-code ]; then
        mv /usr/local/lib/hobot-code "/usr/local/lib/hobot-code-failed-$timestamp-$$"
      fi
    fi
    if [ "$launcher_replaced" -eq 1 ]; then
      if [ -f "$backup_dir/hobot-command" ]; then
        install -m 0755 "$backup_dir/hobot-command" /usr/local/bin/hobot
      else
        rm -f /usr/local/bin/hobot
      fi
    fi
    if [ "$rollback_replaced" -eq 1 ]; then
      if [ -f "$backup_dir/hobot-rollback-command" ]; then
        install -m 0755 "$backup_dir/hobot-rollback-command" /usr/local/sbin/hobot-rollback
      else
        rm -f /usr/local/sbin/hobot-rollback
      fi
    fi
    if [ "$user_paths_touched" -eq 1 ]; then
      if [ -d "$backup_dir/user-config-before-install" ]; then
        rm -rf "$config_root"
        cp -a "$backup_dir/user-config-before-install" "$config_root"
      elif [ "$config_root_existed" -eq 0 ]; then
        rm -rf "$config_root"
      fi
      if [ "$migration_marker_existed" -eq 0 ]; then
        rm -f "$state_root/.system-layout-migrated"
      fi
      if [ "$legacy_sessions_marker_existed" -eq 0 ]; then
        rm -f "$state_root/.legacy-sessions-archived"
      fi
    fi
    if [ -n "$backup_dir" ] && [ ! -d "$backup_dir/runtime-installed" ]; then
      rm -rf "$backup_dir"
    fi
  fi
  rm -f "$lock_dir/pid"
  rmdir "$lock_dir" 2>/dev/null || true
  exit "$status"
}
trap finish_install EXIT
trap 'exit 130' HUP INT TERM

if [ ! -f "$package_dir/MANIFEST.sha256" ]; then
  printf 'Release package is missing MANIFEST.sha256\n' >&2
  exit 1
fi
package_link=$(find "$package_dir" -type l -print -quit) || exit 1
if [ -n "$package_link" ]; then
  printf 'Release package must not contain symbolic links: %s\n' "$package_link" >&2
  exit 1
fi
package_special=$(find "$package_dir" ! -type d ! -type f -print -quit) || exit 1
if [ -n "$package_special" ]; then
  printf 'Release package contains an unsupported filesystem entry: %s\n' "$package_special" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$package_dir" && sha256sum -c MANIFEST.sha256 >/dev/null)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$package_dir" && shasum -a 256 -c MANIFEST.sha256 >/dev/null)
else
  printf 'sha256sum or shasum is required to verify the release package\n' >&2
  exit 1
fi
manifest_actual_list=$(mktemp "${TMPDIR:-/tmp}/hobot-manifest-actual.XXXXXX")
manifest_expected_list=$(mktemp "${TMPDIR:-/tmp}/hobot-manifest-expected.XXXXXX")
(cd "$package_dir" && find . -type f ! -path './MANIFEST.sha256' -print | sed 's|^\./||' | LC_ALL=C sort) > "$manifest_actual_list"
awk '{ print substr($0, 67) }' "$package_dir/MANIFEST.sha256" | LC_ALL=C sort > "$manifest_expected_list"
if ! cmp -s "$manifest_actual_list" "$manifest_expected_list"; then
  printf 'Release package files do not match MANIFEST.sha256\n' >&2
  exit 1
fi

refuse_symlink() {
  inspected_path=$1
  if [ -L "$inspected_path" ]; then
    printf 'Refusing to install through symbolic link: %s\n' "$inspected_path" >&2
    exit 1
  fi
}

refuse_tree_symlinks() {
  inspected_tree=$1
  if [ ! -d "$inspected_tree" ]; then return; fi
  first_link=$(find "$inspected_tree" -type l -print -quit) || exit 1
  if [ -n "$first_link" ]; then
    printf 'Refusing to migrate into a tree containing symbolic links: %s\n' "$first_link" >&2
    exit 1
  fi
}

refuse_tree_specials() {
  inspected_tree=$1
  if [ ! -d "$inspected_tree" ]; then return; fi
  first_special=$(find "$inspected_tree" ! -type d ! -type f -print -quit) || exit 1
  if [ -n "$first_special" ]; then
    printf 'Refusing a tree containing an unsupported filesystem entry: %s\n' "$first_special" >&2
    exit 1
  fi
}

for system_path in /usr/local/lib/hobot-code /usr/local/lib/hobot-code-backups /usr/local/bin/hobot /usr/local/sbin/hobot-rollback; do
  refuse_symlink "$system_path"
done
for system_directory in /usr/local/lib/hobot-code /usr/local/lib/hobot-code-backups; do
  if [ -e "$system_directory" ] && [ ! -d "$system_directory" ]; then
    printf 'Expected a managed system directory or an absent path: %s\n' "$system_directory" >&2
    exit 1
  fi
done
for system_command in /usr/local/bin/hobot /usr/local/sbin/hobot-rollback; do
  if [ -e "$system_command" ] && [ ! -f "$system_command" ]; then
    printf 'Expected a managed command file or an absent path: %s\n' "$system_command" >&2
    exit 1
  fi
done
for root_owned_path in /usr/local/lib/hobot-code /usr/local/lib/hobot-code-backups /usr/local/bin/hobot /usr/local/sbin/hobot-rollback; do
  if [ -e "$root_owned_path" ] && [ "$(stat -c %u "$root_owned_path")" -ne 0 ]; then
    printf 'Managed system path must be owned by root: %s\n' "$root_owned_path" >&2
    exit 1
  fi
done
refuse_tree_symlinks /usr/local/lib/hobot-code
refuse_tree_specials /usr/local/lib/hobot-code

for user_path in \
  "$install_home/.config" "$config_root" "$agent_dir" "$config_root/hobot.env" \
  "$install_home/.local" "$install_home/.local/state" "$state_root" \
  "$state_root/sessions" "$state_root/memory" "$state_root/goals" "$state_root/audit" "$state_root/legacy-sessions" \
  "$state_root/.system-layout-migrated" "$state_root/.legacy-sessions-archived"; do
  refuse_symlink "$user_path"
done
for legacy_path in "$legacy_config" "$legacy_config/agent" "$legacy_state"; do
  refuse_symlink "$legacy_path"
done
for config_name in settings.json models.json auth.json permissions.json memory.json goals.json hooks.json notifications.json lsp.json; do
  refuse_symlink "$agent_dir/$config_name"
done
refuse_tree_symlinks "$config_root"
for state_name in memory goals audit legacy-sessions; do
  refuse_tree_symlinks "$state_root/$state_name"
done
refuse_tree_symlinks "$legacy_config"
refuse_tree_symlinks "$legacy_state"
refuse_tree_specials "$config_root"
for state_name in memory goals audit legacy-sessions; do
  refuse_tree_specials "$state_root/$state_name"
done
refuse_tree_specials "$legacy_config"
refuse_tree_specials "$legacy_state"

for managed_directory in \
  "$install_home/.config" "$config_root" "$agent_dir" \
  "$install_home/.local" "$install_home/.local/state" "$state_root" \
  "$state_root/sessions" "$state_root/memory" "$state_root/goals" "$state_root/audit" "$state_root/legacy-sessions" \
  "$legacy_config" "$legacy_config/agent" "$legacy_state"; do
  if [ -e "$managed_directory" ] && [ ! -d "$managed_directory" ]; then
    printf 'Expected a directory or an absent path: %s\n' "$managed_directory" >&2
    exit 1
  fi
done
for managed_file in "$config_root/hobot.env" "$state_root/.system-layout-migrated" "$state_root/.legacy-sessions-archived"; do
  if [ -e "$managed_file" ] && [ ! -f "$managed_file" ]; then
    printf 'Expected a regular file or an absent path: %s\n' "$managed_file" >&2
    exit 1
  fi
done
for config_name in settings.json models.json auth.json permissions.json memory.json goals.json hooks.json notifications.json lsp.json; do
  if [ -e "$agent_dir/$config_name" ] && [ ! -f "$agent_dir/$config_name" ]; then
    printf 'Expected a regular configuration file: %s\n' "$agent_dir/$config_name" >&2
    exit 1
  fi
done

active_hobot_pids() {
  detected_pids=
  for process_path in /proc/[0-9]*; do
    [ -r "$process_path/exe" ] || continue
    executable=$(readlink "$process_path/exe" 2>/dev/null || true)
    executable=${executable% (deleted)}
    case "$executable" in
      /usr/local/lib/hobot-code/hobot|/usr/local/lib/hobot-code/agentd)
        process_id=${process_path##*/}
        detected_pids="${detected_pids}${detected_pids:+ }$process_id"
        ;;
    esac
  done
  printf '%s\n' "$detected_pids"
}

active_pids=$(active_hobot_pids)
if [ -n "$active_pids" ]; then
  printf 'Stop active Hobot Code processes before upgrading: %s\n' "$active_pids" >&2
  exit 1
fi

copy_missing_tree() {
  source_tree=$1
  target_tree=$2
  migration_list=$(mktemp "${TMPDIR:-/tmp}/hobot-migration-list.XXXXXX")
  (cd "$source_tree" && find . -mindepth 1 -type d -print) > "$migration_list"
  while IFS= read -r relative_path; do
    install -d -m 0700 "$target_tree/$relative_path"
    chown "$install_user:$install_group" "$target_tree/$relative_path"
  done < "$migration_list"
  (cd "$source_tree" && find . -mindepth 1 -type f -print) > "$migration_list"
  while IFS= read -r relative_path; do
    if [ ! -e "$target_tree/$relative_path" ]; then
      cp -p "$source_tree/$relative_path" "$target_tree/$relative_path"
      chmod 0600 "$target_tree/$relative_path"
      chown "$install_user:$install_group" "$target_tree/$relative_path"
    fi
  done < "$migration_list"
  rm -f "$migration_list"
  migration_list=
}

prune_install_backups() {
  protected_backup=$1
  backup_root=/usr/local/lib/hobot-code-backups
  backup_limit_kib=$((backup_max_mib * 1024))
  backup_list=$(mktemp "${TMPDIR:-/tmp}/hobot-backups.XXXXXX")
  find "$backup_root" -mindepth 1 -maxdepth 1 -type d -print | LC_ALL=C sort -r > "$backup_list"
  backup_index=0
  backup_total_kib=0
  while IFS= read -r candidate; do
    candidate_kib=$(du -sk "$candidate" 2>/dev/null | awk '{print $1}')
    case "$candidate_kib" in ''|*[!0-9]*) candidate_kib=0 ;; esac
    backup_index=$((backup_index + 1))
    if [ "$candidate" = "$protected_backup" ]; then
      backup_total_kib=$((backup_total_kib + candidate_kib))
      continue
    fi
    if [ "$backup_index" -le "$backup_keep" ] && [ $((backup_total_kib + candidate_kib)) -le "$backup_limit_kib" ]; then
      backup_total_kib=$((backup_total_kib + candidate_kib))
      continue
    fi
    if [ -L "$candidate" ]; then
      printf 'Warning: refusing to prune symbolic-link backup: %s\n' "$candidate" >&2
      continue
    fi
    rm -rf "$candidate" || printf 'Warning: could not prune old Hobot Code backup: %s\n' "$candidate" >&2
  done < "$backup_list"
  rm -f "$backup_list"
  return 0
}

install -d -m 0755 /usr/local/lib /usr/local/bin /usr/local/sbin /usr/local/lib/hobot-code-backups

required_kib=$(du -sk "$package_dir" 2>/dev/null | awk '{print $1}')
for backup_source in /usr/local/lib/hobot-code "$legacy_config" "$legacy_state" "$config_root"; do
  if [ -e "$backup_source" ]; then
    source_kib=$(du -sk "$backup_source" 2>/dev/null | awk '{print $1}')
    required_kib=$((required_kib + source_kib))
  fi
done
available_kib=$(df -Pk /usr/local/lib | awk 'NR == 2 { print $4 }')
required_kib=$((required_kib + 65536))
home_required_kib=16384
for migration_source in "$legacy_config" "$legacy_state"; do
  if [ -e "$migration_source" ]; then
    source_kib=$(du -sk "$migration_source" 2>/dev/null | awk '{print $1}')
    home_required_kib=$((home_required_kib + source_kib))
  fi
done
home_available_kib=$(df -Pk "$install_home" | awk 'NR == 2 { print $4 }')
local_device=$(stat -c %d /usr/local/lib)
home_device=$(stat -c %d "$install_home")
if [ "$local_device" = "$home_device" ]; then
  combined_required_kib=$((required_kib + home_required_kib))
  if [ -z "$available_kib" ] || [ "$available_kib" -lt "$combined_required_kib" ]; then
    printf 'Insufficient shared filesystem space: need at least %s KiB, available %s KiB\n' "$combined_required_kib" "${available_kib:-unknown}" >&2
    exit 1
  fi
elif [ -z "$available_kib" ] || [ "$available_kib" -lt "$required_kib" ]; then
  printf 'Insufficient free space: need at least %s KiB, available %s KiB\n' "$required_kib" "${available_kib:-unknown}" >&2
  exit 1
elif [ -z "$home_available_kib" ] || [ "$home_available_kib" -lt "$home_required_kib" ]; then
  printf 'Insufficient user-home space: need at least %s KiB, available %s KiB\n' "$home_required_kib" "${home_available_kib:-unknown}" >&2
  exit 1
fi

backup_dir=$(mktemp -d "/usr/local/lib/hobot-code-backups/$timestamp.XXXXXX")
chmod 0750 "$backup_dir"
new_runtime=$(mktemp -d /usr/local/lib/hobot-code.new.XXXXXX)
chmod 0755 "$new_runtime"
new_launcher=$(mktemp /usr/local/bin/hobot.new.XXXXXX)
new_rollback=$(mktemp /usr/local/sbin/hobot-rollback.new.XXXXXX)

if [ -e /usr/local/bin/hobot ]; then
  install -m 0755 /usr/local/bin/hobot "$backup_dir/hobot-command"
fi
if [ -e /usr/local/sbin/hobot-rollback ]; then
  install -m 0755 /usr/local/sbin/hobot-rollback "$backup_dir/hobot-rollback-command"
fi
if [ -d "$config_root" ]; then
  cp -a "$config_root" "$backup_dir/user-config-before-install"
fi
install -d -m 0755 "$new_runtime/bin" "$new_runtime/default-config" "$new_runtime/licenses"
cp -R "$package_dir/runtime/." "$new_runtime/"
install -m 0755 "$package_dir/agentd" "$new_runtime/agentd"
install -m 0755 "$package_dir/release.sh" "$new_runtime/release.sh"
install -m 0755 "$package_dir/uninstall.sh" "$new_runtime/uninstall.sh"
install -d -m 0755 "$new_runtime/docs" "$new_runtime/extensions" "$new_runtime/skills" "$new_runtime/knowledge" "$new_runtime/prompts"
cp -R "$package_dir/docs/." "$new_runtime/docs/"
cp -R "$package_dir/extensions/." "$new_runtime/extensions/"
cp -R "$package_dir/skills/." "$new_runtime/skills/"
cp -R "$package_dir/knowledge/." "$new_runtime/knowledge/"
cp -R "$package_dir/prompts/." "$new_runtime/prompts/"
install -m 0644 "$package_dir/PI_RUNTIME" "$new_runtime/PI_RUNTIME"
install -m 0644 "$package_dir/TOOLS_RUNTIME" "$new_runtime/TOOLS_RUNTIME"
install -m 0644 "$package_dir/BUILD_INFO.json" "$new_runtime/BUILD_INFO.json"
install -m 0644 "$package_dir/VERSION" "$new_runtime/VERSION"
printf '%s\n' "$install_channel" > "$new_runtime/CHANNEL"
chmod 0644 "$new_runtime/CHANNEL"
cp -R "$package_dir/licenses/." "$new_runtime/licenses/"
install -m 0755 "$package_dir/managed-bin/fd" "$new_runtime/bin/fd"
install -m 0755 "$package_dir/managed-bin/rg" "$new_runtime/bin/rg"
for config_name in settings.json models.json permissions.json memory.json goals.json hooks.json notifications.json lsp.json; do
  install -m 0644 "$package_dir/config/$config_name" "$new_runtime/default-config/$config_name"
done
install -m 0644 "$package_dir/config/hobot.env.example" "$new_runtime/default-config/hobot.env.example"
install -m 0644 "$package_dir/config/tmux.conf" "$new_runtime/tmux.conf"
runtime_version=$("$new_runtime/hobot" --version)
agentd_version=$("$new_runtime/agentd" version)
if [ "$runtime_version" != "$version" ] || [ "$agentd_version" != "$version" ]; then
  printf 'Installed component version mismatch: package=%s runtime=%s agentd=%s\n' \
    "$version" "$runtime_version" "$agentd_version" >&2
  exit 1
fi

if [ -d "$legacy_config" ]; then
  cp -a "$legacy_config" "$backup_dir/legacy-etc-hobot-code"
fi
if [ -d "$legacy_state" ]; then
  cp -a "$legacy_state" "$backup_dir/legacy-var-hobot-code"
fi

user_paths_touched=1
install -d -m 0700 "$config_root" "$agent_dir" "$state_root" "$state_root/sessions" "$state_root/memory" "$state_root/goals" "$state_root/audit"
chown "$install_user:$install_group" "$config_root" "$agent_dir" "$state_root" "$state_root/sessions" "$state_root/memory" "$state_root/goals" "$state_root/audit"

if [ -d "$legacy_config/agent" ]; then
  for config_name in settings.json models.json auth.json permissions.json memory.json goals.json hooks.json notifications.json lsp.json; do
    if [ -f "$legacy_config/agent/$config_name" ] && [ ! -e "$agent_dir/$config_name" ]; then
      install -m 0600 "$legacy_config/agent/$config_name" "$agent_dir/$config_name"
      chown "$install_user:$install_group" "$agent_dir/$config_name"
    fi
  done
fi
if [ -f "$legacy_config/hobot.env" ] && [ ! -e "$config_root/hobot.env" ]; then
  install -m 0600 "$legacy_config/hobot.env" "$config_root/hobot.env"
  chown "$install_user:$install_group" "$config_root/hobot.env"
fi

if [ -f "$config_root/hobot.env" ]; then
  env_migration=$(mktemp "$config_root/.hobot.env.migrate.XXXXXX")
  sed \
    -e '\|^HOBOT_CODING_AGENT_DIR=/etc/hobot-code/agent$|d' \
    -e '\|^HOBOT_CODING_AGENT_SESSION_DIR=/var/lib/hobot-code/sessions$|d' \
    "$config_root/hobot.env" > "$env_migration"
  chmod 0600 "$env_migration"
  chown "$install_user:$install_group" "$env_migration"
  mv "$env_migration" "$config_root/hobot.env"
fi

for config_name in settings.json models.json permissions.json memory.json goals.json hooks.json notifications.json lsp.json; do
  if [ ! -e "$agent_dir/$config_name" ]; then
    install -m 0600 "$package_dir/config/$config_name" "$agent_dir/$config_name"
  fi
  chmod 0600 "$agent_dir/$config_name"
  chown "$install_user:$install_group" "$agent_dir/$config_name"
done
if [ ! -e "$config_root/hobot.env" ]; then
  install -m 0600 "$package_dir/config/hobot.env.example" "$config_root/hobot.env"
fi
chmod 0600 "$config_root/hobot.env"
chown "$install_user:$install_group" "$config_root/hobot.env"
if [ -e "$agent_dir/auth.json" ]; then
  chmod 0600 "$agent_dir/auth.json"
  chown "$install_user:$install_group" "$agent_dir/auth.json"
fi

if [ ! -e "$state_root/.system-layout-migrated" ]; then
  for state_name in memory goals audit; do
    if [ -d "$legacy_state/$state_name" ]; then
      copy_missing_tree "$legacy_state/$state_name" "$state_root/$state_name"
    fi
  done
  : > "$state_root/.system-layout-migrated"
  chmod 0600 "$state_root/.system-layout-migrated"
  chown "$install_user:$install_group" "$state_root/.system-layout-migrated"
fi

if [ ! -e "$state_root/.legacy-sessions-archived" ]; then
  if [ -d "$legacy_state/sessions" ]; then
    install -d -m 0700 "$state_root/legacy-sessions"
    chown "$install_user:$install_group" "$state_root/legacy-sessions"
    copy_missing_tree "$legacy_state/sessions" "$state_root/legacy-sessions"
  fi
  : > "$state_root/.legacy-sessions-archived"
  chmod 0600 "$state_root/.legacy-sessions-archived"
  chown "$install_user:$install_group" "$state_root/.legacy-sessions-archived"
fi

if [ -d /usr/local/lib/hobot-code ]; then
  had_previous_runtime=1
  runtime_swapped=1
  mv /usr/local/lib/hobot-code "$backup_dir/runtime-installed"
else
  runtime_swapped=1
fi
mv "$new_runtime" /usr/local/lib/hobot-code

install -m 0755 "$package_dir/hobot-launcher" "$new_launcher"
launcher_replaced=1
mv "$new_launcher" /usr/local/bin/hobot
install -m 0755 "$package_dir/rollback.sh" "$new_rollback"
rollback_replaced=1
mv "$new_rollback" /usr/local/sbin/hobot-rollback

if [ ! -f /usr/local/bin/hobot ] || [ ! -x /usr/local/bin/hobot ] ||
   [ ! -f /usr/local/sbin/hobot-rollback ] || [ ! -x /usr/local/sbin/hobot-rollback ]; then
  printf 'Installed command validation failed\n' >&2
  exit 1
fi

if [ -d "$backup_dir/runtime-installed" ]; then
  printf '%s\n' "$backup_dir" > /usr/local/lib/hobot-code/LAST_BACKUP
fi
/usr/local/lib/hobot-code/hobot --version
transaction_complete=1
prune_install_backups "$backup_dir"
rm -rf "$legacy_config" "$legacy_state" || printf 'Warning: legacy directories could not be removed after a successful install.\n' >&2
if awk '
  index($0, "ANTHROPIC_AUTH_TOKEN=") == 1 { value=substr($0, 22) }
  END {
    gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
    exit value != "" && value != "\"\"" && value != "\047\047" ? 0 : 1
  }
' "$config_root/hobot.env"; then
  printf 'Installed Hobot Code %s. Run: hobot\n' "$version"
else
  printf 'Installed Hobot Code %s. Configure your model first: hobot setup\n' "$version"
fi
printf 'User config: %s\n' "$config_root"
printf 'User state: %s\n' "$state_root"
printf 'Rollback: hobot-rollback\n'
