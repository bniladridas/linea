#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LINEA_VERSION="${VERSION:-$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null | sed 's/^v//')}"
APP_VERSION="${APP_VERSION:-$(git -C "$ROOT_DIR" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')}"
BUILD_VERSION="${BUILD_VERSION:-$(git -C "$ROOT_DIR" rev-list --count HEAD 2>/dev/null)}"
APP_VERSION="${APP_VERSION:-0.1.0}"
BUILD_VERSION="${BUILD_VERSION:-1}"
DIST_DIR="$ROOT_DIR/dist/macos"
APP_DIR="$DIST_DIR/Linea.app"
DMG_ROOT="$DIST_DIR/dmg-root"
DMG_PATH="$DIST_DIR/Linea.dmg"
RW_DMG_PATH="$DIST_DIR/Linea-rw.dmg"
ICON_SOURCE="$ROOT_DIR/assets/linea-icon.png"
ICONSET="$DIST_DIR/Linea.iconset"
MACOS_SOURCE="$ROOT_DIR/macos/Linea/main.swift"
MOUNT_DIR=""

cleanup() {
  if [[ -n "$MOUNT_DIR" && -d "$MOUNT_DIR" ]]; then
    hdiutil detach "$MOUNT_DIR" >/dev/null 2>&1 || true
    rm -rf "$MOUNT_DIR"
  fi
}
trap cleanup EXIT

retry_hdiutil() {
  local attempt
  for attempt in 1 2 3; do
    if hdiutil "$@"; then
      return 0
    fi
    if [[ "$attempt" == "3" ]]; then
      return 1
    fi
    sleep "$attempt"
  done
}

rm -rf "$APP_DIR" "$DMG_ROOT" "$DMG_PATH" "$RW_DMG_PATH" "$ICONSET"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources" "$DMG_ROOT"

cd "$ROOT_DIR/frontend"
npm ci
npm run build

cd "$ROOT_DIR/backend"
GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w -X main.version=$LINEA_VERSION" -o "$APP_DIR/Contents/Resources/linea" ./cmd/server
swiftc \
  -Osize \
  -target arm64-apple-macos13.0 \
  -framework Cocoa \
  -framework WebKit \
  -o "$APP_DIR/Contents/MacOS/Linea" \
  "$MACOS_SOURCE"

cat > "$APP_DIR/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleDisplayName</key>
  <string>Linea</string>
  <key>CFBundleExecutable</key>
  <string>Linea</string>
  <key>CFBundleIconFile</key>
  <string>Linea.icns</string>
  <key>CFBundleIdentifier</key>
  <string>com.bniladridas.linea</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>Linea</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>$APP_VERSION</string>
  <key>CFBundleVersion</key>
  <string>$BUILD_VERSION</string>
  <key>LSMinimumSystemVersion</key>
  <string>13.0</string>
  <key>NSHighResolutionCapable</key>
  <true/>
  <key>NSAppTransportSecurity</key>
  <dict>
    <key>NSAllowsLocalNetworking</key>
    <true/>
  </dict>
</dict>
</plist>
PLIST
printf "APPL????" > "$APP_DIR/Contents/PkgInfo"

if [[ -f "$ICON_SOURCE" ]]; then
  mkdir -p "$ICONSET"
  sips -z 16 16 "$ICON_SOURCE" --out "$ICONSET/icon_16x16.png" >/dev/null
  sips -z 32 32 "$ICON_SOURCE" --out "$ICONSET/icon_16x16@2x.png" >/dev/null
  sips -z 32 32 "$ICON_SOURCE" --out "$ICONSET/icon_32x32.png" >/dev/null
  sips -z 64 64 "$ICON_SOURCE" --out "$ICONSET/icon_32x32@2x.png" >/dev/null
  sips -z 128 128 "$ICON_SOURCE" --out "$ICONSET/icon_128x128.png" >/dev/null
  sips -z 256 256 "$ICON_SOURCE" --out "$ICONSET/icon_128x128@2x.png" >/dev/null
  sips -z 256 256 "$ICON_SOURCE" --out "$ICONSET/icon_256x256.png" >/dev/null
  sips -z 512 512 "$ICON_SOURCE" --out "$ICONSET/icon_256x256@2x.png" >/dev/null
  sips -z 512 512 "$ICON_SOURCE" --out "$ICONSET/icon_512x512.png" >/dev/null
  cp "$ICON_SOURCE" "$ICONSET/icon_512x512@2x.png"
  iconutil -c icns "$ICONSET" -o "$APP_DIR/Contents/Resources/Linea.icns"
  cp "$APP_DIR/Contents/Resources/Linea.icns" "$DMG_ROOT/.VolumeIcon.icns"
  SetFile -a B "$APP_DIR" 2>/dev/null || true
fi

ditto --rsrc --extattr "$APP_DIR" "$DMG_ROOT/Linea.app"
SetFile -a B "$DMG_ROOT/Linea.app" 2>/dev/null || true
ln -s /Applications "$DMG_ROOT/Applications"
retry_hdiutil create -volname "Linea" -srcfolder "$DMG_ROOT" -ov -format UDRW "$RW_DMG_PATH" >/dev/null

MOUNT_DIR="$(mktemp -d "$DIST_DIR/linea-dmg.XXXXXX")"
hdiutil attach "$RW_DMG_PATH" -mountpoint "$MOUNT_DIR" -nobrowse >/dev/null
SetFile -a C "$MOUNT_DIR" 2>/dev/null || true
if ! osascript <<APPLESCRIPT
set dmgFolder to POSIX file "$MOUNT_DIR" as alias
tell application "Finder"
  open dmgFolder
  set theWindow to container window of dmgFolder
  tell theWindow
    set current view of theWindow to icon view
    set toolbar visible of theWindow to false
    set statusbar visible of theWindow to false
    set bounds of theWindow to {100, 100, 700, 420}
    set theOptions to icon view options of theWindow
    set arrangement of theOptions to not arranged
    set icon size of theOptions to 96
    set position of item "Linea.app" of theWindow to {190, 160}
    set position of item "Applications" of theWindow to {470, 160}
    close
  end tell
  update dmgFolder without registering applications
  delay 1
end tell
APPLESCRIPT
then
  echo "warning: could not apply Finder DMG layout" >&2
fi
hdiutil detach "$MOUNT_DIR" >/dev/null
rm -rf "$MOUNT_DIR"
MOUNT_DIR=""
retry_hdiutil convert "$RW_DMG_PATH" -ov -format UDZO -o "$DMG_PATH" >/dev/null
rm -rf "$DMG_ROOT" "$RW_DMG_PATH" "$ICONSET"

echo "$APP_DIR"
echo "$DMG_PATH"
