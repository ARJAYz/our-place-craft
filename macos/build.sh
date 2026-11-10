#!/bin/bash
# Builds "Our Place Craft Launcher.app" (universal: Apple Silicon + Intel) on a macOS machine.
# usage: macos/build.sh <version YYYYMMDD-HHMM> <out dir>
set -euo pipefail
V="$1"; OUT="$2"
cd "$(dirname "$0")/.."
go get github.com/webview/webview_go@latest
APP="$OUT/Our Place Craft Launcher.app"
rm -rf "$APP"; mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
for A in arm64 amd64; do
  CC_ARCH=$([ $A = amd64 ] && echo x86_64 || echo arm64)
  CGO_ENABLED=1 GOOS=darwin GOARCH=$A CC="clang -arch $CC_ARCH" CGO_CFLAGS="-mmacosx-version-min=11.0" CGO_LDFLAGS="-mmacosx-version-min=11.0" \
    go build -trimpath -ldflags "-s -w -X main.launcherVersion=$V" -o "$OUT/opcl-$A" .
done
lipo -create -output "$APP/Contents/MacOS/OurPlaceCraftLauncher" "$OUT/opcl-arm64" "$OUT/opcl-amd64"
rm "$OUT/opcl-arm64" "$OUT/opcl-amd64"
cp macos/Info.plist "$APP/Contents/Info.plist"  # fixed content: the self-update swaps only the binary
IS="$OUT/AppIcon.iconset"; rm -rf "$IS"; mkdir "$IS"
for s in 16 32 128 256 512; do
  sips -z $s $s macos/icon_1024.png --out "$IS/icon_${s}x${s}.png" >/dev/null
  d=$((s*2)); sips -z $d $d macos/icon_1024.png --out "$IS/icon_${s}x${s}@2x.png" >/dev/null
done
iconutil -c icns "$IS" -o "$APP/Contents/Resources/AppIcon.icns"; rm -rf "$IS"
codesign --force --deep -s - "$APP"
codesign --verify --deep --strict "$APP"
lipo -info "$APP/Contents/MacOS/OurPlaceCraftLauncher"
# downloads for people: the app in a zip (ditto keeps the bundle intact)
( cd "$OUT" && ditto -c -k --keepParent "Our Place Craft Launcher.app" "OurPlaceCraftLauncher-macOS.zip" )
# self-update asset: only the signed binary
mkdir -p "$OUT/upd/MacOS" && cp "$APP/Contents/MacOS/OurPlaceCraftLauncher" "$OUT/upd/MacOS/"
( cd "$OUT/upd" && zip -q -X "../OurPlaceCraftLauncher-macOS-update.zip" MacOS/OurPlaceCraftLauncher )
rm -rf "$OUT/upd"
