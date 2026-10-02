import 'dart:async';
import 'dart:io';

import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/source_status.dart';

import 'fixtures.dart';

class LibraryFeatureRepository extends FixtureRepository {
  final statuses = <String, SourceStatus>{};
  final starts = <String>[];
  final stops = <String>[];
  final detailRequests = <String>[];
  final details = <String, DramaDetail>{};
  final detailFailures = <String>{};
  final startFailures = <String>{};
  final queued = <String>{};
  final enqueues = <(String, List<int>, int)>[];
  Completer<SourceStatus>? pendingStart;
  Completer<DramaDetail>? pendingDetail;
  Completer<void>? pendingEnqueue;
  int? failChunkStartingAt;

  static const first = Drama(
    id: 'hongguo:one',
    source: 'hongguo',
    title: '合成一号',
    episodes: 2,
  );
  static const second = Drama(
    id: 'hongguo:two',
    source: 'hongguo',
    title: '合成二号',
    episodes: 2,
  );

  LibraryFeatureRepository() {
    cachedPages['hongguo'] = CatalogPage(
      [first, second],
      fresh: true,
      page: 3,
      hasMore: true,
    );
    details[first.id] = makeDetail(first, 2);
    details[second.id] = makeDetail(second, 2);
  }

  static DramaDetail makeDetail(Drama drama, int count, {int vipFrom = 0}) =>
      DramaDetail(drama, [
        for (var number = 1; number <= count; number++)
          Episode({
            'id': '${drama.id}:$number',
            'currentEpisode': number,
            'vip': vipFrom > 0 && number >= vipFrom,
          }, number),
      ]);

  SourceStatus running(String source) => SourceStatus.fromJson({
    'source': source,
    'operation': 'update',
    'running': true,
    'stage': '查找新剧',
    'startedAt': '2026-09-21T08:00:00Z',
  });

  SourceStatus finished(String source) => SourceStatus.fromJson({
    'source': source,
    'operation': 'update',
    'running': false,
    'stage': '已完成',
    'startedAt': '2026-09-21T08:00:00Z',
    'finishedAt': '2026-09-21T08:01:00Z',
    'updatedAt': '2026-09-21T08:01:00Z',
    'added': 1,
    'count': 3,
    'page': 4,
  });

  @override
  bool get supportsSourceManagement => true;
  @override
  bool get supportsDownloads => true;

  @override
  Future<String> cover(Drama drama, {bool force = false}) async =>
      File('test/fixtures/cover.png').absolute.path;

  @override
  Future<SourceStatus> sourceStatus(String source) async =>
      statuses[source] ?? SourceStatus.fromJson({'source': source});

  @override
  Future<SourceStatus> startSourceJob(
    String source,
    String operation, {
    Drama? drama,
  }) async {
    starts.add('$source:$operation');
    if (startFailures.contains(source)) throw AppFailure('合成站源启动失败');
    return statuses[source] = pendingStart == null
        ? running(source)
        : await pendingStart!.future;
  }

  @override
  Future<SourceStatus> cancelSourceJob(String source) async {
    stops.add(source);
    return statuses[source] = SourceStatus.fromJson({
      'source': source,
      'operation': 'update',
      'running': false,
      'stage': '已停止',
      'finishedAt': '2026-09-21T08:01:00Z',
    });
  }

  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async {
    requests.add(source);
    pages.add(page);
    forced.add(force);
    return CatalogPage(cachedPages[source]?.items ?? [], page: page);
  }

  @override
  Future<DramaDetail> detail(Drama drama) async {
    detailRequests.add(drama.id);
    if (detailFailures.contains(drama.id)) throw AppFailure('合成分集读取失败');
    if (pendingDetail != null) return pendingDetail!.future;
    return details[drama.id] ??
        makeDetail(drama, drama.episodes.clamp(1, 2).toInt());
  }

  @override
  Future<int> enqueueDownloads(
    DramaDetail detail,
    List<Episode> episodes, {
    int quality = 0,
  }) async {
    enqueues.add((
      detail.drama.id,
      episodes.map((episode) => episode.number).toList(),
      quality,
    ));
    if (failChunkStartingAt == episodes.first.number) {
      failChunkStartingAt = null;
      throw AppFailure('合成添加失败');
    }
    if (pendingEnqueue != null) await pendingEnqueue!.future;
    var added = 0;
    for (final episode in episodes) {
      if (queued.add('${detail.drama.id}:${episode.number}')) added++;
    }
    return added;
  }
}
