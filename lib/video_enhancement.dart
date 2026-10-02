import 'dart:async';
import 'dart:io';
import 'dart:math' as math;
import 'dart:ui';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';

import 'video_enhancement_assets.dart';
import 'video_enhancement_mpv.dart';
import 'video_enhancement_policy.dart';
import 'video_enhancement_preferences.dart';
import 'video_output_size.dart';

class VideoEnhancementController extends ChangeNotifier {
  VideoEnhancementController({
    required this.player,
    required VideoController? video,
    required VideoEnhancementPreferences preferences,
    required String category,
    required List<String> tags,
  }) : _preferences = preferences,
       _animationHint = videoHasAnimationTags(category, tags) {
    final native = player.platform;
    supported = video != null && native is NativePlayer && Platform.isWindows;
    if (!supported) {
      _status = '当前平台暂不支持画质增强';
      return;
    }
    _mpv = VideoEnhancementMpv(native as NativePlayer);
    _output = VideoOutputSizeAdapter(video!, onFailure: _outputFailed);
    _subscriptions.add(player.stream.log.listen(_onLog));
    _subscriptions.add(player.stream.videoParams.listen((_) => _schedule()));
    _subscriptions.add(
      player.stream.rate.listen((_) {
        _ignorePerformanceUntil = DateTime.now().add(
          const Duration(seconds: 5),
        );
        _schedule();
      }),
    );
    _subscriptions.add(
      player.stream.buffering.listen((buffering) {
        if (buffering) ignorePerformance();
      }),
    );
    _initialization = _initialize();
    _timer = Timer.periodic(const Duration(seconds: 2), (_) => _startMonitor());
  }

  static const _device = MethodChannel('duanju/device');
  static const _filterLabel = 'duanju_vsr';
  final Player player;
  late final bool supported;
  final bool _animationHint;
  VideoEnhancementPreferences _preferences;
  VideoEnhancementMpv? _mpv;
  VideoOutputSizeAdapter? _output;
  Future<void>? _initialization;
  Future<void>? _monitoring;
  Future<void>? _closing;
  Future<void> _tail = Future<void>.value();
  final _subscriptions = <StreamSubscription<dynamic>>[];
  final _ownedShaders = <String>{};
  final _originalOptions = <String, String>{};
  final _appliedOptions = <String, String>{};
  final _rejected = <VideoEnhancementBackend>{};
  final _renderSamples = <double>[];
  final _recentEnhancementErrors = <String>{};
  Timer? _timer;
  Timer? _debounce;
  int _revision = 0;
  int _costLimit = 4;
  int _confirmationMisses = 0;
  int _failureCount = 0;
  int? _previousDrops;
  DateTime? _previousSample;
  DateTime? _overloadedSince;
  DateTime _ignorePerformanceUntil = DateTime.fromMillisecondsSinceEpoch(0);
  VideoEnhancementPower _power = const VideoEnhancementPower();
  VideoEnhancementDecision? _applied;
  VideoEnhancementBackend? _applyingBackend;
  bool _ready = false;
  bool _initialized = false;
  bool _foreground = true;
  bool _television = false;
  bool _closed = false;
  bool _comparing = false;
  bool _confirmed = false;
  bool _gpu = false;
  bool _luma = false;
  bool _opaque8Bit = false;
  bool _hardware = false;
  bool _softwareRenderer = false;
  bool _circuitOpen = false;
  String _renderer = '';
  String _vendor = '';
  String _status = '画质增强已关闭';
  String _downgradeReason = '';
  String _lastFailure = '';
  Size _viewport = Size.zero;

  VideoEnhancementPreferences get preferences => _preferences;
  bool get comparing => _comparing;
  bool get canCompare =>
      supported &&
      _ready &&
      _foreground &&
      !_circuitOpen &&
      _preferences.mode != VideoEnhancementMode.off &&
      (_comparing || (_applied?.backend.cost ?? 0) > 0);
  bool get animation => switch (_preferences.content) {
    VideoEnhancementContent.automatic => _animationHint,
    VideoEnhancementContent.animation => true,
    VideoEnhancementContent.general => false,
  };
  String get status => _status;
  String get contentLabel => animation ? '当前按动漫增强' : '当前按通用画面增强';
  String get detail {
    final source = videoDisplaySize(player.state.videoParams);
    final output = _applied?.output;
    final dimensions = source != null && output != null
        ? '${source.width.round()}×${source.height.round()} → ${output.width.round()}×${output.height.round()}'
        : '';
    return [
      dimensions,
      if (_downgradeReason.isNotEmpty) _downgradeReason,
    ].where((item) => item.isNotEmpty).join(' · ');
  }

  Future<void> _initialize() async {
    try {
      await _mpv!.initialize();
      if (_closed) return;
      await _output!.initialize();
      if (_closed) return;
      _initialized = true;
      await _readPower();
      _readCapabilities();
      _schedule();
    } catch (_) {
      if (!_closed) _setStatus('增强初始化失败，继续原画播放');
    }
  }

  void setPreferences(VideoEnhancementPreferences value) {
    if (_closed || value == _preferences) return;
    _preferences = value;
    _comparing = false;
    _costLimit = 4;
    _rejected.clear();
    _downgradeReason = '';
    _lastFailure = '';
    _failureCount = 0;
    _circuitOpen = false;
    _schedule(immediate: true);
    notifyListeners();
  }

  void setViewport(Size pixels, {required bool television}) {
    if (_closed || !supported) return;
    if (_viewport == pixels && _television == television) return;
    _viewport = pixels;
    _television = television;
    ignorePerformance();
    _schedule();
  }

  void setForeground(bool value) {
    if (_closed || _foreground == value) return;
    _foreground = value;
    ignorePerformance();
    _schedule(immediate: true);
  }

  void suspend() {
    if (_closed) return;
    _ready = false;
    _comparing = false;
    _revision++;
    _schedule(immediate: true);
  }

  Future<void> beforeMedia() async {
    if (_closed || !supported) return;
    _ready = false;
    _comparing = false;
    _revision++;
    _debounce?.cancel();
    _costLimit = 4;
    _downgradeReason = '';
    _lastFailure = '';
    _previousDrops = null;
    _previousSample = null;
    _renderSamples.clear();
    _recentEnhancementErrors.clear();
    _failureCount = 0;
    _circuitOpen = false;
    ignorePerformance();
    await _enqueue(() async {
      await _initialization;
      if (_closed || !_initialized) return;
      _clearEffects();
      await _output!.setTarget(null);
      _applied = null;
      _confirmed = false;
    });
  }

  void mediaReady() {
    if (_closed) return;
    _ready = true;
    ignorePerformance();
    _schedule();
  }

  void ignorePerformance() {
    _ignorePerformanceUntil = DateTime.now().add(const Duration(seconds: 6));
    _overloadedSince = null;
    _renderSamples.clear();
    _previousDrops = null;
    _previousSample = null;
  }

  Future<void> toggleCompare() async {
    if (!canCompare || _closed) return;
    _comparing = !_comparing;
    ignorePerformance();
    _schedule(immediate: true);
    notifyListeners();
    await _tail;
  }

  void _schedule({bool immediate = false}) {
    if (_closed || !supported || _circuitOpen) return;
    final revision = ++_revision;
    _debounce?.cancel();
    if (immediate) {
      unawaited(_enqueue(() => _apply(revision)));
    } else {
      _debounce = Timer(const Duration(milliseconds: 220), () {
        unawaited(_enqueue(() => _apply(revision)));
      });
    }
  }

  Future<void> _enqueue(Future<void> Function() work) {
    final next = _tail.then((_) async {
      if (_closed) return;
      try {
        await work();
      } catch (error) {
        if (!_closed) _failBackend(error);
      }
    });
    _tail = next.catchError((Object _) {});
    return _tail;
  }

  Future<void> _apply(int revision) async {
    await _initialization;
    if (_closed || !_initialized || revision != _revision) return;
    _readCapabilities();
    final decision = chooseVideoEnhancement(
      preferences: _preferences,
      parameters: player.state.videoParams,
      viewport: _viewport,
      ready: _ready && _foreground,
      android: Platform.isAndroid,
      television: _television,
      gpu: _gpu,
      luma: _luma,
      hardware: _hardware,
      animation: animation,
      power: _power,
      rate: player.state.rate,
      costLimit: _costLimit,
      rejected: _rejected,
      opaque8Bit: _opaque8Bit,
    );
    final selected = _comparing && canCompare
        ? VideoEnhancementDecision(
            VideoEnhancementBackend.original,
            decision.output,
            '原画对比中',
          )
        : decision;
    if (selected == _applied) {
      _publishStatus();
      return;
    }
    _applyingBackend = selected.backend;
    try {
      final shader = switch (selected.backend) {
        VideoEnhancementBackend.ravu =>
          _luma
              ? VideoEnhancementAssets.ravuLuma
              : VideoEnhancementAssets.ravuRgb,
        VideoEnhancementBackend.fsrcnnx => VideoEnhancementAssets.fsrcnnx,
        VideoEnhancementBackend.anime => VideoEnhancementAssets.anime,
        _ => null,
      };
      final shaderPath = shader == null
          ? null
          : await VideoEnhancementAssets.prepare(shader);
      if (_closed || revision != _revision) return;
      _applied = null;
      _clearEffects();
      await _output!.setTarget(selected.output);
      if (_closed || revision != _revision) return;
      if (selected.backend != VideoEnhancementBackend.original) {
        _setOption('scale', 'spline36');
        _setOption('dscale', 'mitchell');
        _setOption('cscale', 'bilinear');
        _setOption('correct-downscaling', 'yes');
        if (shaderPath != null) {
          _setOption('fbo-format', 'rgba16f');
          final current = _shaderList();
          _ownedShaders.add(shaderPath);
          _mpv!.setStrings('glsl-shaders', [...current, shaderPath]);
        } else if (selected.backend == VideoEnhancementBackend.hardware) {
          final parameters = player.state.videoParams;
          final factor = math
              .min(
                selected.output!.width / parameters.w!,
                selected.output!.height / parameters.h!,
              )
              .clamp(1.0, 2.0);
          _mpv!.command([
            'vf',
            'add',
            '@$_filterLabel:d3d11vpp=scale=${factor.toStringAsFixed(6)}:scaling-mode=$_vendor:format=nv12:deint=no',
          ]);
        }
      }
      _applied = selected;
      _confirmationMisses = 0;
      _confirmed = selected.backend == VideoEnhancementBackend.original;
      ignorePerformance();
      _publishStatus();
      if (!_closed) notifyListeners();
    } catch (error) {
      _failBackend(error, backend: selected.backend);
    } finally {
      _applyingBackend = null;
    }
  }

  String _optionValue(Object? value) => switch (value) {
    bool flag => flag ? 'yes' : 'no',
    null => '',
    _ => value.toString(),
  };

  void _setOption(String key, String value) {
    final original = _mpv!.read(key);
    if (original == null) throw VideoEnhancementFailure(key);
    _originalOptions.putIfAbsent(key, () => _optionValue(original));
    _mpv!.set(key, value);
    _appliedOptions[key] = value;
  }

  List<String> _shaderList() {
    final value = _mpv!.read('glsl-shaders');
    if (value is! List) throw const VideoEnhancementFailure('shader-list');
    return value.whereType<String>().toList();
  }

  void _clearEffects() {
    if (_mpv == null || !_initialized) return;
    Object? failure;
    try {
      if (_ownedShaders.isNotEmpty) {
        final current = _shaderList();
        final remaining = current
            .where((item) => !_ownedShaders.contains(item))
            .toList();
        if (current.length != remaining.length)
          _mpv!.setStrings('glsl-shaders', remaining);
      }
    } catch (error) {
      failure = error;
    }
    try {
      final filters = _mpv!.read('vf');
      if (filters is List &&
          filters.any((item) => item is Map && item['label'] == _filterLabel)) {
        _mpv!.command(['vf', 'remove', '@$_filterLabel']);
      }
    } catch (error) {
      failure ??= error;
    }
    for (final entry in _appliedOptions.entries.toList().reversed) {
      try {
        if (_optionValue(_mpv!.read(entry.key)) == entry.value) {
          _mpv!.set(entry.key, _originalOptions[entry.key]!);
        }
        _appliedOptions.remove(entry.key);
      } catch (error) {
        failure ??= error;
      }
    }
    if (failure != null) throw failure;
  }

  void _readCapabilities() {
    if (_closed || !_initialized) return;
    final native = _mpv!;
    final vo = native.read('current-vo')?.toString() ?? '';
    final hwdec = native.read('hwdec-current')?.toString() ?? '';
    final version = native.read('mpv-version')?.toString() ?? '';
    final versionNumber = RegExp(r'\bv?0\.(\d+)\.').firstMatch(version);
    final recentMpv =
        versionNumber != null && (int.tryParse(versionNumber[1]!) ?? 0) >= 39;
    final passes = native.read('vo-passes');
    _gpu =
        !_softwareRenderer &&
        (vo == 'gpu' ||
            vo == 'libmpv' && (_renderer.isNotEmpty || passes is Map));
    final parameters = player.state.videoParams;
    final format = videoEnhancementPixelFormat(parameters);
    _luma =
        !(Platform.isAndroid && hwdec == 'mediacodec') &&
        (format.startsWith('yuv') || format == 'nv12' || format == 'nv21');
    _opaque8Bit = false;
    if (Platform.isAndroid && hwdec == 'mediacodec' && format == 'mediacodec') {
      final tracks = native.read('track-list');
      if (tracks is List) {
        for (final track in tracks.whereType<Map>()) {
          if (track['type'] != 'video' || track['selected'] != true) continue;
          final codec = track['codec']?.toString().toLowerCase() ?? '';
          final profile =
              track['codec-profile']?.toString().toLowerCase() ?? '';
          _opaque8Bit =
              codec == 'h264' &&
                  {
                    'baseline',
                    'constrained baseline',
                    'main',
                    'extended',
                    'high',
                    'constrained high',
                  }.contains(profile) ||
              codec == 'hevc' &&
                  {'main', 'main still picture'}.contains(profile);
          break;
        }
      }
    }
    _hardware =
        Platform.isWindows &&
        _gpu &&
        recentMpv &&
        (_vendor == 'nvidia' || _vendor == 'intel') &&
        hwdec == 'd3d11va' &&
        (format == 'nv12' || format == 'yuv420p');
  }

  void _onLog(PlayerLog log) {
    if (_closed) return;
    final prefix = log.prefix.toLowerCase();
    final text = log.text.toLowerCase();
    if ((prefix.contains('vo/') || prefix.contains('libmpv')) &&
        (text.contains('gl_renderer') ||
            text.contains('gl_vendor') ||
            text.contains('gl renderer') ||
            text.contains('gl vendor'))) {
      if (text.contains('renderer')) {
        _renderer = log.text.length <= 512
            ? log.text
            : log.text.substring(0, 512);
        _vendor = text.contains('nvidia')
            ? 'nvidia'
            : text.contains('intel')
            ? 'intel'
            : '';
      }
      _softwareRenderer =
          _softwareRenderer ||
          RegExp(
            r'swiftshader|llvmpipe|softpipe|software rasterizer|microsoft basic',
          ).hasMatch(text);
      _schedule();
    }
    final active = _applyingBackend ?? _applied?.backend;
    if (active == null ||
        active == VideoEnhancementBackend.original ||
        _circuitOpen)
      return;
    final graphics =
        prefix.contains('d3d11vpp') ||
        prefix.contains(_filterLabel) ||
        prefix.contains('vo/gpu') ||
        prefix.contains('vo/libmpv');
    final failed =
        log.level == 'error' ||
        log.level == 'fatal' ||
        log.level == 'warn' &&
            RegExp(r'fail|error|unsupported|disabled').hasMatch(text);
    if (graphics && failed) {
      if (_recentEnhancementErrors.length >= 16)
        _recentEnhancementErrors.clear();
      _recentEnhancementErrors.add(log.text.trim());
      _failBackend(const VideoEnhancementFailure('render'));
    }
  }

  bool handlePlaybackError(String text) {
    if (_closed || !supported) return false;
    final value = text.trim();
    final active = (_applyingBackend ?? _applied?.backend)?.cost ?? 0;
    final owned =
        _recentEnhancementErrors.contains(value) ||
        active > 0 &&
            RegExp(
              r'd3d11vpp|duanju_vsr|glsl.shaders|shader.*compil|compil.*shader',
              caseSensitive: false,
            ).hasMatch(value);
    if (!owned) return false;
    _failBackend(const VideoEnhancementFailure('render'));
    return true;
  }

  void _outputFailed() {
    if (_closed || _costLimit == 0) return;
    _costLimit = 0;
    _failBackend(const VideoEnhancementFailure('output-size'));
  }

  void _failBackend(Object error, {VideoEnhancementBackend? backend}) {
    if (_closed || _circuitOpen) return;
    final failed = backend ?? _applyingBackend ?? _applied?.backend;
    _failureCount++;
    _applyingBackend = null;
    if (failed != null) _rejected.add(failed);
    if (failed == VideoEnhancementBackend.scaling ||
        failed == VideoEnhancementBackend.original ||
        error is VideoEnhancementFailure && error.stage == 'output-size') {
      _costLimit = 0;
    }
    _lastFailure = error is VideoEnhancementFailure ? error.stage : 'resource';
    _downgradeReason = '增强不可用，已自动回退';
    _applied = null;
    _confirmed = false;
    _setStatus('正在恢复原画播放');
    if (_failureCount >= 4 || failed == VideoEnhancementBackend.original) {
      _circuitOpen = true;
      _costLimit = 0;
      _revision++;
      _debounce?.cancel();
      unawaited(
        _enqueue(() async {
          try {
            _clearEffects();
            await _output?.setTarget(null);
            if (_closed) return;
            _applied = const VideoEnhancementDecision(
              VideoEnhancementBackend.original,
              null,
              '增强不可用，已恢复原画',
            );
            _setStatus('增强不可用，已恢复原画');
          } catch (_) {
            _setStatus('增强已停用，请重新打开本集');
          }
        }),
      );
      return;
    }
    _schedule(immediate: true);
  }

  void _startMonitor() {
    if (_closed ||
        !_initialized ||
        _monitoring != null ||
        _circuitOpen ||
        !_foreground ||
        !_ready ||
        _preferences.mode == VideoEnhancementMode.off)
      return;
    final future = _monitor();
    _monitoring = future;
    unawaited(
      future.whenComplete(() {
        if (identical(_monitoring, future)) _monitoring = null;
      }),
    );
  }

  Future<void> _readPower() async {
    try {
      final value = await _device
          .invokeMapMethod<String, dynamic>('playbackPower')
          .timeout(const Duration(seconds: 2));
      if (!_closed && value != null)
        _power = VideoEnhancementPower.fromMap(value);
    } catch (_) {}
  }

  Future<void> _monitor() async {
    try {
      await _readPower();
      if (_closed || !_initialized || _circuitOpen || _applyingBackend != null)
        return;
      final oldCapabilities = (_gpu, _luma, _hardware, _opaque8Bit);
      _readCapabilities();
      if (oldCapabilities != (_gpu, _luma, _hardware, _opaque8Bit)) _schedule();
      if (!_ready || !_foreground) return;
      final selected = _applied;
      if (selected == null) {
        _schedule();
        return;
      }
      final reassessed = chooseVideoEnhancement(
        preferences: _preferences,
        parameters: player.state.videoParams,
        viewport: _viewport,
        ready: true,
        android: Platform.isAndroid,
        television: _television,
        gpu: _gpu,
        luma: _luma,
        hardware: _hardware,
        animation: animation,
        power: _power,
        rate: player.state.rate,
        costLimit: _costLimit,
        rejected: _rejected,
        opaque8Bit: _opaque8Bit,
      );
      if (!_comparing && reassessed != selected) {
        if (reassessed.backend.cost < selected.backend.cost &&
            (_power.thermalStatus >= 2 || (_power.headroom ?? 0) >= .85)) {
          _costLimit = math.min(_costLimit, reassessed.backend.cost);
          _downgradeReason = '本集已因温控降低增强';
        }
        _schedule();
        return;
      }
      if (selected.backend == VideoEnhancementBackend.original) return;
      final passes = _mpv!.read('vo-passes');
      final fresh = passes is Map && passes['fresh'] is List
          ? passes['fresh'] as List
          : const [];
      final descriptions = fresh
          .whereType<Map>()
          .map((item) => item['desc']?.toString() ?? '')
          .join('\n');
      final actual = _output!.actualSize;
      final surface = _mpv!.read('osd-dimensions');
      final surfaceWidth = surface is Map ? surface['w'] : null;
      final surfaceHeight = surface is Map ? surface['h'] : null;
      final sizeMatches =
          actual != null &&
          selected.output != null &&
          (actual.width - selected.output!.width).abs() <= 2 &&
          (actual.height - selected.output!.height).abs() <= 2 &&
          surfaceWidth is num &&
          surfaceHeight is num &&
          (surfaceWidth - selected.output!.width).abs() <= 2 &&
          (surfaceHeight - selected.output!.height).abs() <= 2;
      final effectFound = switch (selected.backend) {
        VideoEnhancementBackend.ravu => descriptions.contains(
          _luma ? 'RAVU-Lite-AR (step2' : 'RAVU (step4',
        ),
        VideoEnhancementBackend.anime =>
          descriptions.contains('Anime4K') &&
              descriptions.contains('Depth-to-Space'),
        VideoEnhancementBackend.fsrcnnx =>
          descriptions.contains('feature map') &&
              descriptions.contains('aggregation'),
        VideoEnhancementBackend.hardware => _hardwareFramesChanged(selected),
        _ => _gpu,
      };
      final wasConfirmed = _confirmed;
      if (sizeMatches && effectFound) {
        _confirmed = true;
        _confirmationMisses = 0;
      } else if (player.state.playing && !player.state.buffering) {
        _confirmed = false;
        _confirmationMisses++;
        if (_confirmationMisses >= 5) {
          _failBackend(
            VideoEnhancementFailure(
              sizeMatches ? 'render-pass' : 'output-size',
            ),
          );
          return;
        }
      }
      _publishStatus();
      if (wasConfirmed != _confirmed && !_closed) notifyListeners();
      _watchPerformance(selected, fresh);
    } catch (_) {
      if (!_closed) {
        _previousDrops = null;
        _previousSample = null;
      }
    }
  }

  bool _hardwareFramesChanged(VideoEnhancementDecision selected) {
    final output = _mpv!.read('video-out-params');
    final filters = _mpv!.read('vf');
    if (output is! Map || filters is! List || selected.output == null)
      return false;
    final width = (output['w'] as num?)?.toDouble() ?? 0;
    final height = (output['h'] as num?)?.toDouble() ?? 0;
    return filters.any(
          (item) =>
              item is Map &&
              item['label'] == _filterLabel &&
              item['enabled'] != false,
        ) &&
        (width - selected.output!.width).abs() <= 4 &&
        (height - selected.output!.height).abs() <= 4;
  }

  bool _isOwnPass(
    String description,
    VideoEnhancementBackend backend,
  ) => switch (backend) {
    VideoEnhancementBackend.ravu => description.contains('RAVU'),
    VideoEnhancementBackend.anime => description.contains('Anime4K'),
    VideoEnhancementBackend.fsrcnnx => RegExp(
      r'feature map [12]|mapping [1-4]_[12]|sub-band residuals [12]|sub-pixel convolution 1|aggregation',
    ).hasMatch(description),
    _ => false,
  };

  void _watchPerformance(VideoEnhancementDecision selected, List fresh) {
    final now = DateTime.now();
    final state = player.state;
    if (!_confirmed ||
        !state.playing ||
        state.buffering ||
        _comparing ||
        state.rate > 1.01 ||
        now.isBefore(_ignorePerformanceUntil)) {
      _previousSample = null;
      _previousDrops = null;
      _overloadedSince = null;
      return;
    }
    var fpsValue = _mpv!.read('container-fps');
    if (fpsValue is! num || fpsValue <= 0) {
      fpsValue = _mpv!.read('estimated-vf-fps');
    }
    final fps = fpsValue is num ? fpsValue.toDouble() : 0.0;
    if (!fps.isFinite || fps <= 0 || fps > 240) return;
    var ownMs = 0.0;
    var timed = false;
    for (final pass in fresh.whereType<Map>()) {
      if (!_isOwnPass(pass['desc']?.toString() ?? '', selected.backend))
        continue;
      final last = pass['last'];
      if (last is num && last >= 0) {
        ownMs += last / 1000000;
        timed = true;
      }
    }
    if (timed && ownMs.isFinite) {
      _renderSamples.add(ownMs);
      if (_renderSamples.length > 20) _renderSamples.removeAt(0);
    }
    final samples = [..._renderSamples]..sort();
    final p95 = samples.isEmpty
        ? 0.0
        : samples[((samples.length - 1) * .95).ceil()];
    var overloaded = samples.length >= 5 && p95 > 1000 / fps / state.rate * .25;
    final rawDrops = _mpv!.read('frame-drop-count');
    if (rawDrops is num) {
      final drops = rawDrops.toInt();
      final previous = _previousDrops;
      final previousTime = _previousSample;
      if (previous != null && previousTime != null && drops >= previous) {
        final seconds = now.difference(previousTime).inMilliseconds / 1000;
        if (seconds >= 1 && seconds <= 5) {
          overloaded = overloaded || (drops - previous) / (fps * seconds) > .01;
        }
      }
      _previousDrops = drops;
      _previousSample = now;
    }
    if (!overloaded) {
      _overloadedSince = null;
      return;
    }
    _overloadedSince ??= now;
    if (now.difference(_overloadedSince!) < const Duration(seconds: 10)) return;
    _costLimit = math.min(
      _costLimit,
      selected.backend == VideoEnhancementBackend.hardware
          ? 2
          : selected.backend.cost - 1,
    );
    _downgradeReason = '本集已为保持流畅降低增强';
    ignorePerformance();
    _schedule(immediate: true);
  }

  void _publishStatus() {
    final selected = _applied;
    if (selected == null) return;
    if (_comparing) {
      _setStatus('原画对比中');
    } else if (selected.backend == VideoEnhancementBackend.original) {
      _setStatus(
        _lastFailure.isNotEmpty && _preferences.mode != VideoEnhancementMode.off
            ? '增强不可用，已恢复原画'
            : selected.reason,
      );
    } else if (!_confirmed) {
      _setStatus('正在确认${selected.backend.label}');
    } else if (selected.backend == VideoEnhancementBackend.hardware) {
      _setStatus('硬件增强已接入，驱动效果以对比为准');
    } else {
      _setStatus(
        [
          selected.backend.label,
          selected.reason,
        ].where((item) => item.isNotEmpty).join(' · '),
      );
    }
  }

  void _setStatus(String value) {
    if (_closed || value == _status) return;
    _status = value;
    notifyListeners();
  }

  Future<void> close() => _closing ??= _close();

  Future<void> _close() async {
    if (_closed) return;
    _closed = true;
    _revision++;
    _timer?.cancel();
    _debounce?.cancel();
    for (final subscription in _subscriptions) {
      await subscription.cancel();
    }
    await _initialization;
    await _tail;
    await _monitoring;
    await _output?.close();
    if (_mpv != null) _mpv!.closed = true;
    super.dispose();
  }
}
