#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_DIR="${LINEA_MACOS_APP:-$ROOT_DIR/dist/macos/Linea.app}"
DMG_PATH="${LINEA_MACOS_DMG:-$ROOT_DIR/dist/macos/Linea.dmg}"
SERVER="$APP_DIR/Contents/Resources/linea"
PORT="${LINEA_MACOS_SMOKE_PORT:-18081}"
API_ADDR="127.0.0.1:$PORT"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/linea-macos-smoke.XXXXXX")"
ENV_FILE="$TMP_DIR/no-env"
SETTINGS_FILE="$TMP_DIR/settings.json"
LOG_FILE="$TMP_DIR/linea.log"
PID=""

cleanup() {
  if [[ -n "$PID" ]]; then
    kill "$PID" >/dev/null 2>&1 || true
    wait "$PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

if [[ ! -x "$SERVER" ]]; then
  echo "missing bundled server: $SERVER" >&2
  exit 1
fi

if [[ ! -f "$APP_DIR/Contents/MacOS/Linea" ]]; then
  echo "missing app launcher: $APP_DIR/Contents/MacOS/Linea" >&2
  exit 1
fi

if [[ ! -f "$APP_DIR/Contents/Info.plist" ]]; then
  echo "missing Info.plist" >&2
  exit 1
fi

LINEA_ENV_FILE="$ENV_FILE" LINEA_SETTINGS_FILE="$SETTINGS_FILE" API_ADDR="$API_ADDR" "$SERVER" >"$LOG_FILE" 2>&1 &
PID="$!"

for _ in $(seq 1 80); do
  if curl -fsS "http://$API_ADDR/healthz" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$PID" >/dev/null 2>&1; then
    echo "bundled server exited early" >&2
    tail -n 40 "$LOG_FILE" >&2 || true
    exit 1
  fi
  sleep 0.25
done

health="$(curl -fsS "http://$API_ADDR/healthz")"
case "$health" in
  *'"status":"ok"'*) ;;
  *)
    echo "unexpected health response: $health" >&2
    exit 1
    ;;
esac

index="$(curl -fsS "http://$API_ADDR/")"
case "$index" in
  *'<div id="root">'*|*'<div id="root"></div>'*) ;;
  *)
    echo "packaged UI did not serve the React root" >&2
    exit 1
    ;;
esac

if ! "$SERVER" -version >/dev/null; then
  echo "bundled server version command failed" >&2
  exit 1
fi

if [[ -f "$DMG_PATH" ]]; then
  hdiutil verify "$DMG_PATH" >/dev/null
else
  echo "missing DMG: $DMG_PATH" >&2
  exit 1
fi

echo "PASS macOS app smoke - $APP_DIR"
