# android

Linea Android app - WebView wrapper with bundled Go server binary.

The app bundles `linea-android-arm64` (cross-compiled Go binary) as an asset,
extracts it at runtime, spawns it as a subprocess, then loads the React UI.

## Build

From the project root:

```sh
make android-check
```

Or manually:
```sh
GOOS=android GOARCH=arm64 CGO_ENABLED=0 go build -o app/src/main/assets/linea-android-arm64 ../backend/cmd/server
cd android && ./gradlew assembleDebug
```

APK at `app/build/outputs/apk/debug/app-debug.apk`.

## Run on emulator

```sh
emulator -avd Pixel_10_Pro_XL -no-audio &
adb wait-for-device
adb install -r app/build/outputs/apk/debug/app-debug.apk
adb shell am start -n com.bniladridas.linea/.MainActivity
```

The server runs on-device at `127.0.0.1:8080`.
