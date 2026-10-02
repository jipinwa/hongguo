import 'dart:math';

import 'package:flutter/foundation.dart';

import 'core_bridge.dart';
import 'local_store.dart';
import 'models.dart';

class DownloadBatchResult {
  const DownloadBatchResult({
    this.completed = const [],
    this.failures = const {},
  });
  final List<String> completed;
  final Map<String, String> failures;
  factory DownloadBatchResult.fromJson(Map<String, dynamic> data) =>
      DownloadBatchResult(
        completed: (data['completed'] as List? ?? [])
            .whereType<String>()
            .toList(),
        failures: (data['failures'] as Map? ?? {}).map(
          (key, value) => MapEntry('$key', '$value'),
        ),
      );
}

class DownloadCollection {
  DownloadCollection(this.drama, this.jobs);
  final Drama drama;
  final List<DownloadJob> jobs;
  int get completed => jobs.where((job) => job.completed).length;
  int get active => jobs.where((job) => job.active).length;
  int get failed => jobs.where((job) => job.state == 'failed').length;
  int get bytes => jobs.fold(0, (total, job) => total + job.bytes);
}

List<DownloadCollection> downloadCollections(
  Iterable<DownloadJob> jobs, {
  String query = '',
  String filter = 'all',
  String release = '',
}) {
  final groups = <String, List<DownloadJob>>{};
  final terms = query
      .trim()
      .toLowerCase()
      .split(RegExp(r'\s+'))
      .where((term) => term.isNotEmpty)
      .toList();
  for (final job in jobs) {
    if (filter == 'archived' ? !job.archived : job.archived) continue;
    if (release.isNotEmpty &&
        job.drama.releaseStatus != release &&
        !(release == 'finished' && job.drama.releaseStatus == 'completed')) {
      continue;
    }
    final text =
        '${job.drama.title} ${SourceSite.byId(job.drama.source).name} ${job.drama.category}'
            .toLowerCase();
    if (terms.any((term) => !text.contains(term))) continue;
    if (!(switch (filter) {
      'all' || 'archived' => true,
      'pending' => !job.completed,
      'active' => job.active,
      'completed' => job.completed,
      _ => job.state == filter,
    })) {
      continue;
    }
    (groups[job.drama.id] ??= []).add(job);
  }
  final collections = [
    for (final rows in groups.values)
      DownloadCollection(
        rows.first.drama,
        rows..sort((a, b) => a.episode.number.compareTo(b.episode.number)),
      ),
  ];
  collections.sort(
    (a, b) => b.jobs
        .map((j) => j.created)
        .reduce(max)
        .compareTo(a.jobs.map((j) => j.created).reduce(max)),
  );
  return collections;
}

class DownloadCollectionUpdater extends ChangeNotifier {
  DownloadCollectionUpdater(this.repository, this.store)
    : _epoch = store.profileEpoch;
  final AppRepository repository;
  final LocalStore store;
  final int _epoch;
  bool _disposed = false;
  bool _stop = false;
  bool busy = false;
  String status = '';
  String? error;
  int added = 0;

  bool get valid =>
      !_disposed && store.canDownload && store.profileEpoch == _epoch;
  void _check(String source) {
    if (!valid || _stop || !store.allowsSource(source)) {
      throw AppFailure('更新已停止；已加入的任务保留');
    }
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void cancel() {
    _stop = true;
  }

  Future<void> update(Drama drama) async {
    if (busy || !valid) return;
    busy = true;
    _stop = false;
    error = null;
    added = 0;
    status = '正在读取 ${drama.title} 的最新分集';
    _notify();
    try {
      _check(drama.source);
      final fresh = await repository.detail(drama);
      _check(drama.source);
      if (fresh.drama.id != drama.id || fresh.drama.source != drama.source) {
        throw AppFailure('剧集信息不匹配，请重试');
      }
      final detail = DramaDetail(drama.merge(fresh.drama), fresh.episodes);
      if (detail.episodes.isEmpty) throw AppFailure('暂未取得分集，请更新资料后重试');
      repository.catalogUpdates.publish(detail.drama);
      await store.refreshDrama(detail.drama);
      _check(drama.source);
      final preferences = store.downloadPreferences;
      final episodes = {
        for (final episode in detail.episodes)
          if (preferences.includeVip || !episode.vip) episode.number: episode,
      }.values.toList();
      for (var offset = 0; offset < episodes.length; offset += 500) {
        _check(drama.source);
        status =
            '正在检查 ${drama.title} · ${min(offset + 500, episodes.length)}/${episodes.length} 集';
        _notify();
        added += await repository.updateDownloadCollection(
          detail,
          episodes.sublist(offset, min(offset + 500, episodes.length)),
          quality: preferences.quality,
        );
      }
      _check(drama.source);
      status = added == 0 ? '本剧暂无需要补充的分集' : '已加入 $added 集；已有完整文件和暂停中的任务保留';
    } catch (value) {
      error = value.toString();
    } finally {
      busy = false;
      _notify();
    }
  }

  @override
  void dispose() {
    _disposed = true;
    _stop = true;
    super.dispose();
  }
}
