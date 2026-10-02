## 2.6.2

* **Fixed: Windows build failure `Cannot extract through symlink ... .plugin_symlinks` (#165).** The prebuilt FFmpeg archive was unpacked into the plugin's own source directory. For a package resolved from the pub cache that directory is reached through a symlink, and the extractor refuses to write through it. The archive now unpacks into the application's build directory instead.
* The same change stops the plugin writing ~19 MB of native libraries into the shared pub cache on every platform. `flutter clean` never removed those; to reclaim the space delete `<pub-cache>/hosted/pub.dev/<package>-<version>/{linux,windows}/ffmpeg-kit` or run `dart pub cache repair`. The archive is now fetched once per build configuration rather than once per machine.

## 2.6.1

* **Fixed: `FFmpegKitConfig.getPlatform()` returned `windows` on Linux.** The Linux native layer was reporting the wrong platform name, so any code branching on it took the Windows path. It now returns `linux`. The prebuilt libraries attached to the `8.1.2-<variant>` release have been rebuilt with the fix; nothing else changed.

## 2.6.0

* **Linux support (x86_64).** `flutter run -d linux` and `flutter build linux` now work. Prebuilt FFmpeg **8.1.2** libraries for the `min-gpl` variant are downloaded at build time, the same way the Windows bundle already worked; set `FFMPEGKIT_LOCAL_DIR` (env or CMake cache variable) to build against a self-built bundle instead.
* Linux builds additionally require **`libjson-glib-dev`** (runtime: `libjson-glib`) alongside Flutter's standard Linux prerequisites. It ships with GNOME, so most desktops already have the runtime.

## 2.5.5

* **Fixed: log/statistics/complete callbacks silently dropped in apps with a second `FlutterEngine`** (e.g. a Firebase Messaging background isolate), a 4.4.1+ regression (#163). FFmpegKit's global native callbacks are now registered once and dispatched to every attached engine that is actually listening, instead of being captured by whichever engine attached last. Background-isolate execution (#155) keeps working.

## 2.5.4

* **Fixed: App Store rejection `Invalid architecture ... arm64e slice with the ios 18.5 SDK` (#164).** The iOS device slices of the bundled FFmpeg XCFrameworks are now arm64 only, for both Swift Package Manager (regenerated binary targets + checksums) and CocoaPods (setup script). arm64e was never used by App Store apps, so nothing is lost.
* SPM only: the regenerated iOS/macOS binaries now actually include the zlib/PNG decoders (#105) and VideoToolbox hardware encoders (#148) that the previous release added for CocoaPods and Android but not for the SPM artifacts.

## 2.5.3

* **Fixed: `Decoder (codec png) not found` (#105).** The native FFmpeg builds for this variant were compiled without zlib, which silently removed the PNG/APNG decoders. zlib is now enabled on Android (`com.antonkarpenko:ffmpeg-kit-*:2.2.2`), iOS and macOS, restoring PNG/APNG decoding (image overlays etc.) as in the original arthenica builds.

## 2.5.2

* **iOS/macOS: hardware-accelerated H.264/HEVC encoding via Apple VideoToolbox** (`h264_videotoolbox`, `hevc_videotoolbox`), plus AVFoundation, is now enabled for this variant. Previously only the `full` / `full_gpl` variants shipped VideoToolbox. No patent-encumbered codecs are added and the codec surface is otherwise unchanged (#148).

## 2.5.1

* Fixed the iOS/macOS build error `'ffmpegkit/FFmpegKitConfig.h' file not found` (#88). The framework setup scripts now download and install atomically, verify the archive, and never leave an empty `Frameworks/` directory behind after a failed download; the podspec re-runs setup until the frameworks are actually present. On restricted networks, point `FFMPEG_KIT_IOS_URL` / `FFMPEG_KIT_MACOS_URL` at a mirror of the release zip.
* Docs: made the FFmpeg badge a link and tidied the README badges.

## 2.5.0

* **Swift Package Manager support** (iOS & macOS): the plugin now integrates via Flutter's SPM support (`flutter config --enable-swift-package-manager`; enabled by default from Flutter 3.44) as well as CocoaPods. Native FFmpeg frameworks are consumed as prebuilt, checksum-pinned XCFrameworks with a native arm64 iOS-simulator slice, downloaded from the GitHub release.
* iOS/macOS plugin sources moved to the Flutter SPM plugin layout; CocoaPods integration is unchanged (and still supported).
* Example app now includes runtime integration tests (`example/integration_test/`).

## 2.4.2

* Fixes the native binary wiring for the CVE-2026-8461 (MagicYUV / "PixelSmash") security patch: Android now pulls `com.antonkarpenko:ffmpeg-kit-*:2.2.1` and iOS/macOS fetch the FFmpeg 8.1.2 frameworks (the 8.1.2 rebuild was previously only wired for Windows). Please upgrade from any earlier version.

## 2.4.1

* **Security (CVE-2026-8461):** updated **FFmpeg to v8.1.2** (arthenica `n8.1.2`) to fix a heap out-of-bounds write in the MagicYUV decoder ("PixelSmash", CVSS 8.8). All platforms (Android/iOS/macOS/Windows) rebuilt against FFmpeg 8.1.2; no API changes.

## 2.4.0

* Updated **FFmpeg to v8.1.1** (arthenica `n8.1.1`), up from 8.0.0.
* iOS/macOS `.xcframework`s, the Android Maven library (`com.antonkarpenko:ffmpeg-kit-*`), and the Windows binaries are now built against FFmpeg 8.1.1.

## 2.3.2

* Android: broadened the bundled ProGuard/R8 consumer rules to keep **all** `com.antonkarpenko.ffmpegkit.**` classes (plus `-dontwarn`), not just the JNI entry points. Fully prevents R8 from stripping FFmpegKit in release builds — fixes the release-only white screen / `channel-error` (FFmpegKit failing to initialise, which cascades into errors like `shared_preferences` `getAll`). Closes #158.

## 2.3.1

* Android: ship ProGuard/R8 **consumer rules** (`consumer-rules.pro`, applied automatically via `consumerProguardFiles`) that keep the FFmpegKit JNI bindings — `FFmpegKitConfig` native/callback methods and `AbiDetect`. Prevents release-mode crashes such as `Bad JNI version returned from JNI_OnLoad` without requiring any ProGuard rules in your app. Thanks @niclasEX (#133).

## 2.3.0

* Fixed **iOS Simulator on Apple Silicon** (`arm64`) support — required for Xcode 26 / iOS 26+ simulators. Builds previously failed with *"The following target(s) do not support arm64 architecture, which is a requirement for Apple Silicon iOS 26+ simulators"*.
* The downloaded iOS `.framework` bundles are now converted to `.xcframework`s at build time (`scripts/setup_ios.sh`), exposing a native `ios-arm64_x86_64-simulator` slice alongside the `ios-arm64_arm64e` device slice. The simulator `arm64` slice is produced by retagging the device `arm64` slice's Mach-O build-version platform to `iOS-Simulator` via `vtool`.
* The iOS podspec now vendors `.xcframework`s and no longer excludes `arm64` for the simulator (`EXCLUDED_ARCHS[sdk=iphonesimulator*]` is now `i386` only).
* If you previously added `config.build_settings["EXCLUDED_ARCHS[sdk=iphonesimulator*]"] = "arm64"` to your app's `ios/Podfile` `post_install` hook as a workaround, **remove it** — it now prevents the simulator `arm64` slice from linking.

## 2.2.0

* Added **Windows** support (x86_64) for the `min-gpl` FFmpeg 8.0 variant (bare FFmpeg plus the four GPL video libraries: `x264`, `x265`, `xvid`, `vid.stab`).
* Native libraries are downloaded automatically at build time from the `8.0.0-min-gpl` release (`ffmpeg-kit-windows-x86_64-min-gpl-8.0.0.zip`); for local development point `FFMPEGKIT_LOCAL_DIR` (CMake cache var or environment) at a locally built bundle.
* The bundled MinGW DLLs are rebased/ASLR-adjusted at build time and the full runtime dependency closure is shipped so the plugin loads standalone.
* Shell scripts are forced to LF via `.gitattributes` so the published package never carries Windows CRLF line endings (avoids the iOS/macOS `pod install` `/bin/bash^M: bad interpreter` failure).

## 2.1.1

* Added ProGuard rules to support release builds
* Enabled ProGuard for Android release builds

## 2.1.0

* Fixed the FFmpeg 8.0 compatibility issue across all platforms. The problem was that `all_channel_counts` was being set AFTER the filter was created, but FFmpeg 8.0 requires it to be set DURING filter creation.

## 2.0.0

* FFmpeg `v8.0.0` with [all the sweet perks](https://ffmpeg.org/index.html#news)

## 1.1.0

* Added proguard-rules.pro to keep `ffmpeg` dependencies when minification is enabled
* Upgraded `freetype` from **2.13.0** to **2.13.3**
* Upgraded `harfbuzz` from **8.0.1** to **11.3.3**
* Upgraded `fontconfig` from **2.16.2** to 2.17.1
* Added support for `harfbuzz` library in order to support `drawtext` filter
* Fixed missing `libunibreak` for `libass.sh`
* Downgraded required Kotlin version to `v1.8.22`
* Upgraded com.android.library from `8.11.1` to `8.12.0`

## 1.0.0

* Initial release
* FFmpeg version 7.1.1
* Removed bundled Android FFmpeg (jniLibs, cpp, bindings)
* Added FFmpeg min using new Maven Central package