import 'dart:async';
import 'dart:io';
import 'dart:ui';

import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

Size? videoDisplaySize(VideoParams parameters) {
  final width = (parameters.dw ?? 0) > 0 ? parameters.dw! : parameters.w ?? 0;
  final height = (parameters.dh ?? 0) > 0 ? parameters.dh! : parameters.h ?? 0;
  if (width <= 0 || height <= 0) return null;
  final rotation = (parameters.rotate ?? 0) % 180;
  return rotation == 90
      ? Size(height.toDouble(), width.toDouble())
      : Size(width.toDouble(), height.toDouble());
}

class VideoOutputSizeAdapter {
  VideoOutputSizeAdapter(this.video, {required this.onFailure});

  static const _channel = MethodChannel('com.alexmercerind/media_kit_video');
  final VideoController video;
  final void Function() onFailure;
  Future<void>? _initialization;
  StreamSubscription<VideoParams>? _subscription;
  Size? _target;
  bool _closed = false;

  Size? get actualSize => video.rect.value?.size;

  Future<void> initialize() => _initialization ??= _initialize();

  Future<void> _initialize() async {
    await video.platform.future;
    if (_closed) return;
    if (Platform.isAndroid) {
      _subscription = video.player.stream.videoParams.listen((_) {
        if (_closed || _target == null) return;
        unawaited(
          _resizeAndroid().catchError((Object _) {
            if (!_closed) onFailure();
          }),
        );
      });
    }
  }

  Future<void> setTarget(Size? value) async {
    await initialize();
    if (_closed) return;
    final normalized = value == null
        ? null
        : Size(value.width.roundToDouble(), value.height.roundToDouble());
    final changed = _target != normalized;
    _target = normalized;
    if (Platform.isAndroid) {
      await _resizeAndroid();
    } else if (changed || normalized == null) {
      await video.setSize(
        width: normalized?.width.toInt(),
        height: normalized?.height.toInt(),
      );
    }
  }

  Future<void> _resizeAndroid() async {
    if (_closed) return;
    final source = videoDisplaySize(video.player.state.videoParams);
    if (source == null) return;
    final desired = _target ?? source;
    final width = desired.width.round();
    final height = desired.height.round();
    if (width < 1 || height < 1 || width > 16384 || height > 16384) return;
    if (video.rect.value?.size == desired) return;
    final handle = await video.player.handle;
    if (_closed) return;
    await _channel.invokeMethod<void>('VideoOutputManager.SetSurfaceSize', {
      'handle': handle.toString(),
      'width': width.toString(),
      'height': height.toString(),
    });
    if (_closed) return;
    video.rect.value = Rect.fromLTWH(0, 0, width.toDouble(), height.toDouble());
  }

  Future<void> close() async {
    _closed = true;
    await _initialization;
    await _subscription?.cancel();
  }
}
