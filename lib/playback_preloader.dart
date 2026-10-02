import 'dart:async';

import 'package:flutter/foundation.dart';

import 'core_bridge.dart';
import 'models.dart';

class PlaybackPreloader extends ChangeNotifier {
  PlaybackPreloader(this.repository, {DateTime Function()? now})
    : _now = now ?? DateTime.now;

  final AppRepository repository;
  final DateTime Function() _now;
  PlaybackPlan? _ready;
  String? _identity;
  Timer? _delay;
  DateTime _expires = DateTime(2000), _retryAt = DateTime(2000);
  int _generation = 0;
  bool _closed = false, loading = false;
  String error = '';
  void _release(PlaybackPlan plan) =>
      unawaited(repository.release(plan.session).catchError((Object _) {}));

  String get status {
    if (_ready != null && _now().isBefore(_expires)) {
      final bytes = _ready!.prefetchedBytes;
      return _ready!.local
          ? '下一集已下载'
          : bytes > 0
          ? '下一集已准备 · 预取 ${(bytes / 1024).ceil()} KB'
          : '下一集播放地址已准备';
    }
    if (loading || _delay != null) return '正在准备下一集';
    return error.isNotEmpty ? '下一集预加载暂不可用，切集时会重新获取' : '播放后段提前准备下一集';
  }

  String _key(Drama drama, Episode episode, int quality, bool online) =>
      '${drama.id}\u0000${episode.number}\u0000$quality\u0000$online';

  void prepare(
    Drama drama,
    Episode episode, {
    int quality = 0,
    bool online = false,
  }) {
    if (_closed) return;
    final identity = _key(drama, episode, quality, online);
    if (_identity != identity) {
      clear();
      _identity = identity;
    }
    if (_ready != null && !_now().isBefore(_expires)) {
      final expired = _ready!;
      _ready = null;
      _release(expired);
    }
    if (_ready != null ||
        loading ||
        _delay != null ||
        _now().isBefore(_retryAt)) {
      return;
    }
    final ticket = _generation;
    _delay = Timer(const Duration(milliseconds: 600), () async {
      _delay = null;
      loading = true;
      notifyListeners();
      try {
        final plan = await repository.preload(
          drama,
          episode,
          quality: quality,
          online: online,
        );
        if (_closed || ticket != _generation) {
          if (plan != null) await repository.release(plan.session);
          return;
        }
        if (plan == null || plan.url.isEmpty) throw AppFailure('未取得预加载结果');
        var expires = _now().add(const Duration(minutes: 2));
        if (plan.expiresAt > 0) {
          final authorizationExpiry = DateTime.fromMillisecondsSinceEpoch(
            plan.expiresAt,
          ).subtract(const Duration(seconds: 5));
          if (authorizationExpiry.isBefore(expires)) {
            expires = authorizationExpiry;
          }
        }
        if (!expires.isAfter(_now())) {
          _release(plan);
          throw AppFailure('下一集播放凭证即将过期');
        }
        _ready = plan;
        _expires = expires;
        error = '';
      } catch (failure) {
        if (_closed || ticket != _generation) return;
        error = failure.toString();
        _retryAt = _now().add(const Duration(seconds: 30));
      } finally {
        if (!_closed && ticket == _generation) {
          loading = false;
          notifyListeners();
        }
      }
    });
    notifyListeners();
  }

  PlaybackPlan? take(
    Drama drama,
    Episode episode, {
    int quality = 0,
    bool online = false,
  }) {
    final result =
        _identity == _key(drama, episode, quality, online) &&
            _now().isBefore(_expires)
        ? _ready
        : null;
    if (result != null) _ready = null;
    clear();
    return result;
  }

  void pause() {
    final changed = loading || _delay != null;
    _generation++;
    _delay?.cancel();
    _delay = null;
    if (loading) {
      unawaited(repository.cancelPreload().catchError((Object _) {}));
    }
    loading = false;
    if (changed && !_closed) notifyListeners();
  }

  void clear() {
    pause();
    final old = _ready;
    _ready = null;
    _identity = null;
    _retryAt = DateTime(2000);
    error = '';
    if (old != null) _release(old);
    if (!_closed) notifyListeners();
  }

  @override
  void dispose() {
    _closed = true;
    clear();
    super.dispose();
  }
}
