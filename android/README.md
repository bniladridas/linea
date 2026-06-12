# android

Linea Android app - WebView wrapper that loads the React UI from the Go server.

## Build

```sh
./gradlew assembleDebug
```

APK at `app/build/outputs/apk/debug/app-debug.apk`.

## Run on emulator

```sh
emulator -avd Pixel_10_Pro_XL -no-audio &
adb wait-for-device
adb install -r app/build/outputs/apk/debug/app-debug.apk
adb shell am start -n com.bniladridas.linea/.MainActivity
```

The app connects to `http://10.0.2.2:8080` (the host's localhost via emulator routing).
