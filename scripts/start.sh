#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# ── parse flags ──────────────────────────────────────────────────────────────
ANDROID="${ANDROID:-0}"
IOS="${IOS:-0}"
MACOS="${MACOS:-0}"
NODB="${NODB:-0}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --android) ANDROID=1; shift ;;
    --ios)     IOS=1; shift ;;
    --macos)   MACOS=1; shift ;;
    --all)     ANDROID=1; IOS=1; MACOS=1; NODB=1; shift ;;
    --no-db)   NODB=1; shift ;;
    *)         echo "usage: $0 [--android] [--ios] [--macos] [--all] [--no-db]"; exit 1 ;;
  esac
done

# If no flags passed and no env vars set, prompt interactively.
if [ "$ANDROID" != 1 ] && [ "$IOS" != 1 ] && [ "$MACOS" != 1 ] && [ "$NODB" != 1 ]; then
  if [ -t 0 ]; then
    read -r -p "Run without database? (y/n) [n] " nodb_choice
    case "$nodb_choice" in
      y|Y) NODB=1 ;;
      *) ;;
    esac

    echo "Which platforms? (space-separated, e.g. '1 3')"
    echo "  0) server only"
    echo "  1) Android"
    echo "  2) iOS"
    echo "  3) macOS"
    echo "  a) all"
    read -r -p "> " choice
    for opt in $choice; do
      case "$opt" in
        0|"") ;;
        1) ANDROID=1 ;;
        2) IOS=1 ;;
        3) MACOS=1 ;;
        a|A) ANDROID=1; IOS=1; MACOS=1 ;;
        *) ;;
      esac
    done
  fi
fi

# ── server ────────────────────────────────────────────────────────────────────
echo "==> checking prerequisites"
if [ "$NODB" != 1 ]; then
  command -v psql >/dev/null 2>&1 || { echo "missing: psql"; exit 1; }
  pg_isready -q 2>/dev/null || { echo "postgres not running"; exit 1; }
  [ -f .env ] || { echo "missing: .env"; exit 1; }
  export $(grep -v '^#' .env | xargs)
  echo "==> running migrations"
  ./bin/linea migrate
else
  echo "  skipping postgres (in-memory mode)"
  if [ -f .env ]; then
    export $(grep -v '^#' .env | xargs)
    unset DATABASE_URL
  fi
fi

echo "==> starting server"
pkill -f "bin/linea" 2>/dev/null || true
LINEA_ENV_FILE=/dev/null nohup bin/linea > /tmp/linea.log 2>&1 &
sleep 2

echo "==> checking health"
if curl -sf http://127.0.0.1:8080/healthz > /dev/null; then
  echo "linea server running at http://localhost:8080"
else
  echo "startup failed - check /tmp/linea.log"
  exit 1
fi

# ── android ───────────────────────────────────────────────────────────────────
if [ "$ANDROID" = 1 ]; then
  echo "==> building android apk"
  (cd android && ./gradlew assembleDebug) || { echo "  android build failed"; true; }

  echo "==> starting emulator"
  ANDROID_HOME="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
  AVD="${ANDROID_AVD:-Pixel_10_Pro_XL}"

  if [ -x "$ANDROID_HOME/emulator/emulator" ]; then
    nohup "$ANDROID_HOME/emulator/emulator" -avd "$AVD" -no-audio > /tmp/emulator.log 2>&1 & disown
    echo "  waiting for boot..."
    for i in $(seq 1 90); do
      sleep 2
      BT=$("$ANDROID_HOME/platform-tools/adb" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r\n') || true
      if [ "$BT" = "1" ]; then
        echo "  emulator booted after $((i*2))s"
        break
      fi
    done

    echo "==> installing and launching"
    BOOTED=$("$ANDROID_HOME/platform-tools/adb" devices 2>/dev/null | grep -w "device" | head -1) || true
    APK="android/app/build/outputs/apk/debug/app-debug.apk"
    if [ -n "$BOOTED" ] && [ -f "$APK" ]; then
      "$ANDROID_HOME/platform-tools/adb" install -r android/app/build/outputs/apk/debug/app-debug.apk 2>/dev/null || true
      "$ANDROID_HOME/platform-tools/adb" shell am start -n com.bniladridas.linea/.MainActivity 2>/dev/null || true
      echo "  android app launched"
    else
      echo "  emulator not available - skip install"
    fi
  else
    echo "  emulator binary not found - skip android"
  fi
fi

# ── ios ───────────────────────────────────────────────────────────────────────
if [ "$IOS" = 1 ]; then
  echo "==> building ios app"
  xcodebuild -project ios/Linea.xcodeproj -scheme Linea \
    -destination 'platform=iOS Simulator,name=iPhone 17 Pro' \
    -derivedDataPath ios/DerivedData build CODE_SIGNING_ALLOWED=NO \
    -quiet || true

  echo "==> launching simulator"
  xcrun simctl boot "iPhone 17 Pro" 2>/dev/null || true
  open -a Simulator 2>/dev/null || true
  xcrun simctl install booted ios/DerivedData/Build/Products/Debug-iphonesimulator/Linea.app 2>/dev/null || true
  xcrun simctl launch booted com.bniladridas.linea 2>/dev/null || true
  echo "  ios app launched"
fi

# ── macos ─────────────────────────────────────────────────────────────────────
if [ "$MACOS" = 1 ]; then
  echo "==> building macos app"
  ./scripts/package-macos.sh 2>/dev/null || true

  APP="$ROOT/dist/macos/Linea.app"
  if [ -d "$APP" ]; then
    echo "==> launching macos app"
    open "$APP" 2>/dev/null || true
    echo "  macos app launched"
  else
    echo "  macos build failed - $APP not found"
  fi
fi

echo "==> done"