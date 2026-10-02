import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import 'app_layout.dart';

class AppOrientationController {
  AppOrientationController({required bool television})
    : _television = television;

  bool _television;
  bool _disposed = false;
  bool? _nativeTelevision;
  Object? _playbackOwner;
  List<DeviceOrientation> _playbackOrientations = DeviceOrientation.values;
  List<DeviceOrientation>? _appliedOrientations;
  Future<void> _pending = Future<void>.value();

  static List<DeviceOrientation> orientations({
    required bool television,
    bool fullscreen = false,
    double aspectRatio = 1,
  }) {
    if (television || fullscreen && aspectRatio >= 1) {
      return const [
        DeviceOrientation.landscapeLeft,
        DeviceOrientation.landscapeRight,
      ];
    }
    if (fullscreen) {
      return const [
        DeviceOrientation.portraitUp,
        DeviceOrientation.portraitDown,
      ];
    }
    return DeviceOrientation.values;
  }

  Future<void> setTelevision(bool television) {
    if (_television != television) {
      _television = television;
      _playbackOwner = null;
      _playbackOrientations = DeviceOrientation.values;
    }
    return _apply();
  }

  Future<void> setPlayback(
    Object owner, {
    required bool fullscreen,
    required double aspectRatio,
  }) async {
    final previousOwner = _playbackOwner;
    final previousOrientations = _playbackOrientations;
    if (!_television) {
      _playbackOwner = owner;
      _playbackOrientations = orientations(
        television: false,
        fullscreen: fullscreen,
        aspectRatio: aspectRatio,
      );
    }
    final requested = _playbackOrientations;
    try {
      await _apply();
    } catch (_) {
      if (!_disposed &&
          !_television &&
          identical(_playbackOwner, owner) &&
          identical(_playbackOrientations, requested)) {
        _playbackOwner = previousOwner;
        _playbackOrientations = previousOrientations;
      }
      rethrow;
    }
  }

  Future<void> releasePlayback(Object owner) {
    if (_television) return refresh();
    if (!identical(_playbackOwner, owner)) return Future<void>.value();
    _playbackOwner = null;
    _playbackOrientations = DeviceOrientation.values;
    return _apply();
  }

  Future<void> refresh() => _apply(force: true);

  Future<void> _apply({bool force = false}) {
    final next = _pending.catchError((Object _) {}).then((_) async {
      if (_disposed ||
          kIsWeb ||
          (defaultTargetPlatform != TargetPlatform.android &&
              defaultTargetPlatform != TargetPlatform.iOS)) {
        return;
      }
      final television = _television;
      if (defaultTargetPlatform == TargetPlatform.android &&
          (force || _nativeTelevision != television)) {
        try {
          await AppDevice.channel.invokeMethod<void>('setTelevisionMode', {
            'enabled': television,
          });
          _nativeTelevision = television;
        } on MissingPluginException {
          _nativeTelevision = null;
        }
      }
      if (_disposed || television != _television) return;
      final preferred = television
          ? orientations(television: true)
          : _playbackOrientations;
      if (force || !listEquals(preferred, _appliedOrientations)) {
        await SystemChrome.setPreferredOrientations(preferred);
        if (!_disposed) _appliedOrientations = preferred;
      }
    });
    _pending = next;
    return next;
  }

  void dispose() {
    _disposed = true;
  }
}

class AppOrientationScope extends StatefulWidget {
  const AppOrientationScope({
    super.key,
    required this.television,
    required this.child,
  });

  final bool television;
  final Widget child;

  static AppOrientationController? maybeOf(BuildContext context) => context
      .dependOnInheritedWidgetOfExactType<_OrientationScope>()
      ?.controller;

  @override
  State<AppOrientationScope> createState() => _AppOrientationScopeState();
}

class _AppOrientationScopeState extends State<AppOrientationScope>
    with WidgetsBindingObserver {
  late final _controller = AppOrientationController(
    television: widget.television,
  );

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _apply(_controller.refresh());
  }

  @override
  void didUpdateWidget(covariant AppOrientationScope oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.television != widget.television) {
      _apply(_controller.setTelevision(widget.television));
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _apply(_controller.refresh());
  }

  void _apply(Future<void> operation) {
    unawaited(
      operation.catchError((Object error, StackTrace stack) {
        FlutterError.reportError(
          FlutterErrorDetails(
            exception: error,
            stack: stack,
            library: 'app orientation',
          ),
        );
      }),
    );
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) =>
      _OrientationScope(controller: _controller, child: widget.child);
}

class _OrientationScope extends InheritedWidget {
  const _OrientationScope({required this.controller, required super.child});

  final AppOrientationController controller;

  @override
  bool updateShouldNotify(_OrientationScope oldWidget) =>
      controller != oldWidget.controller;
}
