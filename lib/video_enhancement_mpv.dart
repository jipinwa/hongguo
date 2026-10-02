import 'dart:ffi';

import 'package:ffi/ffi.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit/generated/libmpv/bindings.dart' as mpv;

class VideoEnhancementFailure implements Exception {
  const VideoEnhancementFailure(this.stage, [this.code = 0]);
  final String stage;
  final int code;
  @override
  String toString() => 'VideoEnhancementFailure($stage, $code)';
}

class VideoEnhancementMpv {
  VideoEnhancementMpv(this.player);
  final NativePlayer player;
  bool closed = false;

  Future<void> initialize() async {
    await player.waitForPlayerInitialization;
    await player.waitForVideoControllerInitializationIfAttached;
    _available();
  }

  void _available() {
    if (closed || player.disposed || player.ctx == nullptr) {
      throw const VideoEnhancementFailure('disposed');
    }
  }

  Object? read(String property) {
    _available();
    final name = property.toNativeUtf8();
    final node = calloc<mpv.mpv_node>();
    var obtained = false;
    try {
      final result = player.mpv.mpv_get_property(
        player.ctx,
        name.cast(),
        mpv.mpv_format.MPV_FORMAT_NODE,
        node.cast(),
      );
      if (result < 0) return null;
      obtained = true;
      return _decode(node.ref, 0);
    } finally {
      if (obtained) player.mpv.mpv_free_node_contents(node);
      calloc.free(node);
      calloc.free(name);
    }
  }

  Object? _decode(mpv.mpv_node node, int depth) {
    if (depth > 10) return null;
    switch (node.format) {
      case mpv.mpv_format.MPV_FORMAT_STRING:
        return node.u.string == nullptr
            ? null
            : node.u.string.cast<Utf8>().toDartString();
      case mpv.mpv_format.MPV_FORMAT_FLAG:
        return node.u.flag != 0;
      case mpv.mpv_format.MPV_FORMAT_INT64:
        return node.u.int64;
      case mpv.mpv_format.MPV_FORMAT_DOUBLE:
        return node.u.double_;
      case mpv.mpv_format.MPV_FORMAT_NODE_ARRAY:
      case mpv.mpv_format.MPV_FORMAT_NODE_MAP:
        if (node.u.list == nullptr) return null;
        final list = node.u.list.ref;
        if (list.num < 0 ||
            list.num > 16384 ||
            list.num > 0 && list.values == nullptr) {
          return null;
        }
        if (node.format == mpv.mpv_format.MPV_FORMAT_NODE_ARRAY) {
          return [
            for (var i = 0; i < list.num; i++)
              _decode(list.values[i], depth + 1),
          ];
        }
        if (list.num > 0 && list.keys == nullptr) return null;
        return <String, Object?>{
          for (var i = 0; i < list.num; i++)
            if (list.keys[i] != nullptr)
              list.keys[i].cast<Utf8>().toDartString(): _decode(
                list.values[i],
                depth + 1,
              ),
        };
      default:
        return null;
    }
  }

  void set(String property, String value) {
    _available();
    final name = property.toNativeUtf8();
    final data = value.toNativeUtf8();
    try {
      final code = player.mpv.mpv_set_property_string(
        player.ctx,
        name.cast(),
        data.cast(),
      );
      if (code < 0) throw VideoEnhancementFailure(property, code);
    } finally {
      calloc.free(data);
      calloc.free(name);
    }
  }

  void setStrings(String property, List<String> values) {
    _available();
    final name = property.toNativeUtf8();
    final node = calloc<mpv.mpv_node>();
    final list = calloc<mpv.mpv_node_list>();
    final items = calloc<mpv.mpv_node>(values.isEmpty ? 1 : values.length);
    final strings = <Pointer<Utf8>>[];
    try {
      for (var i = 0; i < values.length; i++) {
        final text = values[i].toNativeUtf8();
        strings.add(text);
        items[i].format = mpv.mpv_format.MPV_FORMAT_STRING;
        items[i].u.string = text.cast();
      }
      list.ref.num = values.length;
      list.ref.values = items;
      node.ref.format = mpv.mpv_format.MPV_FORMAT_NODE_ARRAY;
      node.ref.u.list = list;
      final code = player.mpv.mpv_set_property(
        player.ctx,
        name.cast(),
        mpv.mpv_format.MPV_FORMAT_NODE,
        node.cast(),
      );
      if (code < 0) throw VideoEnhancementFailure(property, code);
    } finally {
      for (final value in strings) {
        calloc.free(value);
      }
      calloc.free(items);
      calloc.free(list);
      calloc.free(node);
      calloc.free(name);
    }
  }

  void command(List<String> values) {
    _available();
    final arguments = calloc<Pointer<Int8>>(values.length + 1);
    final strings = <Pointer<Utf8>>[];
    try {
      for (var i = 0; i < values.length; i++) {
        final value = values[i].toNativeUtf8();
        strings.add(value);
        arguments[i] = value.cast();
      }
      final code = player.mpv.mpv_command(player.ctx, arguments);
      if (code < 0) throw VideoEnhancementFailure(values.first, code);
    } finally {
      for (final value in strings) {
        calloc.free(value);
      }
      calloc.free(arguments);
    }
  }
}
