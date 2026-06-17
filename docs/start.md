# Build from source

Everything depends on the Go server. The web UI, iOS app, and Android app are all
WebView wrappers pointing at it.

One command to build and start:

```sh
make start     # prompts which platforms to launch
make stop      # kills server, emulator, simulator, and macOS app
```

Skip the prompt:

```sh
make start ANDROID=1 IOS=1 MACOS=1   # launches all platforms
make start NODB=1                    # no Postgres (in-memory storage)
make start NODB=1 IOS=1 MACOS=1      # no DB, just iOS + macOS
```

## 1. Go server

Serves the React frontend and the API.

```sh
make run
```

This loads `.env` and starts `bin/linea` in the background.
First-time setup also needs a database migration:

```sh
./bin/linea migrate
```

Visit `http://localhost:8080`.

## 2. Terminal UI

```sh
bin/linea tui
```

Starts the terminal chat interface directly. No server needed.

## 3. Web UI

Open `http://localhost:8080` in any browser. No additional steps.

## 4. macOS (native app)

Builds a `.app` bundle with the daemon and native WebView wrapper.

```sh
make start         # pick macOS from the prompt
# or directly:
./scripts/start.sh --macos
```

Or manually:

```sh
./scripts/package-macos.sh
open dist/Linea.app
```

The app starts the Go daemon automatically and opens the UI in its own
window. Connects to `http://localhost:8080`.

## 5. iOS (simulator)

```sh
make start         # pick iOS from the prompt
# or directly:
./scripts/start.sh --ios
```

Or manually:

```sh
xcodebuild -project ios/Linea.xcodeproj -scheme Linea \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' \
  -derivedDataPath ios/DerivedData build CODE_SIGNING_ALLOWED=NO

xcrun simctl boot "iPhone 17 Pro"
xcrun simctl install booted ios/DerivedData/Build/Products/Debug-iphonesimulator/Linea.app
xcrun simctl launch booted com.bniladridas.linea
```

Connects to `http://localhost:8080` by default.
Override with `LINEA_SERVER_URL`.

## 6. Android (emulator)

```sh
make start         # pick Android from the prompt
# or directly:
./scripts/start.sh --android
```

Or manually:

```sh
cd android && ./gradlew assembleDebug

emulator -avd Pixel_10_Pro_XL -no-audio &
adb wait-for-device
adb install -r app/build/outputs/apk/debug/app-debug.apk
adb shell am start -n com.bniladridas.linea/.MainActivity
```

Connects to `http://10.0.2.2:8080` (emulator alias for host localhost).

## Release

```sh
make release-check    # pull latest, check Homebrew formula info, upgrade
make install-check    # tap, pull, upgrade, link the Homebrew formula

linea check           # health checks
```

## Stop everything

```sh
make stop
```

Kills the Go server, Android emulator, iOS simulator, and macOS app.
