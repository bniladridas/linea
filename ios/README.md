# ios

Linea iOS app - WebView wrapper that loads the React UI from the Go server.

## Build

```sh
xcodebuild -project Linea.xcodeproj -scheme Linea \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' \
  -derivedDataPath DerivedData build CODE_SIGNING_ALLOWED=NO
```

## Run

```sh
xcrun simctl boot "iPhone 17 Pro"
xcrun simctl install booted DerivedData/Build/Products/Debug-iphonesimulator/Linea.app
xcrun simctl launch booted com.bniladridas.linea
open -a Simulator
```

The app connects to `http://localhost:8080` by default.
Set `LINEA_SERVER_URL` to point at a different host.
