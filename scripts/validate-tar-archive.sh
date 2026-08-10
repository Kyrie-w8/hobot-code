#!/bin/sh
set -eu

if [ "$#" -ne 3 ]; then
  printf 'Usage: validate-tar-archive.sh <absolute-archive-path> <expected-root> <label>\n' >&2
  exit 2
fi

archive_path=$1
expected_root=$2
archive_label=$3
case "$archive_path" in
  /*) ;;
  *)
    printf '%s path must be absolute: %s\n' "$archive_label" "$archive_path" >&2
    exit 1
    ;;
esac
case "$expected_root" in
  ''|.|..|*/*)
    printf '%s expected root is invalid: %s\n' "$archive_label" "$expected_root" >&2
    exit 1
    ;;
esac
if [ ! -f "$archive_path" ]; then
  printf '%s is not a regular archive: %s\n' "$archive_label" "$archive_path" >&2
  exit 1
fi

listing_dir=$(mktemp -d "${TMPDIR:-/tmp}/hobot-archive-listing.XXXXXX")
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  rm -rf "$listing_dir"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' HUP INT TERM

LC_ALL=C tar -tzf "$archive_path" > "$listing_dir/names"
LC_ALL=C tar -tvzf "$archive_path" > "$listing_dir/verbose"

member_count=0
while IFS= read -r member_name || [ -n "$member_name" ]; do
  member_count=$((member_count + 1))
  case "$member_name" in
    *[![:print:]]*|*\\*)
      printf '%s contains a path with control characters or backslashes\n' "$archive_label" >&2
      exit 1
      ;;
    /*)
      printf '%s contains an absolute path outside %s/: %s\n' "$archive_label" "$expected_root" "$member_name" >&2
      exit 1
      ;;
  esac
  canonical_name=${member_name%/}
  case "/$canonical_name/" in
    *'//'*)
      printf '%s contains an empty path component: %s\n' "$archive_label" "$member_name" >&2
      exit 1
      ;;
    */./*|*/../*)
      printf '%s contains a non-canonical path: %s\n' "$archive_label" "$member_name" >&2
      exit 1
      ;;
  esac
  case "$canonical_name" in
    "$expected_root"|"$expected_root"/*) ;;
    *)
      printf '%s contains a path outside %s/: %s\n' "$archive_label" "$expected_root" "$member_name" >&2
      exit 1
      ;;
  esac
done < "$listing_dir/names"

if [ "$member_count" -eq 0 ]; then
  printf '%s is empty\n' "$archive_label" >&2
  exit 1
fi
unsupported_type=$(awk 'substr($0, 1, 1) != "-" && substr($0, 1, 1) != "d" { print substr($0, 1, 1); exit }' "$listing_dir/verbose")
if [ -n "$unsupported_type" ]; then
  printf '%s contains unsupported archive entry type: %s\n' "$archive_label" "$unsupported_type" >&2
  exit 1
fi

printf 'Validated %s layout under %s/\n' "$archive_label" "$expected_root"
