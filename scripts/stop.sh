#!/usr/bin/env bash
set -euo pipefail

echo "==> stopping linea"

# Go server
pkill -f "bin/linea" 2>/dev/null && echo "  server stopped" || true
lsof -ti:8080 2>/dev/null | xargs kill 2>/dev/null || true

# Android emulator
ANDROID_HOME="${ANDROID_HOME:-$HOME/Library/Android/sdk}"
"$ANDROID_HOME/platform-tools/adb" emu kill 2>/dev/null && echo "  emulator stopped" || true
pkill -f "emulator" 2>/dev/null || true

# iOS simulator
xcrun simctl shutdown all 2>/dev/null && echo "  simulator stopped" || true
pkill -f "Simulator" 2>/dev/null || true

# macOS app
pkill -f "Linea.app" 2>/dev/null && echo "  macos app stopped" || true

echo "==> done"