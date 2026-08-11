#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
version=$(sed -n '1p' "$repo_root/VERSION")
app="$repo_root/studio/build/bin/Hobot Code.app"
executable="$app/Contents/MacOS/HobotCode"
plist="$app/Contents/Info.plist"
dist="$repo_root/dist"
image="$dist/hobot-code-$version-macos-arm64.dmg"
checksum="$image.sha256"

test -d "$app"
test -x "$executable"
test "$(plutil -extract CFBundleName raw -o - "$plist")" = "Hobot Code"
test "$(plutil -extract CFBundleIdentifier raw -o - "$plist")" = "cc.d-robotics.hobot-code"
test "$(plutil -extract CFBundleShortVersionString raw -o - "$plist")" = "$version"
file "$executable" | grep -q 'Mach-O 64-bit executable arm64'
codesign --verify --deep --strict "$app"

mkdir -p "$dist"
staging=$(mktemp -d "${TMPDIR:-/tmp}/hobot-code-dmg.XXXXXX")
trap 'rm -rf "$staging"' EXIT HUP INT TERM
ditto "$app" "$staging/Hobot Code.app"
ln -s /Applications "$staging/Applications"

hdiutil create \
  -volname "Hobot Code $version" \
  -srcfolder "$staging" \
  -ov \
  -format UDZO \
  "$image"

(
  cd "$dist"
  shasum -a 256 "$(basename "$image")" >"$(basename "$checksum")"
)

printf 'Created %s\n' "$image"
