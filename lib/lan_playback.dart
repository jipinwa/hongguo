part of 'lan_controller.dart';

class _LanPreparedPlayback {
  _LanPreparedPlayback(this.id, this.intent, this.remote, this.previousHost);
  final String id;
  final LanPlaybackIntent intent;
  final LanConnection remote;
  final Object? previousHost;
  final DateTime created = DateTime.now();
  String state = 'preparing';
  String message = '';
  String replacing = '';
  bool cancelled = false;
  LanIncomingPlayback? playback;
  double? actualPosition;

  Map<String, dynamic> get result => {
    'id': id,
    'state': state,
    'message': message,
    'replacing': replacing,
    if (actualPosition != null) 'position': actualPosition,
  };
}

extension LanPlayback on LanController {
  void attachPlayback(LanPlaybackHost host) {
    playbackHost = host;
  }

  void detachPlayback(Object identity) {
    if (identical(playbackHost?.identity, identity)) playbackHost = null;
  }

  Future<void> _cancelIncomingPlayback({String? id}) async {
    final pending = _preparedPlayback;
    if (pending == null || id != null && pending.id != id) return;
    if (pending.state == 'playing') {
      if (id != null) {
        pending.cancelled = true;
        await pending.playback?.stop?.call();
        pending.state = 'cancelled';
        _playReceipts[pending.id] = pending.result;
      }
      if (identical(_preparedPlayback, pending)) _preparedPlayback = null;
      return;
    }
    pending.cancelled = true;
    pending.state = 'cancelled';
    pending.message = '推送已取消';
    pending.playback?.cancelled = true;
    pending.playback?.fail('推送已取消');
    await repository.cancelHandoff().catchError((Object _) {});
    final playback = pending.playback;
    if (playback != null) {
      if (playback.consumed) {
        await playback.stop?.call();
      } else {
        await repository
            .release(playback.plan.session)
            .catchError((Object _) {});
      }
    }
    _playReceipts[pending.id] = pending.result;
    if (identical(_preparedPlayback, pending)) _preparedPlayback = null;
    _trimPlayReceipts();
  }

  void _trimPlayReceipts() {
    while (_playReceipts.length > 32) {
      _playReceipts.remove(_playReceipts.keys.first);
    }
  }

  Future<void> _expireIncomingPlayback() async {
    final pending = _preparedPlayback;
    if (pending == null ||
        pending.state == 'playing' ||
        pending.state == 'starting')
      return;
    if (DateTime.now().difference(pending.created) >
        const Duration(minutes: 2)) {
      await _cancelIncomingPlayback();
    }
  }

  Future<Map<String, dynamic>> _receivePlayback(
    String path,
    Map<String, dynamic> body,
    LanConnection remote,
  ) async {
    final id = lanText(body['id'], 32);
    if (!RegExp(r'^[a-f0-9]{32}$').hasMatch(id))
      throw const FormatException('播放交接标识无效');
    var pending = _preparedPlayback;
    if (path == 'play/cancel') {
      if (pending?.id == id && pending?.remote == remote)
        await _cancelIncomingPlayback(id: id);
      _playReceipts.putIfAbsent(id, () => {'id': id, 'state': 'cancelled'});
      _trimPlayReceipts();
      return _playReceipts[id]!;
    }
    if (pending?.id != id && _playReceipts.containsKey(id))
      return _playReceipts[id]!;
    if (path == 'play/prepare') {
      final intent = LanPlaybackIntent.fromJson(body['intent']);
      if (!store.allowsSource(intent.drama.source) ||
          !remote.sources.contains(intent.drama.source)) {
        throw StateError('接收设备当前用户没有此站源权限');
      }
      if (!intent.playing || openPlayback == null)
        throw StateError('接收设备暂不能开始播放');
      if (pending != null && pending.id == id) {
        if (pending.intent.identity != intent.identity ||
            pending.remote != remote) {
          throw StateError('重复的播放交接内容不一致');
        }
        return pending.result;
      }
      if (pending != null &&
          {'preparing', 'prepared', 'starting'}.contains(pending.state)) {
        throw StateError('接收设备正在处理另一次推送');
      }
      pending = _LanPreparedPlayback(
        id,
        intent,
        remote,
        playbackHost?.identity,
      );
      pending.replacing = playbackHost?.title ?? '';
      _preparedPlayback = pending;
      unawaited(_preparePlayback(pending));
      return pending.result;
    }
    if (pending == null || pending.id != id || pending.remote != remote) {
      throw StateError('播放准备已过期，请重新推送');
    }
    if (path == 'play/status') return pending.result;
    if (path == 'play/start') {
      if (pending.state != 'prepared') return pending.result;
      if (playbackHost?.identity != pending.previousHost) {
        await _cancelIncomingPlayback(id: id);
        throw StateError('接收端播放内容已变化，请重新推送');
      }
      final position = body['position'];
      if (position is! num ||
          !position.isFinite ||
          position < 0 ||
          position > 604800) {
        throw const FormatException('推送播放位置无效');
      }
      if (body['identity'] != pending.intent.identity)
        throw StateError('推送的分集已变化');
      pending.playback!.position = position.toDouble();
      pending.state = 'starting';
      unawaited(_startPlayback(pending));
      return pending.result;
    }
    throw StateError('不支持此播放交接操作');
  }

  Future<void> _preparePlayback(_LanPreparedPlayback pending) async {
    final run = _run;
    PlaybackPlan? plan;
    try {
      DramaDetail? detail;
      if (store.canDownload && repository.supportsDownloads) {
        final jobs =
            (await repository.downloads())
                .where(
                  (job) =>
                      job.completed && job.drama.id == pending.intent.drama.id,
                )
                .toList()
              ..sort((a, b) => a.episode.number.compareTo(b.episode.number));
        if (pending.cancelled || _preparedPlayback != pending || _run != run)
          return;
        final selected = jobs
            .where(
              (job) =>
                  job.episode.number == pending.intent.episode &&
                  (pending.intent.episodeID.isEmpty ||
                      job.episode.id == pending.intent.episodeID),
            )
            .firstOrNull;
        if (selected != null) {
          try {
            plan = await repository.localPlayback(
              selected.drama,
              selected.episode,
            );
          } on AppFailure catch (failure) {
            if (failure.code != 'local_media') rethrow;
          }
          if (plan != null) {
            detail = DramaDetail(
              selected.drama,
              jobs.map((job) => job.episode).toList(),
              warning: '当前仅列出接收设备已下载的分集',
            );
          }
        }
      }
      if (pending.cancelled || _preparedPlayback != pending || _run != run) {
        if (plan != null)
          await repository.release(plan.session).catchError((Object _) {});
        return;
      }
      detail ??= await repository.detail(pending.intent.drama);
      if (pending.cancelled || _preparedPlayback != pending || _run != run) {
        if (plan != null)
          await repository.release(plan.session).catchError((Object _) {});
        return;
      }
      if (detail.drama.id != pending.intent.drama.id)
        throw StateError('接收端剧目身份不一致');
      final index = detail.episodes.indexWhere(
        (episode) =>
            episode.number == pending.intent.episode &&
            (pending.intent.episodeID.isEmpty ||
                episode.id == pending.intent.episodeID),
      );
      if (index < 0) throw StateError('接收端尚未取得这一个分集，请更新剧目后重试');
      plan ??= await repository.prepareHandoff(
        detail.drama,
        detail.episodes[index],
        quality: store.playbackPreferences.quality,
      );
      if (plan == null || plan.url.isEmpty) throw StateError('接收端未能取得播放资源');
      if (pending.cancelled ||
          _preparedPlayback != pending ||
          _run != run ||
          !receiving ||
          connection != pending.remote ||
          store.profileEpoch != _sessionEpoch) {
        await repository.release(plan.session);
        return;
      }
      pending.playback = LanIncomingPlayback(
        id: pending.id,
        intent: pending.intent,
        detail: detail,
        index: index,
        plan: plan,
        profileEpoch: _sessionEpoch,
      );
      pending.state = 'prepared';
    } catch (failure) {
      if (plan != null && pending.playback == null) {
        await repository.release(plan.session).catchError((Object _) {});
      }
      if (_preparedPlayback == pending && !pending.cancelled) {
        pending.state = 'failed';
        pending.message = failure.toString();
        _playReceipts[pending.id] = pending.result;
        _trimPlayReceipts();
      }
    }
  }

  Future<void> _startPlayback(_LanPreparedPlayback pending) async {
    final run = _run;
    final playback = pending.playback!;
    try {
      if (pending.cancelled || !receiving || connection != pending.remote) {
        throw StateError('设备连接已变更');
      }
      await openPlayback!(playback);
      final result = await playback.started.future.timeout(
        const Duration(seconds: 40),
        onTimeout: () => {'state': 'failed', 'message': '接收端尚未确认定位并开播，请重试'},
      );
      if (pending.cancelled || run != _run || _preparedPlayback != pending) {
        await playback.stop?.call();
        return;
      }
      if (result['state'] != 'playing')
        throw StateError(result['message'] as String? ?? '接收端播放失败');
      pending.state = 'playing';
      pending.actualPosition = (result['position'] as num).toDouble();
      _playReceipts[pending.id] = pending.result;
      _trimPlayReceipts();
    } catch (failure) {
      if (!playback.consumed) {
        await repository
            .release(playback.plan.session)
            .catchError((Object _) {});
      } else {
        await playback.stop?.call();
      }
      if (_preparedPlayback == pending && !pending.cancelled) {
        pending.state = 'failed';
        pending.message = failure.toString();
        _playReceipts[pending.id] = pending.result;
        _trimPlayReceipts();
      }
    }
  }

  Future<void> pushPlayback({
    required LanPlaybackIntent Function() snapshot,
    required bool Function() stillCurrent,
    required Future<void> Function() onAccepted,
    required Future<bool> Function(String title) confirmReplace,
  }) async {
    if (_pushing) throw StateError('正在推送，请稍候');
    final remote = connection;
    if (remote == null) throw StateError('请先选择接收设备');
    final intent = snapshot();
    if (!remote.sources.contains(intent.drama.source))
      throw StateError('对方当前用户没有此站源权限');
    final run = _run;
    final id = lanID();
    final ticket = ++_pushSequence;
    var accepted = false;
    _pushing = true;
    pushMessage = '对方准备中';
    _notify();
    bool valid() =>
        !_disposed &&
        ticket == _pushSequence &&
        run == _run &&
        connection == remote &&
        _pushing &&
        stillCurrent() &&
        snapshot().identity == intent.identity;
    try {
      await _request('play/prepare', {
        'id': id,
        'intent': intent.toJson(),
      }, owner: 'play');
      final ready = await _waitPlayback(
        id,
        'prepared',
        valid,
        const Duration(seconds: 75),
      );
      final replacing = ready['replacing'] as String? ?? '';
      if (replacing.isNotEmpty && !await confirmReplace(replacing)) {
        throw StateError('已取消推送，本机继续播放');
      }
      if (!valid()) throw StateError('本机播放内容已变更，推送已取消');
      final latest = snapshot();
      pushMessage = '对方正在定位并开播';
      _notify();
      await _request('play/start', {
        'id': id,
        'identity': intent.identity,
        'position': latest.position,
      }, owner: 'play');
      await _waitPlayback(id, 'playing', valid, const Duration(seconds: 50));
      if (!valid()) throw StateError('播放内容已变更，忽略过期交接');
      await onAccepted();
      accepted = true;
      pushMessage = '已在 ' + remote.peer.name + ' 播放';
      flush();
    } catch (failure) {
      if (ticket == _pushSequence) pushMessage = failure.toString();
      rethrow;
    } finally {
      if (!accepted && connection == remote) {
        await _request('play/cancel', {
          'id': id,
        }).catchError((Object _) => <String, dynamic>{});
      }
      if (ticket == _pushSequence) _pushing = false;
      _notify();
    }
  }

  Future<Map<String, dynamic>> _waitPlayback(
    String id,
    String goal,
    bool Function() valid,
    Duration timeout,
  ) async {
    final deadline = DateTime.now().add(timeout);
    while (DateTime.now().isBefore(deadline)) {
      if (!valid()) throw StateError('推送已取消，本机继续播放');
      final response = await _request('play/status', {'id': id}, owner: 'play');
      if (response['state'] == goal) return response;
      if (response['state'] == 'failed' || response['state'] == 'cancelled') {
        throw StateError(response['message'] as String? ?? '对方未能完成播放');
      }
      await Future<void>.delayed(const Duration(milliseconds: 650));
    }
    throw StateError('等待对方播放超时，本机继续播放');
  }

  Future<void> cancelPush() async {
    _pushSequence++;
    _pushing = false;
    for (final id in _playRequests.toList()) {
      if (!receiving) break;
      await _native('cancel', {
        'requestId': id,
      }).catchError((Object _) => <String, dynamic>{});
    }
    pushMessage = '已取消推送，本机继续播放';
    _notify();
  }
}
