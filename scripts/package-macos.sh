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
ICON_SOURCE="$ROOT_DIR/assets/linea-icon.png"
ICONSET="$DIST_DIR/Linea.iconset"

rm -rf "$APP_DIR" "$DMG_ROOT" "$DMG_PATH" "$ICONSET"
mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources" "$DMG_ROOT"

cd "$ROOT_DIR/frontend"
npm ci
npm run build

cd "$ROOT_DIR/backend"
go build -ldflags="-s -w -X main.version=$LINEA_VERSION" -o "$APP_DIR/Contents/Resources/linea" ./cmd/server

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
  <string>Linea</string>
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
</dict>
</plist>
PLIST

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
fi

cat > "$APP_DIR/Contents/MacOS/Linea" <<'LAUNCHER'
#!/bin/zsh
set -eu

APP_DIR="${0:A:h:h}"
LINEA_BIN="$APP_DIR/Resources/linea"
LOG_DIR="$HOME/Library/Logs/Linea"
mkdir -p "$LOG_DIR"

export API_ADDR="${API_ADDR:-127.0.0.1:8080}"

case "$API_ADDR" in
  :*) LINEA_URL="http://127.0.0.1${API_ADDR}" ;;
  *:*) LINEA_URL="http://${API_ADDR}" ;;
  *) LINEA_URL="http://127.0.0.1:${API_ADDR}" ;;
esac

"$LINEA_BIN" >> "$LOG_DIR/linea.log" 2>&1 &
LINEA_PID=$!

cleanup() {
  kill "$LINEA_PID" 2>/dev/null || true
  wait "$LINEA_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

for _ in {1..80}; do
  if curl -fsS "$LINEA_URL/healthz" >/dev/null 2>&1; then
    open "$LINEA_URL/"
    wait "$LINEA_PID"
    exit $?
  fi
  sleep 0.25
done

open "$LOG_DIR/linea.log"
exit 1
LAUNCHER
chmod +x "$APP_DIR/Contents/MacOS/Linea"

cp -R "$APP_DIR" "$DMG_ROOT/"
hdiutil create -volname "Linea" -srcfolder "$DMG_ROOT" -ov -format UDZO "$DMG_PATH" >/dev/null
rm -rf "$DMG_ROOT" "$ICONSET"

echo "$APP_DIR"
echo "$DMG_PATH"
