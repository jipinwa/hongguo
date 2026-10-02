import 'dart:async';
import 'dart:math';

import 'package:flutter/services.dart';

import 'core_bridge.dart';
import 'app_build.dart';

class MediaProbe {
  MediaProbe(this.raw);
  final Map<String, dynamic> raw;
  List<Map<String, dynamic>> get streams => (raw['streams'] as List? ?? [])
      .whereType<Map>()
      .map((s) => Map<String, dynamic>.from(s))
      .toList();
  Map<String, dynamic> get video =>
      streams.firstWhere((s) => s['codec_type'] == 'video', orElse: () => {});
  Map<String, dynamic> get audio =>
      streams.firstWhere((s) => s['codec_type'] == 'audio', orElse: () => {});
  double get duration =>
      double.tryParse('${(raw['format'] as Map?)?['duration']}') ?? 0;
  String get videoCodec => video['codec_name'] as String? ?? '';
  bool get hasAudio => audio.isNotEmpty;
  String get videoSignature => [
    video['codec_name'],
    video['width'],
    video['height'],
    video['pix_fmt'],
    video['sample_aspect_ratio'] ?? '1:1',
    video['color_transfer'] ?? 'unknown',
    video['color_primaries'] ?? 'unknown',
  ].join('|');
  String get audioSignature => !hasAudio
      ? 'none'
      : [
          audio['codec_name'],
          audio['profile'] ?? 'unknown',
          audio['sample_rate'],
          audio['channels'],
          audio['channel_layout'],
          audio['extradata_hash'] ?? 'unknown',
        ].join('|');
}

class MergePlan {
  MergePlan._(
    this.video,
    this.audio,
    this.videoChanges,
    this.audioChanges,
    this.transportStream,
    this.canonicalAac,
  );
  final MediaProbe video;
  final MediaProbe? audio;
  final List<bool> videoChanges;
  final List<bool> audioChanges;
  final bool transportStream;
  final bool canonicalAac;
  int get videoTranscodes => videoChanges.where((change) => change).length;
  int get audioTranscodes => audioChanges.where((change) => change).length;

  static MediaProbe _majority(
    List<MediaProbe> probes,
    String Function(MediaProbe) signature,
  ) {
    final groups = <String, List<MediaProbe>>{};
    for (final probe in probes) {
      groups.putIfAbsent(signature(probe), () => []).add(probe);
    }
    final ordered = groups.values.toList()
      ..sort((a, b) {
        final count = b.length.compareTo(a.length);
        return count != 0
            ? count
            : b
                  .fold<double>(0, (s, p) => s + p.duration)
                  .compareTo(a.fold<double>(0, (s, p) => s + p.duration));
      });
    return ordered.first.first;
  }

  factory MergePlan.create(List<MediaProbe> probes) {
    if (probes.length < 2 ||
        probes.any((p) => p.videoCodec.isEmpty || p.duration <= 0)) {
      throw AppFailure('请至少选择两集完整视频');
    }
    final video = _majority(probes, (p) => p.videoSignature);
    final withAudio = probes.where((p) => p.hasAudio).toList();
    final audio = withAudio.isEmpty
        ? null
        : _majority(withAudio, (p) => p.audioSignature);
    final vc = probes
        .map((p) => p.videoSignature != video.videoSignature)
        .toList();
    var ac = probes
        .map((p) => audio != null && p.audioSignature != audio.audioSignature)
        .toList();
    final canonicalAac =
        audio?.audio['codec_name'] == 'aac' && ac.any((change) => change);
    if (canonicalAac) ac = List.filled(probes.length, true);
    if (vc.any((v) => v) && !{'h264', 'hevc'}.contains(video.videoCodec)) {
      throw AppFailure('多数分集为 ${video.videoCodec}，当前不能将其他格式转换为此编码；原文件已保留。');
    }
    for (var i = 0; i < probes.length; i++) {
      if (!vc[i]) continue;
      final source = probes[i].video['color_transfer'];
      final target = video.video['color_transfer'];
      if ((source == 'smpte2084' ||
              source == 'arib-std-b67' ||
              target == 'smpte2084' ||
              target == 'arib-std-b67') &&
          source != target) {
        throw AppFailure('分集包含不同 HDR 色彩格式，暂不自动转换，避免改变画面色彩。');
      }
    }
    if (audio != null &&
        ac.any((a) => a) &&
        !{
          'aac',
          'ac3',
          'eac3',
          'flac',
          'alac',
          'mp2',
          'pcm_s16le',
          'pcm_s24le',
        }.contains(audio.audio['codec_name'])) {
      throw AppFailure('多数音轨格式暂不支持统一编码，可先按分集导出。');
    }
    final transport =
        {'h264', 'hevc'}.contains(video.videoCodec) &&
        (audio == null ||
            {'aac', 'ac3', 'eac3', 'mp2'}.contains(audio.audio['codec_name']));
    if (!transport &&
        probes.any(
          (p) => p.video['extradata_hash'] != video.video['extradata_hash'],
        )) {
      throw AppFailure('视频编码参数不适合直接拼接，可保留分集并导出 Emby。');
    }
    return MergePlan._(video, audio, vc, ac, transport, canonicalAac);
  }

  List<String> normalizeArguments(
    String input,
    String output,
    MediaProbe probe,
    int index,
  ) {
    final args = <String>['-i', input];
    final addSilence = audio != null && !probe.hasAudio;
    if (addSilence) {
      final layout = intValue(audio!.audio['channels']) == 1
          ? 'mono'
          : intValue(audio!.audio['channels']) == 2
          ? 'stereo'
          : audio!.audio['channel_layout'] as String? ?? 'stereo';
      args.addAll([
        '-f',
        'lavfi',
        '-i',
        'anullsrc=r=${audio!.audio['sample_rate']}:cl=$layout',
      ]);
    }
    args.addAll(['-map', '0:v:0']);
    if (audio != null) args.addAll(['-map', addSilence ? '1:a:0' : '0:a:0']);
    if (videoChanges[index]) {
      final width = intValue(video.video['width']),
          height = intValue(video.video['height']);
      final pixel = video.video['pix_fmt'] as String? ?? 'yuv420p';
      final aspect = (video.video['sample_aspect_ratio'] as String? ?? '1:1')
          .replaceAll(':', '/');
      args.addAll([
        '-c:v',
        video.videoCodec == 'hevc' ? 'libx265' : 'libx264',
        '-preset',
        'veryfast',
        '-crf',
        '18',
        '-threads',
        '2',
        '-vf',
        'scale=$width:$height:force_original_aspect_ratio=decrease,pad=$width:$height:(ow-iw)/2:(oh-ih)/2,setsar=$aspect,format=$pixel',
      ]);
      if (video.videoCodec == 'hevc') {
        args.addAll(['-x265-params', 'pools=2:frame-threads=1']);
      }
    } else {
      args.addAll(['-c:v', 'copy']);
    }
    if (audio != null) {
      if (audioChanges[index]) {
        args.addAll([
          '-c:a',
          audio!.audio['codec_name'] as String,
          '-ar',
          '${audio!.audio['sample_rate']}',
          '-ac',
          '${audio!.audio['channels']}',
        ]);
        if (canonicalAac) args.addAll(['-profile:a', 'aac_low']);
        if ({
          'aac',
          'ac3',
          'eac3',
          'mp2',
        }.contains(audio!.audio['codec_name'])) {
          args.addAll(['-b:a', '192k']);
        }
      } else {
        args.addAll(['-c:a', 'copy']);
      }
    }
    if (addSilence) args.addAll(['-t', probe.duration.toStringAsFixed(6)]);
    args.addAll(['-map_metadata', '-1', '-avoid_negative_ts', 'make_zero']);
    if (transportStream) {
      args.addAll([
        '-bsf:v',
        video.videoCodec == 'h264' ? 'h264_mp4toannexb' : 'hevc_mp4toannexb',
        '-mpegts_flags',
        '+resend_headers',
        '-f',
        'mpegts',
      ]);
    }
    return [...args, output];
  }
}

abstract class MediaExecutor {
  Future<MediaProbe> probe(String file);
  Future<void> run(
    List<String> arguments, {
    double duration = 0,
    void Function(double)? progress,
  });
  Future<void> cancel();
}

class FFmpegExecutor implements MediaExecutor {
  static const _sdkGateMessage = '当前系统版本过低，本地媒体处理需要 Android 6.0 及以上。';
  static const _channel = MethodChannel('duanju/device');
  static int _jobSequence = 0;
  String? _jobId;
  bool _cancelled = false;
  Timer? _progressTimer;

  @override
  Future<MediaProbe> probe(String file) async {
    if (!ffmpegSupported()) throw AppFailure(_sdkGateMessage);
    ffmpegSessionStarted = true;
    final data = await _channel.invokeMapMethod<String, dynamic>(
      'ffmpegProbe',
      {'file': file},
    );
    if (data == null) {
      throw AppFailure('无法读取本地视频信息，请检查文件完整性。');
    }
    return MediaProbe(Map<String, dynamic>.from(data));
  }

  @override
  Future<void> run(
    List<String> arguments, {
    double duration = 0,
    void Function(double)? progress,
  }) async {
    if (!ffmpegSupported()) throw AppFailure(_sdkGateMessage);
    ffmpegSessionStarted = true;
    _cancelled = false;
    final jobId = 'ff-${DateTime.now().microsecondsSinceEpoch}-${_jobSequence++}';
    _jobId = jobId;
    if (duration > 0 && progress != null) {
      _progressTimer?.cancel();
      _progressTimer = Timer.periodic(const Duration(milliseconds: 400), (
        _,
      ) async {
        final ms = await _channel.invokeMethod<int>(
          'ffmpegProgress',
          {'id': jobId},
        );
        if (ms != null && ms > 0) {
          progress((ms / 1000 / duration).clamp(0, 1));
        }
      });
    }
    try {
      final result = await _channel.invokeMapMethod<String, dynamic>('ffmpegRun', {
        'id': jobId,
        'arguments': [
          '-hide_banner',
          '-loglevel',
          'error',
          '-nostdin',
          '-y',
          '-protocol_whitelist',
          'file,crypto,data',
          ...arguments,
        ],
      });
      final code = (result?['code'] as num?)?.toInt() ?? -1;
      if (code != 0) {
        throw AppFailure(
          _cancelled || (code == 255 && result?['cancelled'] == true)
              ? '已取消本地媒体处理'
              : '本地媒体处理失败（$code），原分集已保留。',
        );
      }
    } on PlatformException catch (error) {
      throw AppFailure(error.message ?? '本地媒体处理失败，原分集已保留。');
    } finally {
      await _stopProgress();
      _jobId = null;
    }
  }

  Future<void> _stopProgress() async {
    _progressTimer?.cancel();
    _progressTimer = null;
  }

  @override
  Future<void> cancel() async {
    _cancelled = true;
    final jobId = _jobId;
    await _stopProgress();
    if (jobId != null) {
      try {
        await _channel.invokeMethod<void>('ffmpegCancel', {'id': jobId});
      } on PlatformException {
        // 任务已结束时取消无效
      }
    }
  }
}

void verifyMediaDuration(MediaProbe probe, double expected) {
  if (probe.videoCodec.isEmpty ||
      !probe.duration.isFinite ||
      probe.duration <= 0 ||
      expected > 0 &&
          (probe.duration - expected).abs() > max(2, expected * .02)) {
    throw AppFailure('生成的视频时长或视频轨不完整，原分集已保留。');
  }
}

String concatFileLine(String file) {
  if (file.contains('\n') || file.contains('\r')) {
    throw AppFailure('文件路径不能包含换行');
  }
  return "file '${file.replaceAll("'", "'\\''")}'";
}
