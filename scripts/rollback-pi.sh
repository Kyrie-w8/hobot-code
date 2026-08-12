#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  printf 'hobot-rollback must run as root\n' >&2
  exit 1
fi
if [ "$#" -gt 1 ]; then
  printf 'Usage: hobot-rollback [backup-directory]\n' >&2
  exit 2
fi

backup_root=/usr/local/lib/hobot-code-backups
lock_dir=/usr/local/lib/hobot-code.install.lock
requested_backup=${1:-}
rollback_complete=0
runtime_swapped=0
had_previous_runtime=0
launcher_swapped=0
rollback_command_swapped=0
legacy_config_restored=0
legacy_state_restored=0
staged_runtime=
staged_launcher=
staged_rollback=
previous_runtime=
previous_launcher=
previous_rollback=
last_restored_new=
last_restored_existed=0
last_restored_write_started=0
restored_marker=
restored_marker_existed=0
restored_marker_new=

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

cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  set +e
  if [ "$status" -ne 0 ] && [ "$rollback_complete" -ne 1 ] && [ "$runtime_swapped" -eq 1 ]; then
    printf 'Rollback failed; restoring the runtime that was active before rollback.\n' >&2
    if [ "$had_previous_runtime" -eq 1 ] && [ -n "$previous_runtime" ] && [ -d "$previous_runtime" ]; then
      if [ -d /usr/local/lib/hobot-code ]; then
        mv /usr/local/lib/hobot-code "/usr/local/lib/hobot-code-failed-rollback-$$"
      fi
      mv "$previous_runtime" /usr/local/lib/hobot-code
    elif [ "$had_previous_runtime" -eq 0 ] && [ -d /usr/local/lib/hobot-code ]; then
      mv /usr/local/lib/hobot-code "/usr/local/lib/hobot-code-failed-rollback-$$"
    fi
    if [ "$launcher_swapped" -eq 1 ]; then
      if [ -n "$previous_launcher" ] && [ -f "$previous_launcher" ]; then
        mv "$previous_launcher" /usr/local/bin/hobot
      else
        rm -f /usr/local/bin/hobot
      fi
    fi
    if [ "$rollback_command_swapped" -eq 1 ]; then
      if [ -n "$previous_rollback" ] && [ -f "$previous_rollback" ]; then
        mv "$previous_rollback" /usr/local/sbin/hobot-rollback
      else
        rm -f /usr/local/sbin/hobot-rollback
      fi
    fi
    if [ "$legacy_config_restored" -eq 1 ]; then rm -rf /etc/hobot-code; fi
    if [ "$legacy_state_restored" -eq 1 ]; then rm -rf /var/lib/hobot-code; fi
  fi
  if [ -n "$staged_runtime" ]; then rm -rf "$staged_runtime"; fi
  if [ -n "$staged_launcher" ]; then rm -f "$staged_launcher"; fi
  if [ -n "$staged_rollback" ]; then rm -f "$staged_rollback"; fi
  if [ -n "$previous_launcher" ]; then rm -f "$previous_launcher"; fi
  if [ -n "$previous_rollback" ]; then rm -f "$previous_rollback"; fi
  if [ -n "$last_restored_new" ]; then rm -f "$last_restored_new"; fi
  if [ -n "$restored_marker_new" ]; then rm -f "$restored_marker_new"; fi
  if [ "$status" -ne 0 ] && [ "$rollback_complete" -ne 1 ]; then
    if [ -n "$restored_marker" ] && [ "$restored_marker_existed" -eq 0 ]; then
      rm -f "$restored_marker"
    fi
    if [ "$last_restored_write_started" -eq 1 ]; then
      if [ "$last_restored_existed" -eq 1 ]; then
        last_restored_restore="$backup_root/.last-restored.restore.$$"
        printf '%s\n' "$last_restored" > "$last_restored_restore"
        chmod 0600 "$last_restored_restore"
        mv "$last_restored_restore" "$backup_root/.last-restored"
      else
        rm -f "$backup_root/.last-restored"
      fi
    fi
  fi
  rm -f "$lock_dir/pid"
  rmdir "$lock_dir" 2>/dev/null || true
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

refuse_rollback_symlink() {
  inspected_path=$1
  if [ -L "$inspected_path" ]; then
    printf 'Refusing rollback through symbolic link: %s\n' "$inspected_path" >&2
    exit 1
  fi
}

for managed_path in "$backup_root" /usr/local/lib/hobot-code /usr/local/bin/hobot /usr/local/sbin/hobot-rollback; do
  refuse_rollback_symlink "$managed_path"
done
if [ -e /usr/local/lib/hobot-code ] && [ ! -d /usr/local/lib/hobot-code ]; then
  printf 'Expected the current runtime to be a directory: /usr/local/lib/hobot-code\n' >&2
  exit 1
fi
for managed_command in /usr/local/bin/hobot /usr/local/sbin/hobot-rollback; do
  if [ -e "$managed_command" ] && [ ! -f "$managed_command" ]; then
    printf 'Expected a managed command file or an absent path: %s\n' "$managed_command" >&2
    exit 1
  fi
done
for root_owned_path in "$backup_root" /usr/local/lib/hobot-code /usr/local/bin/hobot /usr/local/sbin/hobot-rollback; do
  if [ -e "$root_owned_path" ] && [ "$(stat -c %u "$root_owned_path")" -ne 0 ]; then
    printf 'Managed rollback path must be owned by root: %s\n' "$root_owned_path" >&2
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
  printf 'Stop active Hobot Code processes before rollback: %s\n' "$active_pids" >&2
  exit 1
fi
if [ ! -d "$backup_root" ]; then
  printf 'No Hobot Code backup directory exists: %s\n' "$backup_root" >&2
  exit 1
fi
backup_root=$(CDPATH= cd -- "$backup_root" && pwd -P)

if [ -L "$backup_root/.last-restored" ] || { [ -e "$backup_root/.last-restored" ] && [ ! -f "$backup_root/.last-restored" ]; }; then
  printf 'Rollback state must be a regular file or an absent path: %s\n' "$backup_root/.last-restored" >&2
  exit 1
fi
if [ -f "$backup_root/.last-restored" ]; then
  last_restored_existed=1
  if [ "$(stat -c %u "$backup_root/.last-restored")" -ne 0 ]; then
    printf 'Rollback state must be owned by root: %s\n' "$backup_root/.last-restored" >&2
    exit 1
  fi
fi
last_restored=$(sed -n '1p' "$backup_root/.last-restored" 2>/dev/null || true)

is_unused_backup() {
  candidate_path=$1
  case "$candidate_path" in
    "$backup_root"/*) ;;
    *) return 1 ;;
  esac
  [ "$candidate_path" != "$last_restored" ] &&
    [ ! -e "$candidate_path/.hobot-restored" ] &&
    [ ! -L "$candidate_path/.hobot-restored" ] &&
    [ -d "$candidate_path/runtime-installed" ] &&
    [ -f "$candidate_path/hobot-command" ]
}

backup_dir=$requested_backup
if [ -z "$backup_dir" ] && [ -r /usr/local/lib/hobot-code/LAST_BACKUP ]; then
  candidate=$(sed -n '1p' /usr/local/lib/hobot-code/LAST_BACKUP)
  if is_unused_backup "$candidate"; then
    backup_dir=$candidate
  fi
fi
if [ -z "$backup_dir" ]; then
  backup_dir=$(
    find "$backup_root" -mindepth 1 -maxdepth 1 -type d -print | LC_ALL=C sort -r |
      while IFS= read -r candidate; do
        if is_unused_backup "$candidate"; then
          printf '%s\n' "$candidate"
          break
        fi
      done
  )
fi
if [ -z "$backup_dir" ] || [ ! -d "$backup_dir" ]; then
  printf 'No unused Hobot Code backup found under %s\n' "$backup_root" >&2
  exit 1
fi
if [ -L "$backup_dir" ]; then
  printf 'Refusing symbolic-link backup: %s\n' "$backup_dir" >&2
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
backup_link=$(find "$backup_dir" -type l -print -quit) || exit 1
if [ -n "$backup_link" ]; then
  printf 'Backup must not contain symbolic links: %s\n' "$backup_link" >&2
  exit 1
fi
backup_special=$(find "$backup_dir" ! -type d ! -type f -print -quit) || exit 1
if [ -n "$backup_special" ]; then
  printf 'Backup contains an unsupported filesystem entry: %s\n' "$backup_special" >&2
  exit 1
fi
if [ "$(stat -c %u "$backup_dir")" -ne 0 ]; then
  printf 'Backup directory must be owned by root: %s\n' "$backup_dir" >&2
  exit 1
fi
if [ ! -d "$backup_dir/runtime-installed" ] || [ ! -f "$backup_dir/hobot-command" ]; then
  printf 'Backup is incomplete: %s\n' "$backup_dir" >&2
  exit 1
fi
restored_marker="$backup_dir/.hobot-restored"
if [ -e "$restored_marker" ]; then
  restored_marker_existed=1
  if [ ! -f "$restored_marker" ]; then
    printf 'Backup restore marker must be a regular file: %s\n' "$restored_marker" >&2
    exit 1
  fi
  printf 'Backup has already been restored: %s\n' "$backup_dir" >&2
  exit 1
fi
if { [ -e /etc/hobot-code ] || [ -L /etc/hobot-code ]; } && [ -d "$backup_dir/legacy-etc-hobot-code" ]; then
  printf 'Refusing to overwrite existing /etc/hobot-code during rollback\n' >&2
  exit 1
fi
if { [ -e /var/lib/hobot-code ] || [ -L /var/lib/hobot-code ]; } && [ -d "$backup_dir/legacy-var-hobot-code" ]; then
  printf 'Refusing to overwrite existing /var/lib/hobot-code during rollback\n' >&2
  exit 1
fi

runtime_required_kib=$(du -sk "$backup_dir/runtime-installed" | awk '{print $1}')
runtime_required_kib=$((runtime_required_kib + 32768))
etc_required_kib=0
var_required_kib=0
if [ -d "$backup_dir/legacy-etc-hobot-code" ]; then
  etc_required_kib=$(du -sk "$backup_dir/legacy-etc-hobot-code" | awk '{print $1}')
  etc_required_kib=$((etc_required_kib + 8192))
fi
if [ -d "$backup_dir/legacy-var-hobot-code" ]; then
  var_required_kib=$(du -sk "$backup_dir/legacy-var-hobot-code" | awk '{print $1}')
  var_required_kib=$((var_required_kib + 8192))
fi

runtime_device=$(stat -c %d /usr/local/lib)
etc_device=$(stat -c %d /etc)
var_device=$(stat -c %d /var/lib)
if [ "$etc_device" = "$runtime_device" ]; then
  runtime_required_kib=$((runtime_required_kib + etc_required_kib))
  etc_required_kib=0
fi
if [ "$var_device" = "$runtime_device" ]; then
  runtime_required_kib=$((runtime_required_kib + var_required_kib))
  var_required_kib=0
elif [ "$var_device" = "$etc_device" ]; then
  etc_required_kib=$((etc_required_kib + var_required_kib))
  var_required_kib=0
fi

check_available_space() {
  space_required=$1
  space_target=$2
  space_label=$3
  if [ "$space_required" -eq 0 ]; then return; fi
  space_available=$(df -Pk "$space_target" | awk 'NR == 2 { print $4 }')
  if [ -z "$space_available" ] || [ "$space_available" -lt "$space_required" ]; then
    printf 'Insufficient free space for %s: need at least %s KiB, available %s KiB\n' "$space_label" "$space_required" "${space_available:-unknown}" >&2
    exit 1
  fi
}
check_available_space "$runtime_required_kib" /usr/local/lib 'rollback'
check_available_space "$etc_required_kib" /etc 'legacy configuration restore'
check_available_space "$var_required_kib" /var/lib 'legacy state restore'

staged_runtime=$(mktemp -d /usr/local/lib/hobot-code.rollback.XXXXXX)
chmod 0755 "$staged_runtime"
cp -R "$backup_dir/runtime-installed/." "$staged_runtime/"
staged_launcher=$(mktemp /usr/local/bin/hobot.rollback.XXXXXX)
install -m 0755 "$backup_dir/hobot-command" "$staged_launcher"
if [ -f "$backup_dir/hobot-rollback-command" ]; then
  staged_rollback=$(mktemp /usr/local/sbin/hobot-rollback.rollback.XXXXXX)
  install -m 0755 "$backup_dir/hobot-rollback-command" "$staged_rollback"
fi
"$staged_runtime/hobot" --version >/dev/null

previous_runtime=$(mktemp -d /usr/local/lib/hobot-code-before-rollback.XXXXXX)
rmdir "$previous_runtime"
previous_launcher=$(mktemp /usr/local/bin/hobot.before-rollback.XXXXXX)
previous_rollback=$(mktemp /usr/local/sbin/hobot-rollback.before-rollback.XXXXXX)
if [ -d /usr/local/lib/hobot-code ]; then
  had_previous_runtime=1
  runtime_swapped=1
  mv /usr/local/lib/hobot-code "$previous_runtime"
else
  runtime_swapped=1
fi
if [ -f /usr/local/bin/hobot ]; then
  install -m 0755 /usr/local/bin/hobot "$previous_launcher"
else
  rm -f "$previous_launcher"
fi
if [ -f /usr/local/sbin/hobot-rollback ]; then
  install -m 0755 /usr/local/sbin/hobot-rollback "$previous_rollback"
else
  rm -f "$previous_rollback"
fi

mv "$staged_runtime" /usr/local/lib/hobot-code
launcher_swapped=1
mv "$staged_launcher" /usr/local/bin/hobot
rollback_command_swapped=1
if [ -n "$staged_rollback" ]; then
  mv "$staged_rollback" /usr/local/sbin/hobot-rollback
else
  rm -f /usr/local/sbin/hobot-rollback
fi

if [ ! -f /usr/local/bin/hobot ] || [ ! -x /usr/local/bin/hobot ]; then
  printf 'Restored launcher validation failed\n' >&2
  exit 1
fi
if [ -n "$staged_rollback" ] && { [ ! -f /usr/local/sbin/hobot-rollback ] || [ ! -x /usr/local/sbin/hobot-rollback ]; }; then
  printf 'Restored rollback command validation failed\n' >&2
  exit 1
fi

if [ -d "$backup_dir/legacy-etc-hobot-code" ]; then
  legacy_config_restored=1
  cp -a "$backup_dir/legacy-etc-hobot-code" /etc/hobot-code
fi
if [ -d "$backup_dir/legacy-var-hobot-code" ]; then
  legacy_state_restored=1
  cp -a "$backup_dir/legacy-var-hobot-code" /var/lib/hobot-code
fi

/usr/local/lib/hobot-code/hobot --version
if [ "$restored_marker_existed" -eq 0 ]; then
  restored_marker_new="$backup_dir/.hobot-restored.new.$$"
  date -u +%Y-%m-%dT%H:%M:%SZ > "$restored_marker_new"
  chmod 0600 "$restored_marker_new"
  mv "$restored_marker_new" "$restored_marker"
fi
last_restored_new="$backup_root/.last-restored.new.$$"
printf '%s\n' "$backup_dir" > "$last_restored_new"
chmod 0600 "$last_restored_new"
last_restored_write_started=1
mv "$last_restored_new" "$backup_root/.last-restored"
rollback_complete=1

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
if [ -n "$previous_runtime" ] && [ -d "$previous_runtime" ]; then
  mv "$previous_runtime" "/usr/local/lib/hobot-code-failed-$timestamp-$$"
fi
printf 'Restored Hobot Code from %s\n' "$backup_dir"
