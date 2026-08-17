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
signing_identity=${HOBOT_CODE_MACOS_SIGNING_IDENTITY:-}
require_signed_release=${HOBOT_CODE_REQUIRE_SIGNED_RELEASE:-0}
notary_key=${HOBOT_CODE_NOTARY_KEY_PATH:-}
notary_key_id=${HOBOT_CODE_NOTARY_KEY_ID:-}
notary_issuer=${HOBOT_CODE_NOTARY_ISSUER_ID:-}

test -d "$app"
test -x "$executable"
test "$(plutil -extract CFBundleName raw -o - "$plist")" = "Hobot Code"
test "$(plutil -extract CFBundleIdentifier raw -o - "$plist")" = "cc.d-robotics.hobot-code"
test "$(plutil -extract CFBundleShortVersionString raw -o - "$plist")" = "$version"
file "$executable" | grep -q 'Mach-O 64-bit executable arm64'

if [ -f "$dist/hobot-code-$version-linux-arm64.tar.gz" ]; then
  mkdir -p "$app/Contents/Resources"
  cp "$dist/hobot-code-$version-linux-arm64.tar.gz" "$app/Contents/Resources/hobot-code-$version-linux-arm64.tar.gz"
fi

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/hobot-code-macos.XXXXXX")

staging="$work_dir/dmg"
app_archive="$work_dir/Hobot Code.zip"
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

if [ -n "$signing_identity" ]; then
  codesign --force --deep --options runtime --timestamp --sign "$signing_identity" "$app"
elif [ "$require_signed_release" = 1 ]; then
  printf 'HOBOT_CODE_MACOS_SIGNING_IDENTITY is required for a public release.\n' >&2
  exit 1
else
  codesign --force --deep --sign - "$app"
fi

codesign --verify --deep --strict --verbose=2 "$app"

if [ "$require_signed_release" = 1 ]; then
  test -r "$notary_key" || { printf 'HOBOT_CODE_NOTARY_KEY_PATH must name a readable App Store Connect API key.\n' >&2; exit 1; }
  test -n "$notary_key_id" || { printf 'HOBOT_CODE_NOTARY_KEY_ID is required.\n' >&2; exit 1; }
  test -n "$notary_issuer" || { printf 'HOBOT_CODE_NOTARY_ISSUER_ID is required.\n' >&2; exit 1; }
  ditto -c -k --keepParent "$app" "$app_archive"
  xcrun notarytool submit "$app_archive" --key "$notary_key" --key-id "$notary_key_id" --issuer "$notary_issuer" --wait
  xcrun stapler staple "$app"
  xcrun stapler validate "$app"
fi

mkdir -p "$dist"
mkdir -p "$staging"
ditto "$app" "$staging/Hobot Code.app"
ln -s /Applications "$staging/Applications"

hdiutil create \
  -volname "Hobot Code $version" \
  -srcfolder "$staging" \
  -ov \
  -format UDZO \
  "$image"

if [ -n "$signing_identity" ]; then
  codesign --force --timestamp --sign "$signing_identity" "$image"
  codesign --verify --verbose=2 "$image"
fi
if [ "$require_signed_release" = 1 ]; then
  xcrun notarytool submit "$image" --key "$notary_key" --key-id "$notary_key_id" --issuer "$notary_issuer" --wait
  xcrun stapler staple "$image"
  xcrun stapler validate "$image"
  spctl --assess --verbose=2 --type open --context context:primary-signature "$image"
fi

(
  cd "$dist"
  shasum -a 256 "$(basename "$image")" >"$(basename "$checksum")"
)

printf 'Created %s\n' "$image"
