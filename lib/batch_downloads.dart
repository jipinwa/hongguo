import 'dart:math';

import 'package:flutter/foundation.dart';

import 'core_bridge.dart';
import 'local_store.dart';
import 'models.dart';

class BatchDownloadItem {
  BatchDownloadItem(this.drama);

  final Drama drama;
  DramaDetail? detail;
  bool selected = true;
  bool loading = false;
  String? error;
  int added = 0;
  int existing = 0;
  final _submitted = <int>{};

  List<Episode> episodes(bool includeVip) => {
    for (final episode in detail?.episodes ?? <Episode>[])
      if (includeVip || !episode.vip) episode.number: episode,
  }.values.toList()..sort((a, b) => a.number.compareTo(b.number));

  List<Episode> pending(bool includeVip) => episodes(
    includeVip,
  ).where((episode) => !_submitted.contains(episode.number)).toList();
}

class BatchDownloads extends ChangeNotifier {
  BatchDownloads(this.repository, this.store, Iterable<Drama> dramas)
    : _epoch = store.profileEpoch,
      includeVip = store.downloadPreferences.includeVip,
      quality = store.downloadPreferences.quality,
      items = {
        for (final drama in dramas) drama.id: BatchDownloadItem(drama),
      }.values.toList() {
    if (items.length > maxDramas) throw AppFailure('一次最多选择 $maxDramas 部短剧');
  }

  static const maxDramas = 50;
  final AppRepository repository;
  final LocalStore store;
  final List<BatchDownloadItem> items;
  final int _epoch;
  bool _disposed = false;
  bool preparing = false;
  bool submitting = false;
  bool stopping = false;
  bool stopped = false;
  bool settingsLocked = false;
  bool includeVip;
  int quality;
  String warning = '';
  BatchDownloadItem? current;

  bool get _valid =>
      !_disposed &&
      repository.supportsDownloads &&
      store.canDownload &&
      store.profileEpoch == _epoch;
  bool get hasAccess => _valid;
  bool get busy => preparing || submitting;
  int get pendingEpisodes => items
      .where((item) => item.selected)
      .fold(0, (count, item) => count + item.pending(includeVip).length);
  int get added => items.fold(0, (count, item) => count + item.added);
  int get existing => items.fold(0, (count, item) => count + item.existing);
  int get unread =>
      items.where((item) => item.detail == null && item.selected).length;
  int get failures =>
      items.where((item) => item.error != null && item.selected).length;

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void setQuality(int value) {
    if (busy || settingsLocked || !{0, 1080, 720, 480}.contains(value)) return;
    quality = value;
    _notify();
  }

  void setIncludeVip(bool value) {
    if (busy || settingsLocked) return;
    includeVip = value;
    _notify();
  }

  void select(BatchDownloadItem item, bool value) {
    if (busy || settingsLocked) return;
    item.selected = value;
    _notify();
  }

  void stop() {
    if (!busy) return;
    stopping = true;
    stopped = true;
    _notify();
  }

  Future<void> prepare() async {
    if (busy || !_valid) return;
    preparing = true;
    stopping = false;
    stopped = false;
    warning = '';
    _notify();
    try {
      for (final item in items) {
        if (!_valid || stopping) break;
        if (!item.selected || item.detail != null) continue;
        item.error = null;
        if (!store.allowsSource(item.drama.source)) {
          item.error = '当前用户没有此站源权限';
          continue;
        }
        current = item;
        item.loading = true;
        _notify();
        try {
          final detail = await repository.detail(item.drama);
          if (!_valid || !store.allowsSource(item.drama.source)) break;
          if (detail.drama.id != item.drama.id ||
              detail.drama.source != item.drama.source) {
            throw AppFailure('返回的剧集信息不匹配，请重试');
          }
          if (detail.episodes.isEmpty ||
              detail.episodes.any((episode) => episode.number <= 0)) {
            throw AppFailure('未取得可下载的分集，请更新资料后重试');
          }
          final drama = item.drama.merge(detail.drama);
          item.detail = DramaDetail(
            drama,
            detail.episodes,
            warning: detail.warning,
          );
          repository.catalogUpdates.publish(drama);
        } catch (error) {
          if (_valid) item.error = error.toString();
        } finally {
          item.loading = false;
          _notify();
        }
      }
      if (_valid) {
        try {
          await store.refreshDramas([
            for (final item in items)
              if (item.detail != null) item.detail!.drama,
          ]);
        } catch (_) {
          warning = '分集已读取，但追剧资料未能保存；请检查存储空间后重试。';
        }
      }
    } finally {
      preparing = false;
      stopping = false;
      current = null;
      _notify();
    }
  }

  Future<void> submit() async {
    if (busy || !_valid || pendingEpisodes == 0) return;
    submitting = true;
    settingsLocked = true;
    stopping = false;
    stopped = false;
    _notify();
    try {
      for (final item in items) {
        if (!_valid || stopping) break;
        if (!item.selected || item.detail == null) continue;
        if (!store.allowsSource(item.drama.source)) {
          item.error = '当前用户没有此站源权限';
          continue;
        }
        final episodes = item.pending(includeVip);
        if (episodes.isEmpty) continue;
        item.error = null;
        current = item;
        _notify();
        for (var offset = 0; offset < episodes.length; offset += 500) {
          if (!_valid || stopping || !store.allowsSource(item.drama.source)) {
            break;
          }
          final chunk = episodes.sublist(
            offset,
            min(offset + 500, episodes.length),
          );
          try {
            final count = await repository.enqueueDownloads(
              item.detail!,
              chunk,
              quality: quality,
            );
            if (count < 0 || count > chunk.length) {
              throw AppFailure('未能确认添加数量，请重试');
            }
            item.added += count;
            item.existing += chunk.length - count;
            item._submitted.addAll(chunk.map((episode) => episode.number));
            _notify();
          } catch (error) {
            if (_valid) item.error = error.toString();
            break;
          }
        }
      }
    } finally {
      submitting = false;
      stopping = false;
      current = null;
      _notify();
    }
  }

  @override
  void dispose() {
    _disposed = true;
    stopping = true;
    super.dispose();
  }
}
