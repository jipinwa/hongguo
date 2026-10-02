import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

class VideoEnhancementAssets {
  static const root = 'assets/video_enhancement';
  static const ravuRgb = 'ravu-r2-rgb.hook';
  static const ravuLuma = 'ravu-lite-ar-r2.hook';
  static const fsrcnnx = 'FSRCNNX_x2_8-0-4-1.glsl';
  static const anime = 'Anime4K_Upscale_Denoise_CNN_x2_S.glsl';
  static final _pending = <String, Future<String>>{};

  static Future<String> prepare(String name) async {
    final existing = _pending[name];
    if (existing != null) return existing;
    final future = _prepare(name);
    _pending[name] = future;
    try {
      return await future;
    } finally {
      if (identical(_pending[name], future)) _pending.remove(name);
    }
  }

  static Future<String> _prepare(String name) async {
    if (!RegExp(r'^[A-Za-z0-9_.-]+$').hasMatch(name)) {
      throw const FormatException('增强资源名称无效');
    }
    final manifest =
        jsonDecode(await rootBundle.loadString('$root/manifest.json')) as Map;
    final entry = (manifest['shaders'] as Map)[name] as Map?;
    final digest = entry?['sha256'] as String? ?? '';
    final length = entry?['bytes'] as int? ?? 0;
    if (!RegExp(r'^[a-f0-9]{64}$').hasMatch(digest) ||
        length <= 0 ||
        length > 4 * 1024 * 1024) {
      throw const FormatException('增强资源清单无效');
    }
    final directory = Directory(
      path.join(
        (await getApplicationSupportDirectory()).path,
        'video_enhancement',
        'v1',
      ),
    );
    await directory.create(recursive: true);
    final destination = File(path.join(directory.path, '$digest-$name'));
    if (await destination.exists() && await destination.length() == length) {
      if (sha256.convert(await destination.readAsBytes()).toString() ==
          digest) {
        return destination.path;
      }
    }
    final data = await rootBundle.load('$root/$name');
    final bytes = data.buffer.asUint8List(
      data.offsetInBytes,
      data.lengthInBytes,
    );
    if (bytes.length != length || sha256.convert(bytes).toString() != digest) {
      throw const FormatException('增强资源校验失败');
    }
    final temporary = File(
      '${destination.path}.${DateTime.now().microsecondsSinceEpoch}.tmp',
    );
    File? previous;
    try {
      await temporary.writeAsBytes(bytes, flush: true);
      if (Platform.isWindows && await destination.exists()) {
        previous = await destination.rename('${temporary.path}.previous');
      }
      await temporary.rename(destination.path);
      if (previous != null && await previous.exists()) await previous.delete();
    } catch (_) {
      if (previous != null &&
          await previous.exists() &&
          !await destination.exists()) {
        await previous.rename(destination.path);
      }
      rethrow;
    } finally {
      if (await temporary.exists()) await temporary.delete();
    }
    return destination.path;
  }

  static void registerLicenses() {
    LicenseRegistry.addLicense(() async* {
      yield LicenseEntryWithLineBreaks(
        ['RAVU', 'FSRCNNX'],
        '${await rootBundle.loadString('$root/LGPL-3.0.txt')}\n\n'
        '${await rootBundle.loadString('$root/GPL-3.0.txt')}',
      );
      yield LicenseEntryWithLineBreaks([
        'Anime4K',
      ], await rootBundle.loadString('$root/Anime4K-LICENSE.txt'));
    });
  }
}
