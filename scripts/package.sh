#!/bin/sh
set -eu

version=${1:-0.2.0}
root_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
stage_dir="$root_dir/dist/aster-$version-linux-arm64"

rm -rf "$stage_dir"
mkdir -p "$stage_dir/bin" "$stage_dir/config/boards" "$stage_dir/config/providers" "$stage_dir/prompts" "$stage_dir/skills"
install -m 0755 "$root_dir/dist/aster-linux-arm64" "$stage_dir/bin/aster"
install -m 0755 "$root_dir/scripts/install.sh" "$stage_dir/install.sh"
install -m 0755 "$root_dir/scripts/uninstall.sh" "$stage_dir/uninstall.sh"
install -m 0644 "$root_dir/packaging/config.json" "$stage_dir/config/config.json"
install -m 0644 "$root_dir/packaging/aster.service" "$stage_dir/config/aster.service"
install -m 0644 "$root_dir/packaging/aster.env.example" "$stage_dir/config/aster.env.example"
cp "$root_dir"/config/boards/*.json "$stage_dir/config/boards/"
cp "$root_dir"/config/providers/*.json "$stage_dir/config/providers/"
cp "$root_dir/prompts/system.md" "$stage_dir/prompts/system.md"
cp -R "$root_dir/skills/." "$stage_dir/skills/"
COPYFILE_DISABLE=1 tar --no-xattrs -C "$root_dir/dist" -czf "$root_dir/dist/aster-$version-linux-arm64.tar.gz" "aster-$version-linux-arm64"
printf '%s\n' "$root_dir/dist/aster-$version-linux-arm64.tar.gz"
