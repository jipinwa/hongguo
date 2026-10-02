import 'dart:async';

import 'package:flutter/foundation.dart';

import 'core_bridge.dart';
import 'danmaku_models.dart';
import 'models.dart';

class DanmakuController extends ChangeNotifier {
  DanmakuController(this.repository, {DateTime Function()? now})
    : _now = now ?? DateTime.now;

  final AppRepository repository;
  final DateTime Function() _now;
  final _pages = <int, DanmakuPage>{};
  final _failures = <int, DateTime>{};
  PlaybackPlan? _plan;
  int _generation = 0;
  int? _pendingStart;
  bool _closed = false;
  bool _enabled = true;
  bool _available = false;
  bool _foreground = true;
  bool _playing = false;
  bool _buffering = false;
  bool _seeking = false;
  int positionMs = 0;
  int durationMs = 0;
  double rate = 1;
  int motionRevision = 0;
  int dataRevision = 0;
  List<DanmakuItem> items = const [];
  DateTime? _anchorTime;
  int _anchorPosition = 0;
  double _anchorRate = 1;
  bool _anchorMoving = false;

  bool get supported =>
      _plan != null &&
      !_plan!.local &&
      _plan!.session.isNotEmpty &&
      _plan!.danmakuId.isNotEmpty;
  bool get visible =>
      !_closed &&
      supported &&
      _enabled &&
      _available &&
      _foreground &&
      !_seeking &&
      durationMs > 0 &&
      positionMs < durationMs;
  bool get moving => visible && _playing && !_buffering;
  bool get loading => _pendingStart != null;
  bool get canRetry => visible && _failures.isNotEmpty;
  String get status {
    if (!_enabled) return '弹幕已关闭';
    if (_plan?.local == true) return '本地播放不加载弹幕';
    if (_plan != null && !supported) return '本集暂不支持弹幕';
    if (canRetry) return '弹幕暂不可用，可重试';
    if (loading && _pages.isEmpty) return '正在加载弹幕';
    if (_pages.isNotEmpty) {
      return items.isEmpty ? '这段暂无弹幕' : '已载入 ${items.length} 条弹幕';
    }
    return '等待播放';
  }

  void setPlan(PlaybackPlan? plan) {
    if (_closed || identical(_plan, plan)) return;
    _cancel();
    _plan = plan;
    _pages.clear();
    _failures.clear();
    _rebuildItems();
    _seeking = false;
    _available = false;
    positionMs = durationMs = 0;
    _resetMotion();
    notifyListeners();
  }

  void setEnabled(bool enabled) {
    if (_closed || _enabled == enabled) return;
    _enabled = enabled;
    _cancel();
    _failures.clear();
    _pages.clear();
    _rebuildItems();
    _resetMotion();
    notifyListeners();
    _ensureWindow();
  }

  void update({
    required Duration position,
    required Duration duration,
    required double speed,
    required bool playing,
    required bool buffering,
    required bool foreground,
    required bool available,
    bool discontinuity = false,
  }) {
    if (_closed) return;
    final wasVisible = visible, wasMoving = moving;
    final previousRate = rate;
    final nextPosition = position.inMilliseconds.clamp(0, danmakuMaxDurationMs);
    final nextDuration = duration.inMilliseconds;
    final elapsed = _anchorTime == null
        ? 0
        : _now()
              .difference(_anchorTime!)
              .inMilliseconds
              .clamp(0, danmakuMaxDurationMs);
    final expected =
        _anchorPosition + (_anchorMoving ? elapsed * _anchorRate : 0);
    final drift = !_seeking && (nextPosition - expected).abs() > 600;
    final durationChanged = nextDuration != durationMs && durationMs > 0;
    final jumped =
        !_seeking &&
        (discontinuity ||
            nextPosition < positionMs ||
            nextPosition - positionMs > 2000);
    if (durationChanged) {
      _cancel();
      _pages.clear();
      _failures.clear();
      _rebuildItems();
    } else if (jumped) {
      _cancel();
    }
    if (!_seeking) positionMs = nextPosition;
    durationMs = nextDuration > 0 && nextDuration <= danmakuMaxDurationMs
        ? nextDuration
        : 0;
    rate = speed.isFinite && speed > 0 ? speed : 1;
    _playing = playing;
    _buffering = buffering;
    _foreground = foreground;
    _available = available;
    if (wasVisible && !visible) _cancel();
    if (drift ||
        jumped ||
        durationChanged ||
        wasVisible != visible ||
        wasMoving != moving ||
        previousRate != rate) {
      _resetMotion();
    }
    notifyListeners();
    _ensureWindow();
  }

  void beginSeek() {
    if (_closed) return;
    _cancel();
    _seeking = true;
    _resetMotion();
    notifyListeners();
  }

  void endSeek(Duration position) {
    if (_closed) return;
    _seeking = false;
    positionMs = position.inMilliseconds.clamp(0, danmakuMaxDurationMs);
    _resetMotion();
    notifyListeners();
    _ensureWindow();
  }

  void retry() {
    if (_closed || !visible) return;
    _failures.clear();
    _ensureWindow();
    notifyListeners();
  }

  void _resetMotion() {
    motionRevision++;
    _anchorTime = _now();
    _anchorPosition = positionMs;
    _anchorRate = rate;
    _anchorMoving = moving;
  }

  void _cancel() {
    _generation++;
    if (_pendingStart != null) {
      _pendingStart = null;
      unawaited(repository.cancelDanmaku().catchError((Object _) {}));
    }
  }

  DanmakuPage? _covering(int position) {
    DanmakuPage? found;
    for (final page in _pages.values) {
      if (page.startMs <= position &&
          position < page.nextMs &&
          (found == null || page.startMs > found.startMs)) {
        found = page;
      }
    }
    return found;
  }

  void _ensureWindow() {
    if (!visible || _buffering || _pendingStart != null) return;
    final page = _covering(positionMs);
    var start = positionMs ~/ danmakuWindowMs * danmakuWindowMs;
    if (page == null && _pages.containsKey(start)) start = positionMs;
    if (page != null) {
      if (!moving ||
          positionMs < page.nextMs - danmakuLifetimeMs ||
          page.nextMs >= durationMs ||
          _covering(page.nextMs) != null) {
        return;
      }
      start = page.nextMs;
    }
    final retryAt = _failures[start];
    if (retryAt != null && _now().isBefore(retryAt)) return;
    unawaited(_load(start));
  }

  Future<void> _load(int start) async {
    final ticket = _generation;
    final plan = _plan!;
    final duration = durationMs;
    _pendingStart = start;
    notifyListeners();
    try {
      final page = await repository.danmaku(
        plan,
        startMs: start,
        durationMs: duration,
      );
      if (_closed || ticket != _generation) return;
      if (page.episodeId != plan.danmakuId ||
          page.startMs != start ||
          page.nextMs <= start ||
          page.nextMs > duration ||
          page.items.length > 90 ||
          page.items.any(
            (item) => item.timeMs < start || item.timeMs >= page.nextMs,
          )) {
        throw const FormatException('弹幕时间范围无效');
      }
      _pages[start] = page;
      _failures.remove(start);
      final nearest = _pages.keys.toList()
        ..sort(
          (a, b) => (a - positionMs).abs().compareTo((b - positionMs).abs()),
        );
      for (final key in nearest.skip(6)) {
        _pages.remove(key);
      }
      _rebuildItems();
    } catch (_) {
      if (_closed || ticket != _generation) return;
      _failures[start] = _now().add(const Duration(seconds: 15));
      while (_failures.length > 8) {
        _failures.remove(_failures.keys.first);
      }
    } finally {
      if (!_closed && ticket == _generation) {
        _pendingStart = null;
        notifyListeners();
        _ensureWindow();
      }
    }
  }

  void _rebuildItems() {
    final unique = <String, DanmakuItem>{};
    final pages = _pages.values.toList()
      ..sort((a, b) => a.startMs.compareTo(b.startMs));
    for (final page in pages) {
      for (final item in page.items) {
        unique.putIfAbsent(item.id, () => item);
      }
    }
    final sorted = unique.values.toList()
      ..sort((a, b) => a.timeMs.compareTo(b.timeMs));
    items = List.unmodifiable(sorted);
    dataRevision++;
  }

  @override
  void dispose() {
    _closed = true;
    _cancel();
    super.dispose();
  }
}
