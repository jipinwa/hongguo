<div align="center">

# 🎬 FFmpegKit for Flutter — Minimal-GPL

**Run `FFmpeg` & `FFprobe` on Android, iOS, macOS, Windows and Linux from a single Dart API.**

_A maintained fork of the original [FFmpegKit](https://github.com/arthenica/ffmpeg-kit/tree/main/flutter/flutter), updated for the Android V2 embedding and Flutter 3+._

<p align="center">
  <a href="https://pub.dev/packages/ffmpeg_kit_flutter_new_min_gpl"><img src="https://img.shields.io/badge/pub-2.5.5-blue?logo=dart" alt="pub version"></a>
  <a href="https://ffmpeg.org/index.html#news"><img src="https://img.shields.io/badge/FFmpeg-8.1.2-green?logo=ffmpeg&logoColor=white" alt="FFmpeg 8.1.2"></a>
  <a href="https://discord.gg/8NVwykjA"><img src="https://img.shields.io/discord/1387108888452665427?logo=discord&logoColor=white&label=Join+Us&color=blueviolet" alt="Discord"></a>
  <a href="https://buymeacoffee.com/sk3llo" target="_blank"><img src="https://cdn.buymeacoffee.com/buttons/default-orange.png" alt="Buy Me A Coffee" height="21" width="114"></a>
</p>

</div>

---

## ✨ Features

- 🧰 **Full toolkit** — both `FFmpeg` and `FFprobe`, with per-session logs, statistics and callbacks.
- 📱 **Five platforms** — `Android`, `iOS`, `macOS`, `Windows` (x86_64), and `Linux` (x86_64) behind one API.
- 🎞️ **FFmpeg `v8.1.2`** — full command-line-compatible build.
- 🗂️ **Android SAF** — process Storage Access Framework URIs directly.
- 📚 **External libraries** — see the [enabled-libraries table](#-enabled-libraries) below.
- 🔄 **Modernised bindings** for the latest Flutter and Android/macOS toolchains.
- 📦 **Swift Package Manager** — iOS & macOS integrate via SPM (Flutter 3.24+) or CocoaPods, with prebuilt checksum-pinned XCFrameworks.

## 📦 Install

```yaml
dependencies:
  ffmpeg_kit_flutter_new_min_gpl: ^2.6.0
```

```dart
import 'package:ffmpeg_kit_flutter_new_min_gpl/ffmpeg_kit.dart';
```

## 🎯 Choose your package

There are **eight** `ffmpeg-kit` packages — pick the smallest one that covers the codecs and features you need. `GPL`-licensed codecs (`x264`, `x265`, `xvidcore`, `vid.stab`) are only in the `-gpl` packages.

| Package | Best for |
|---|---|
| [`_min`](https://pub.dev/packages/ffmpeg_kit_flutter_new_min) | Smallest build — core FFmpeg only |
| [`_min_gpl`](https://pub.dev/packages/ffmpeg_kit_flutter_new_min_gpl) **(this one)** | Minimal **+ GPL** codecs (x264/x265/xvid/vid.stab) |
| [`_https`](https://pub.dev/packages/ffmpeg_kit_flutter_new_https) | Adds TLS (`gnutls`) for `https://` inputs |
| [`_https_gpl`](https://pub.dev/packages/ffmpeg_kit_flutter_new_https_gpl) | HTTPS **+ GPL** codecs |
| [`_audio`](https://pub.dev/packages/ffmpeg_kit_flutter_new_audio) | Audio-focused (mp3, opus, vorbis, speex, …) |
| [`_video`](https://pub.dev/packages/ffmpeg_kit_flutter_new_video) | Video-focused (dav1d, vpx, theora, webp, …) |
| [`_full`](https://pub.dev/packages/ffmpeg_kit_flutter_new_full) | Everything except GPL codecs |
| [`ffmpeg_kit_flutter_new`](https://pub.dev/packages/ffmpeg_kit_flutter_new) | **Full + GPL** — every library |

## 🚀 Quick start

```dart
import 'package:ffmpeg_kit_flutter_new_min_gpl/ffmpeg_kit.dart';

final session = await FFmpegKit.execute('-i input.mp4 -c:v mpeg4 output.mp4');
final returnCode = await session.getReturnCode();

if (ReturnCode.isSuccess(returnCode)) {
  // ✅ done
} else if (ReturnCode.isCancel(returnCode)) {
  // ⏹️ cancelled
} else {
  // ❌ error — inspect await session.getLogs()
}
```

## 🧩 Enabled libraries

Below is which system and external libraries each package enables. Some parts of `FFmpeg` are `GPL`-licensed and are only present in the `GPL` packages.

<table>
<thead>
<tr>
<th align="center"></th>
<th align="center"><sup>min</sup></th>
<th align="center"><sup>min-gpl</sup></th>
<th align="center"><sup>https</sup></th>
<th align="center"><sup>https-gpl</sup></th>
<th align="center"><sup>audio</sup></th>
<th align="center"><sup>video</sup></th>
<th align="center"><sup>full</sup></th>
<th align="center"><sup>full-gpl</sup></th>
</tr>
</thead>
<tbody>
<tr>
<td align="center"><sup>external libraries</sup></td>
<td align="center">-</td>
<td align="center"><sup>vid.stab</sup><br><sup>x264</sup><br><sup>x265</sup><br><sup>xvidcore</sup></td>
<td align="center"><sup>gmp</sup><br><sup>gnutls</sup></td>
<td align="center"><sup>gmp</sup><br><sup>gnutls</sup><br><sup>vid.stab</sup><br><sup>x264</sup><br><sup>x265</sup><br><sup>xvidcore</sup></td>
<td align="center"><sup>lame</sup><br><sup>libilbc</sup><br><sup>libvorbis</sup><br><sup>opencore-amr</sup><br><sup>opus</sup><br><sup>shine</sup><br><sup>soxr</sup><br><sup>speex</sup><br><sup>twolame</sup><br><sup>vo-amrwbenc</sup></td>
<td align="center"><sup>dav1d</sup><br><sup>fontconfig</sup><br><sup>freetype</sup><br><sup>fribidi</sup><br><sup>kvazaar</sup><br><sup>libass</sup><br><sup>libiconv</sup><br><sup>libtheora</sup><br><sup>libvpx</sup><br><sup>libwebp</sup><br><sup>snappy</sup><br><sup>zimg</sup></td>
<td align="center"><sup>dav1d</sup><br><sup>fontconfig</sup><br><sup>freetype</sup><br><sup>fribidi</sup><br><sup>gmp</sup><br><sup>gnutls</sup><br><sup>kvazaar</sup><br><sup>lame</sup><br><sup>libass</sup><br><sup>libiconv</sup><br><sup>libilbc</sup><br><sup>libtheora</sup><br><sup>libvorbis</sup><br><sup>libvpx</sup><br><sup>libwebp</sup><br><sup>libxml2</sup><br><sup>opencore-amr</sup><br><sup>opus</sup><br><sup>shine</sup><br><sup>snappy</sup><br><sup>soxr</sup><br><sup>speex</sup><br><sup>twolame</sup><br><sup>vo-amrwbenc</sup><br><sup>zimg</sup></td>
<td align="center"><sup>dav1d</sup><br><sup>fontconfig</sup><br><sup>freetype</sup><br><sup>fribidi</sup><br><sup>gmp</sup><br><sup>gnutls</sup><br><sup>kvazaar</sup><br><sup>lame</sup><br><sup>libass</sup><br><sup>libiconv</sup><br><sup>libilbc</sup><br><sup>libtheora</sup><br><sup>libvorbis</sup><br><sup>libvpx</sup><br><sup>libwebp</sup><br><sup>libxml2</sup><br><sup>opencore-amr</sup><br><sup>opus</sup><br><sup>shine</sup><br><sup>snappy</sup><br><sup>soxr</sup><br><sup>speex</sup><br><sup>twolame</sup><br><sup>vid.stab</sup><br><sup>vo-amrwbenc</sup><br><sup>x264</sup><br><sup>x265</sup><br><sup>xvidcore</sup><br><sup>zimg</sup></td>
</tr>
<tr>
<td align="center"><sup>android system libraries</sup></td>
<td align="center" colspan=8><sup>zlib</sup><br><sup>MediaCodec</sup></td>
</tr>
<tr>
<td align="center"><sup>ios system libraries</sup></td>
<td align="center" colspan=8><sup>bzip2</sup><br><sup>AudioToolbox</sup><br><sup>AVFoundation</sup><br><sup>iconv</sup><br><sup>zlib</sup></td>
</tr>
<tr>
<td align="center"><sup>ios VideoToolbox</sup></td>
<td align="center"><sup>VideoToolbox</sup></td>
<td align="center"><sup>VideoToolbox</sup></td>
<td align="center">-</td>
<td align="center">-</td>
<td align="center">-</td>
<td align="center">-</td>
<td align="center"><sup>VideoToolbox</sup></td>
<td align="center"><sup>VideoToolbox</sup></td>
</tr>
<tr>
<td align="center"><sup>macos system libraries</sup></td>
<td align="center" colspan=8><sup>bzip2</sup><br><sup>AudioToolbox</sup><br><sup>AVFoundation</sup><br><sup>Core Image</sup><br><sup>iconv</sup><br><sup>OpenCL</sup><br><sup>OpenGL</sup><br><sup>VideoToolbox</sup><br><sup>zlib</sup></td>
</tr>
</tbody>
</table>

## 📱 Platform support

<table align="center">
  <thead>
    <tr>
      <th align="center">Android<br>API Level</th>
      <th align="center">Kotlin<br>Min Version</th>
      <th align="center">iOS<br>Min Target</th>
      <th align="center">macOS<br>Min Target</th>
      <th align="center">Windows</th>
      <th align="center">Linux</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td align="center">24</td>
      <td align="center">1.8.22</td>
      <td align="center">14.0</td>
      <td align="center">10.15</td>
      <td align="center">10+ (x86_64)</td>
      <td align="center">x86_64</td>
    </tr>
  </tbody>
</table>

**Architectures** — Android: `arm-v7a`, `arm-v7a-neon`, `arm64-v8a`, `x86`, `x86_64` · iOS: `arm64` (device) + `arm64`/`x86_64` (simulator, incl. Apple Silicon / Xcode 26), shipped as `.xcframework` · macOS: `arm64`, `x86_64`.

> **Windows:** prebuilt FFmpeg **8.1.2** libraries (x86_64) are downloaded automatically at build time. For local development against a self-built bundle, set `FFMPEGKIT_LOCAL_DIR` (env or CMake cache variable) to the bundle directory before `flutter run/build windows`.

> **Linux:** prebuilt FFmpeg **8.1.2** libraries (x86_64) are downloaded automatically at build time, exactly as on Windows; `FFMPEGKIT_LOCAL_DIR` works the same way. In addition to Flutter's standard Linux prerequisites (`clang`, `cmake`, `ninja-build`, `pkg-config`, `libgtk-3-dev`), the plugin needs **`libjson-glib-dev`**:
>
> ```bash
> sudo apt-get install libjson-glib-dev   # Debian/Ubuntu
> sudo dnf install json-glib-devel        # Fedora
> ```


## 📖 Usage

<details open>
<summary><strong>Execute a command & read the result</strong></summary>

```dart
import 'package:ffmpeg_kit_flutter_new_min_gpl/ffmpeg_kit.dart';

FFmpegKit.execute('-i file1.mp4 -c:v mpeg4 file2.mp4').then((session) async {
  final returnCode = await session.getReturnCode();
  if (ReturnCode.isSuccess(returnCode)) {
    // SUCCESS
  } else if (ReturnCode.isCancel(returnCode)) {
    // CANCEL
  } else {
    // ERROR
  }
});
```

</details>

<details>
<summary><strong>Inspect a session</strong></summary>

```dart
FFmpegKit.execute('-i file1.mp4 -c:v mpeg4 file2.mp4').then((session) async {
  final sessionId = session.getSessionId();
  final command = session.getCommand();
  final state = await session.getState();
  final returnCode = await session.getReturnCode();
  final duration = await session.getDuration();
  final output = await session.getOutput();
  final logs = await session.getLogs();
  final statistics = await (session as FFmpegSession).getStatistics();
});
```

</details>

<details>
<summary><strong>Async execution with callbacks</strong></summary>

```dart
FFmpegKit.executeAsync('-i file1.mp4 -c:v mpeg4 file2.mp4', (Session session) async {
  // CALLED WHEN SESSION IS EXECUTED
}, (Log log) {
  // CALLED WHEN SESSION PRINTS LOGS
}, (Statistics statistics) {
  // CALLED WHEN SESSION GENERATES STATISTICS
});
```

</details>

<details>
<summary><strong>FFprobe & media information</strong></summary>

```dart
FFprobeKit.getMediaInformation('<file path or url>').then((session) async {
  final information = await session.getMediaInformation();
});
```

</details>

<details>
<summary><strong>Cancel sessions</strong></summary>

```dart
FFmpegKit.cancel();          // stop all sessions
FFmpegKit.cancel(sessionId); // stop a specific session
```

</details>

<details>
<summary><strong>Android — Storage Access Framework (SAF)</strong></summary>

```dart
// Reading a file
FFmpegKitConfig.selectDocumentForRead('*/*').then((uri) {
  FFmpegKitConfig.getSafParameterForRead(uri!).then((safUrl) {
    FFmpegKit.executeAsync("-i ${safUrl!} -c:v mpeg4 file2.mp4");
  });
});

// Writing to a file
FFmpegKitConfig.selectDocumentForWrite('video.mp4', 'video/*').then((uri) {
  FFmpegKitConfig.getSafParameterForWrite(uri!).then((safUrl) {
    FFmpegKit.executeAsync("-i file1.mp4 -c:v mpeg4 ${safUrl}");
  });
});
```

</details>

<details>
<summary><strong>Global callbacks & fonts</strong></summary>

```dart
FFmpegKitConfig.enableLogCallback((log) { final message = log.getMessage(); });
FFmpegKitConfig.enableStatisticsCallback((statistics) { final size = statistics.getSize(); });
FFmpegKitConfig.setFontDirectoryList(["/system/fonts", "/System/Library/Fonts", "<folder with fonts>"]);
```

</details>

## 📄 License

Licensed under **LGPL 3.0** by default. This package additionally includes `GPL v3.0`-licensed components (`x264`, `x265`, `xvidcore`, `vid.stab`) and is therefore effectively **GPL v3.0**.

## 💬 Community & support

- 💙 [Join the Discord](https://discord.gg/8NVwykjA)
- ☕ [Buy me a coffee](https://buymeacoffee.com/sk3llo)
- 🐛 [Report an issue](https://github.com/sk3llo/ffmpeg_kit_flutter/issues)
