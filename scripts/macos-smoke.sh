#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_DIR="${LINEA_MACOS_APP:-$ROOT_DIR/dist/macos/Linea.app}"
DMG_PATH="${LINEA_MACOS_DMG:-$ROOT_DIR/dist/macos/Linea.dmg}"
SERVER="$APP_DIR/Contents/Resources/linea"
LAUNCHER="$APP_DIR/Contents/MacOS/Linea"
PORT="${LINEA_MACOS_SMOKE_PORT:-18081}"
API_ADDR="127.0.0.1:$PORT"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/linea-macos-smoke.XXXXXX")"
ENV_FILE="$TMP_DIR/no-env"
SETTINGS_FILE="$TMP_DIR/settings.json"
LOG_FILE="$TMP_DIR/linea.log"
RUN_UI_SMOKE="${LINEA_MACOS_UI_SMOKE:-0}"
PID=""

cleanup() {
  if [[ -n "$PID" ]]; then
    kill "$PID" >/dev/null 2>&1 || true
    wait "$PID" >/dev/null 2>&1 || true
  fi
  while IFS= read -r port_pid; do
    [[ -n "$port_pid" ]] || continue
    kill "$port_pid" >/dev/null 2>&1 || true
  done < <(lsof -tiTCP:"$PORT" -sTCP:LISTEN 2>/dev/null || true)
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

if lsof -tiTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "port $PORT is already in use" >&2
  exit 1
fi

if [[ ! -x "$SERVER" ]]; then
  echo "missing bundled server: $SERVER" >&2
  exit 1
fi

if [[ ! -f "$LAUNCHER" ]]; then
  echo "missing app launcher: $LAUNCHER" >&2
  exit 1
fi

if [[ ! -f "$APP_DIR/Contents/Info.plist" ]]; then
  echo "missing Info.plist" >&2
  exit 1
fi

if [[ "$RUN_UI_SMOKE" == "1" ]]; then
  LINEA_ENV_FILE="$ENV_FILE" \
    LINEA_SETTINGS_FILE="$SETTINGS_FILE" \
    LINEA_WORKSPACE_DIR="$ROOT_DIR" \
    API_ADDR="$API_ADDR" \
    "$LAUNCHER" >"$LOG_FILE" 2>&1 &
else
  LINEA_ENV_FILE="$ENV_FILE" LINEA_SETTINGS_FILE="$SETTINGS_FILE" API_ADDR="$API_ADDR" "$SERVER" >"$LOG_FILE" 2>&1 &
fi
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

if [[ "$RUN_UI_SMOKE" == "1" ]]; then
  LINEA_UI_URL="http://$API_ADDR/" \
    LINEA_AGENT_REVIEW_FILE="README.md" \
    node "$ROOT_DIR/scripts/ui-smoke.mjs" --agent-review

  if grep -q "\[WKWebView Smoke\] FAIL" "$LOG_FILE"; then
    echo "WKWebView smoke test reported failure:" >&2
    grep "\[WKWebView Smoke\]" "$LOG_FILE" >&2 || true
    exit 1
  fi
  if ! grep -q "\[WKWebView Smoke\] PASS" "$LOG_FILE"; then
    echo "WKWebView smoke test did not report PASS in logs" >&2
    tail -n 40 "$LOG_FILE" >&2 || true
    exit 1
  fi
  echo "PASS WKWebView smoke test"
fi

if [[ -f "$DMG_PATH" ]]; then
  hdiutil verify "$DMG_PATH" >/dev/null
else
  echo "missing DMG: $DMG_PATH" >&2
  exit 1
fi

echo "PASS macOS app smoke - $APP_DIR"
