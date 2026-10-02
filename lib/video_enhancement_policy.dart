import 'dart:math' as math;
import 'dart:ui';

import 'package:media_kit/media_kit.dart';

import 'video_enhancement_preferences.dart';
import 'video_output_size.dart';

enum VideoEnhancementBackend {
  original(0, '原画'),
  scaling(1, '高质量缩放'),
  ravu(2, '轻量增强'),
  fsrcnnx(3, '通用神经超分'),
  anime(3, '动漫神经超分'),
  hardware(4, '硬件增强');

  const VideoEnhancementBackend(this.cost, this.label);
  final int cost;
  final String label;
}

class VideoEnhancementPower {
  const VideoEnhancementPower({
    this.batterySaver = false,
    this.onBattery = true,
    this.thermalStatus = 0,
    this.headroom,
    this.lowMemory = false,
    this.gles = 0,
  });
  final bool batterySaver;
  final bool onBattery;
  final int thermalStatus;
  final double? headroom;
  final bool lowMemory;
  final int gles;

  factory VideoEnhancementPower.fromMap(Map value) {
    final headroom = (value['headroom'] as num?)?.toDouble();
    return VideoEnhancementPower(
      batterySaver: value['batterySaver'] == true,
      onBattery: value['onBattery'] != false,
      thermalStatus: (value['thermalStatus'] as num?)?.toInt() ?? 0,
      headroom: headroom != null && headroom.isFinite ? headroom : null,
      lowMemory: value['lowMemory'] == true,
      gles: (value['gles'] as num?)?.toInt() ?? 0,
    );
  }
}

class VideoEnhancementDecision {
  const VideoEnhancementDecision(this.backend, this.output, this.reason);
  final VideoEnhancementBackend backend;
  final Size? output;
  final String reason;

  @override
  bool operator ==(Object other) =>
      other is VideoEnhancementDecision &&
      backend == other.backend &&
      output == other.output &&
      reason == other.reason;

  @override
  int get hashCode => Object.hash(backend, output, reason);
}

bool videoHasAnimationTags(String category, List<String> tags) => RegExp(
  r'漫剧|动漫|动画|二次元|\banime\b|\banimation\b|comic_series',
  caseSensitive: false,
).hasMatch([category, ...tags].join(' '));

String videoEnhancementPixelFormat(VideoParams parameters) {
  final hardware = parameters.hwPixelformat?.trim() ?? '';
  return (hardware.isNotEmpty ? hardware : parameters.pixelformat ?? '')
      .toLowerCase();
}

bool videoHasSafeEnhancementColor(
  VideoParams parameters, {
  bool opaque8Bit = false,
}) {
  final gamma = parameters.gamma?.toLowerCase() ?? '';
  final primaries = parameters.primaries?.toLowerCase() ?? '';
  final format = videoEnhancementPixelFormat(parameters);
  return {
        'bt.1886',
        'srgb',
        'gamma1.8',
        'gamma2.2',
        'gamma2.8',
      }.contains(gamma) &&
      {
        'bt.709',
        'bt.601-525',
        'bt.601-625',
        'bt.470bg',
        'smpte-170m',
      }.contains(primaries) &&
      (opaque8Bit && format == 'mediacodec' ||
          {
            'yuv420p',
            'yuvj420p',
            'yuv422p',
            'yuvj422p',
            'yuv444p',
            'yuvj444p',
            'nv12',
            'nv21',
            'yuyv422',
            'uyvy422',
            'rgb0',
            'bgr0',
            '0rgb',
            '0bgr',
            'rgba',
            'bgra',
            'rgb24',
            'bgr24',
          }.contains(format)) &&
      (parameters.sigPeak == null || parameters.sigPeak! <= 1.01);
}

VideoEnhancementDecision chooseVideoEnhancement({
  required VideoEnhancementPreferences preferences,
  required VideoParams parameters,
  required Size viewport,
  required bool ready,
  required bool android,
  required bool television,
  required bool gpu,
  required bool luma,
  required bool hardware,
  required bool animation,
  required VideoEnhancementPower power,
  required double rate,
  required int costLimit,
  required Set<VideoEnhancementBackend> rejected,
  bool opaque8Bit = false,
}) {
  const original = VideoEnhancementBackend.original;
  if (preferences.mode == VideoEnhancementMode.off) {
    return const VideoEnhancementDecision(original, null, '画质增强已关闭');
  }
  final source = videoDisplaySize(parameters);
  if (!ready ||
      source == null ||
      viewport.isEmpty ||
      !viewport.width.isFinite ||
      !viewport.height.isFinite) {
    return const VideoEnhancementDecision(original, null, '等待播放画面');
  }
  if (!videoHasSafeEnhancementColor(parameters, opaque8Bit: opaque8Bit)) {
    return const VideoEnhancementDecision(original, null, '当前色彩或位深保持原画');
  }
  if (!gpu) {
    return const VideoEnhancementDecision(original, null, '当前渲染环境保持原画');
  }
  if (costLimit < 1 ||
      rejected.contains(VideoEnhancementBackend.scaling) ||
      power.thermalStatus >= 3 ||
      (power.headroom ?? 0) >= 1) {
    return const VideoEnhancementDecision(original, null, '为保持流畅，已暂停增强');
  }
  final targetScale = math.min(
    viewport.width / source.width,
    viewport.height / source.height,
  );
  final maxPixels = android || television ? 2073600.0 : 8294400.0;
  final maxDimension = android || television ? 4096.0 : 8192.0;
  final scale = math.min(
    targetScale,
    math.min(
      2.0,
      math.min(
        math.sqrt(maxPixels / (source.width * source.height)),
        maxDimension / math.max(source.width, source.height),
      ),
    ),
  );
  if (scale <= 0) {
    return const VideoEnhancementDecision(original, null, '等待显示尺寸');
  }
  final output = Size(
    math.max(2, (source.width * scale).round() ~/ 2 * 2).toDouble(),
    math.max(2, (source.height * scale).round() ~/ 2 * 2).toDouble(),
  );
  final scaling = VideoEnhancementDecision(
    VideoEnhancementBackend.scaling,
    output,
    targetScale <= 1 ? '当前尺寸无需超分，使用高质量缩放' : '',
  );
  final ratio = math.min(
    output.width / source.width,
    output.height / source.height,
  );
  if (ratio <= 1.2 || preferences.mode == VideoEnhancementMode.economy) {
    return scaling;
  }
  if (android && preferences.mode == VideoEnhancementMode.automatic) {
    return VideoEnhancementDecision(
      VideoEnhancementBackend.scaling,
      output,
      '手机自动档优先流畅，使用高质量缩放',
    );
  }
  if (power.batterySaver ||
      rate >= 1.75 ||
      power.thermalStatus >= 2 ||
      (power.headroom ?? 0) >= .85) {
    return VideoEnhancementDecision(
      VideoEnhancementBackend.scaling,
      output,
      rate >= 1.75 ? '倍速播放，已降低增强' : '省电或温控限制，已降低增强',
    );
  }
  final squarePixels =
      (parameters.par ?? 1) > .99 && (parameters.par ?? 1) < 1.01;
  if (!squarePixels ||
      (parameters.rotate ?? 0) % 180 != 0 ||
      android && power.gles < 0x30000) {
    return scaling;
  }
  final pixels = (parameters.w ?? 0) * (parameters.h ?? 0);
  if (pixels <= 0) return scaling;
  final limit = math.min(
    costLimit,
    television || power.lowMemory || android ? 2 : 4,
  );
  bool allowed(VideoEnhancementBackend backend) =>
      backend.cost <= limit && !rejected.contains(backend);
  final memory = android ? (power.lowMemory ? 64 : 128) : 512;
  final neuralMemory =
      (pixels * 8 * 18 + output.width * output.height * 8 * 3) / (1024 * 1024);
  final neuralSize = !android;
  final neural = neuralSize && neuralMemory <= memory && rate < 1.5;
  if (preferences.mode == VideoEnhancementMode.quality &&
      !animation &&
      hardware &&
      !power.onBattery &&
      allowed(VideoEnhancementBackend.hardware)) {
    return VideoEnhancementDecision(
      VideoEnhancementBackend.hardware,
      output,
      '驱动增强效果以原画对比为准',
    );
  }
  if (animation && neural && allowed(VideoEnhancementBackend.anime)) {
    return VideoEnhancementDecision(VideoEnhancementBackend.anime, output, '');
  }
  if (preferences.mode == VideoEnhancementMode.quality &&
      !animation &&
      luma &&
      neural &&
      !android &&
      ratio > 1.31 &&
      allowed(VideoEnhancementBackend.fsrcnnx)) {
    return VideoEnhancementDecision(
      VideoEnhancementBackend.fsrcnnx,
      output,
      '',
    );
  }
  final ravuMemory =
      (pixels * 8 * 10 + output.width * output.height * 8 * 3) / (1024 * 1024);
  if (ratio > 1.42 &&
      ravuMemory <= memory &&
      allowed(VideoEnhancementBackend.ravu)) {
    return VideoEnhancementDecision(VideoEnhancementBackend.ravu, output, '');
  }
  return scaling;
}
