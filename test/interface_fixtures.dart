import 'dart:async';

import 'package:duanju_app/models.dart';

import 'fixtures.dart';

class InterfaceRepository extends FixtureRepository {
  Completer<CatalogPage>? pendingCatalog;
  final commands = <String>[];
  List<DownloadJob> jobs = [];

  InterfaceRepository() {
    final drama = dramas('hongguo').first;
    jobs = [
      for (final entry in ['downloading', 'paused', 'completed'].indexed)
        DownloadJob(
          id: 'task-${entry.$1}',
          drama: drama,
          episode: Episode({'currentEpisode': entry.$1 + 1}, entry.$1 + 1),
          state: entry.$2,
          bytes: 1024 * 1024 * (entry.$1 + 1),
          total: 4 * 1024 * 1024,
          progress: .5,
          actualQuality: 1080,
        ),
    ];
  }

  List<Drama> dramas(String source) => [
    Drama(id: '$source:1', source: source, title: '短标题', episodes: 12),
    Drama(
      id: '$source:2',
      source: source,
      title: '这是用于检查两行标题与海报对齐的合成短剧',
      category: '合成分类',
      episodes: 30,
    ),
    Drama(
      id: '$source:3',
      source: source,
      title: '会员合成剧',
      category: '用于检查长分类文字的显示范围',
      vip: true,
    ),
  ];

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
    return pendingCatalog == null
        ? CatalogPage(dramas(source), page: page)
        : await pendingCatalog!.future;
  }

  @override
  bool get supportsDownloads => true;

  @override
  Future<List<DownloadJob>> downloads() async => List.of(jobs);

  @override
  Future<void> controlDownloads(String command, {String id = ''}) async {
    commands.add('$command:$id');
    jobs = [
      for (final job in jobs)
        if (command != 'remove' || job.id != id)
          DownloadJob(
            id: job.id,
            drama: job.drama,
            episode: job.episode,
            state:
                (command == 'pauseAll' || command == 'pause' && job.id == id) &&
                    job.active
                ? 'paused'
                : (command == 'resumeAll' ||
                          command == 'resume' && job.id == id) &&
                      job.resumable
                ? 'queued'
                : job.state,
            bytes: job.bytes,
            total: job.total,
            progress: job.progress,
            actualQuality: job.actualQuality,
          ),
    ];
  }
}
