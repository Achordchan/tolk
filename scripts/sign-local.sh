#!/bin/sh
# Signs a local macOS build with the Apple Development certificate in the login
# keychain, the way release.yml does, so permission grants survive rebuilds
# (an ad-hoc signature changes with every build and macOS forgets the grant).
set -e
APP="${1:-build/darwin-arm64/Tolk.app}"
IDENTITY="$(security find-identity -v -p codesigning | awk '/"Apple Development:/ { print $2; exit }')"
[ -n "$IDENTITY" ] || { echo "没有找到 Apple Development 证书" >&2; exit 1; }
codesign --force --sign "$IDENTITY" --timestamp=none "$APP/Contents/Resources/tolk-helper"
codesign --force --deep --sign "$IDENTITY" --timestamp=none "$APP"
codesign --verify --deep --strict "$APP"
codesign -d -r- "$APP" 2>&1 | sed -n 's/^.*designated => //p'
