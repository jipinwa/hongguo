part of 'media_library.dart';

class MergeQueueJob {
  const MergeQueueJob({
    required this.id,
    required this.owner,
    required this.drama,
    required this.jobIds,
    required this.episodes,
    required this.versions,
    required this.created,
    this.state = 'queued',
    this.error = '',
    this.cleanup = false,
  });
  final String id, owner, state, error;
  final Drama drama;
  final List<String> jobIds;
  final List<int> episodes;
  final Map<String, String> versions;
  final DateTime created;
  final bool cleanup;
  String get outputId => 'merged-$id';
  bool get active =>
      state == 'queued' || state == 'running' || state == 'cleanup';
  String get label => switch (state) {
    'queued' => '等待合并',
    'running' => '正在合并',
    'cleanup' => '正在清理原分集',
    'paused' => '已暂停，可继续',
    'failed' => '处理失败，可重试',
    'completed' => '已完成',
    'cancelled' => '已取消',
    _ => '等待更新',
  };
  MergeQueueJob copyWith({String? state, String? error}) => MergeQueueJob(
    id: id,
    owner: owner,
    drama: drama,
    jobIds: jobIds,
    episodes: episodes,
    versions: versions,
    created: created,
    state: state ?? this.state,
    error: error ?? this.error,
    cleanup: cleanup,
  );
  Map<String, dynamic> toJson() => {
    'id': id,
    'owner': owner,
    'drama': drama.toJson(),
    'jobIds': jobIds,
    'episodes': episodes,
    'versions': versions,
    'created': created.toIso8601String(),
    'state': state,
    'error': error,
    'cleanup': cleanup,
  };
  factory MergeQueueJob.fromJson(Map<String, dynamic> data) {
    final id = data['id'] as String,
        owner = data['owner'] as String,
        state = data['state'] as String;
    final ids = (data['jobIds'] as List).cast<String>();
    final episodes = (data['episodes'] as List).cast<int>();
    final versions = Map<String, String>.from(data['versions'] as Map);
    if (!RegExp(r'^[a-zA-Z0-9_-]{1,100}$').hasMatch(id) ||
        !RegExp(r'^[a-zA-Z0-9_-]{1,64}$').hasMatch(owner) ||
        !{
          'queued',
          'running',
          'cleanup',
          'paused',
          'failed',
          'completed',
          'cancelled',
        }.contains(state) ||
        ids.length < 2 ||
        ids.length > 10000 ||
        ids.toSet().length != ids.length ||
        ids.length != episodes.length ||
        ids.any(
          (id) =>
              !RegExp(r'^[a-zA-Z0-9_-]{1,100}$').hasMatch(id) ||
              !versions.containsKey(id),
        ) ||
        episodes.any((episode) => episode < 1 || episode > 100000) ||
        episodes.indexed.any(
          (row) => row.$1 > 0 && row.$2 != episodes[row.$1 - 1] + 1,
        )) {
      throw const FormatException('合并队列记录无效');
    }
    return MergeQueueJob(
      id: id,
      owner: owner,
      drama: Drama.fromJson(Map<String, dynamic>.from(data['drama'] as Map)),
      jobIds: ids,
      episodes: episodes,
      versions: versions,
      created: DateTime.parse(data['created'] as String),
      state: state,
      error: data['error'] as String? ?? '',
      cleanup: data['cleanup'] == true,
    );
  }
}

class MergeQueue {
  MergeQueue(this.library) {
    if (!library.automaticWorker) {
      _timer = Timer.periodic(const Duration(seconds: 3), (_) => _start());
    }
  }
  final MediaLibrary library;
  List<MergeQueueJob> _jobs = [];
  Future<void> _writes = Future.value();
  Future<void>? _loading;
  Timer? _timer;
  String? _root, _running;
  bool _disposed = false, _loaded = false, _pumping = false;
  String error = '';
  List<MergeQueueJob> get items => _jobs
      .where(
        (job) =>
            library.store.canDownload &&
            job.owner == library.store.profile.id &&
            library.store.allowsSource(job.drama.source),
      )
      .toList();
  bool get running => _running != null;
  MergeQueueJob? _find(String id) =>
      _jobs.where((job) => job.id == id).firstOrNull;

  Future<void> load() {
    if (_loading != null) return _loading!;
    final future = _load();
    _loading = future;
    return future.whenComplete(() => _loading = null);
  }

  Future<void> _load() async {
    if (_disposed || library.automaticWorker) return;
    final root = await library.repository.downloadDirectory();
    if (_loaded && _root == root) return;
    if (_running != null) return;
    await library.repository.workLease('mergeQueue', 'start');
    try {
      _root = await library.repository.downloadDirectory();
      final file = File(path.join(_root!, 'merge-queue.json'));
      var jobs = <MergeQueueJob>[];
      if (await file.exists()) {
        if (await file.length() > 8 * 1024 * 1024) {
          throw AppFailure('合并队列过大，原记录已保留');
        }
        final data = jsonDecode(await file.readAsString()) as Map;
        if (data['schema'] != 1 ||
            data['jobs'] is! List ||
            (data['jobs'] as List).length > 500) {
          throw AppFailure('合并队列无法读取，原记录已保留');
        }
        jobs = (data['jobs'] as List)
            .map(
              (entry) => MergeQueueJob.fromJson(
                Map<String, dynamic>.from(entry as Map),
              ),
            )
            .toList();
        if (jobs.map((job) => job.id).toSet().length != jobs.length) {
          throw AppFailure('合并队列编号重复，原记录已保留');
        }
      }
      final interrupted = jobs.any((job) => job.active);
      _jobs = jobs
          .map(
            (job) => job.active
                ? job.copyWith(state: 'paused', error: '上次处理已中断，可从已保存的进度继续')
                : job,
          )
          .toList();
      if (interrupted) await _save();
      _loaded = true;
      error = '';
      library.notifyListeners();
    } catch (failure) {
      error = failure.toString();
      rethrow;
    } finally {
      await library.repository.workLease('mergeQueue', 'end');
    }
  }

  Future<void> _save() {
    final content = jsonEncode({
      'schema': 1,
      'jobs': _jobs.map((job) => job.toJson()).toList(),
    });
    if (utf8.encode(content).length > 8 * 1024 * 1024) {
      throw AppFailure('合并队列记录已满，请先清理已结束的任务');
    }
    return library._writeText(
      File(path.join(_root!, 'merge-queue.json')),
      content,
    );
  }

  Future<void> _change(void Function() update) {
    final task = _writes.then((_) async {
      if (_disposed || !_loaded) return;
      var lease = false;
      final previous = List<MergeQueueJob>.of(_jobs);
      try {
        if (library.store.canDownload) {
          await library.repository.workLease('mergeQueue', 'start');
          lease = true;
          _root = await library.repository.downloadDirectory();
        }
        update();
        await _save();
        error = '';
      } catch (failure) {
        _jobs = previous;
        error = failure.toString();
        rethrow;
      } finally {
        if (lease) await library.repository.workLease('mergeQueue', 'end');
        library.notifyListeners();
      }
    });
    _writes = task.catchError((Object _) {});
    return task;
  }

  Future<int> enqueue(
    List<List<DownloadJob>> groups, {
    bool cleanup = false,
  }) async {
    final epoch = library.store.profileEpoch;
    if (!library.store.canDownload || groups.isEmpty || groups.length > 50) {
      throw AppFailure('一次请选择 1 至 50 部剧加入合并队列');
    }
    final selections = groups.map(continuousMergeJobs).toList();
    for (final jobs in selections) {
      if (!library.store.allowsSource(jobs.first.drama.source)) {
        throw AppFailure('当前用户无权合并此站源内容');
      }
    }
    await load();
    if (epoch != library.store.profileEpoch || !library.store.canDownload) {
      throw AppFailure('用户已切换，请重新操作');
    }
    var count = 0;
    await _change(() {
      if (epoch != library.store.profileEpoch || !library.store.canDownload) {
        throw AppFailure('用户已切换，请重新操作');
      }
      for (final jobs in selections) {
        final ids = jobs.map((job) => job.id).toList();
        final versions = {
          for (final job in jobs) job.id: library._sourceVersion(job),
        };
        if (_jobs.any(
          (job) =>
              job.owner == library.store.profile.id &&
              job.state != 'cancelled' &&
              job.state != 'completed' &&
              listEquals(job.jobIds, ids) &&
              mapEquals(job.versions, versions),
        )) {
          continue;
        }
        if (_jobs.length >= 500) throw AppFailure('合并队列记录已满，请清理已结束的任务记录');
        _jobs.add(
          MergeQueueJob(
            id: 'q${DateTime.now().microsecondsSinceEpoch}-$count',
            owner: library.store.profile.id,
            drama: jobs.first.drama,
            jobIds: ids,
            episodes: jobs.map((job) => job.episode.number).toList(),
            versions: versions,
            created: DateTime.now(),
            cleanup: cleanup,
          ),
        );
        count++;
      }
    });
    _start();
    return count;
  }

  Future<void> control(String id, String command) async {
    await load();
    final job = _find(id);
    if (job == null || !items.any((job) => job.id == id)) {
      throw AppFailure('合并任务不存在或当前用户无权操作');
    }
    final epoch = library.store.profileEpoch;
    if (command == 'resume' && _running == id) {
      throw AppFailure('正在结束当前步骤，请稍后继续');
    }
    await _change(() {
      if (epoch != library.store.profileEpoch) throw AppFailure('用户已切换，请重新操作');
      final index = _jobs.indexWhere((job) => job.id == id);
      if (index < 0) return;
      if (command == 'forget') {
        if (_running == id || _jobs[index].active) throw AppFailure('请先停止任务');
        _jobs.removeAt(index);
      } else {
        final state = switch (command) {
          'pause' => 'paused',
          'resume' => 'queued',
          'cancel' => 'cancelled',
          _ => throw AppFailure('不支持的队列操作'),
        };
        if (_jobs[index].state == 'completed' && command != 'cancel') return;
        _jobs[index] = _jobs[index].copyWith(state: state, error: '');
      }
    });
    if (_running == id && command != 'resume') await library.cancel();
    if (_running != id && (command == 'cancel' || command == 'forget')) {
      await _cleanWorkspace(id);
    }
    _start();
  }

  Future<void> pauseUnavailable({bool all = false}) async {
    if (!_loaded || _disposed) return;
    try {
      await _change(() {
        _jobs = _jobs
            .map(
              (job) =>
                  job.active &&
                      (all ||
                          !library.store.canDownload ||
                          job.owner != library.store.profile.id ||
                          !library.store.allowsSource(job.drama.source))
                  ? job.copyWith(state: 'paused', error: '任务已暂停，原分集和处理进度保留')
                  : job,
            )
            .toList();
      });
    } catch (_) {}
  }

  Future<void> _setState(String id, String state, [String message = '']) =>
      _change(() {
        final index = _jobs.indexWhere((job) => job.id == id);
        if (index < 0 ||
            state == 'running' && _jobs[index].state != 'queued' ||
            {'cancelled', 'paused', 'completed'}.contains(_jobs[index].state)) {
          return;
        }
        _jobs[index] = _jobs[index].copyWith(state: state, error: message);
      });

  void _start() {
    if (_disposed ||
        !_loaded ||
        _pumping ||
        library.busy ||
        library.suspended ||
        !library.store.canDownload) {
      return;
    }
    unawaited(
      _pump().catchError((Object failure) {
        error = failure.toString();
        library.notifyListeners();
      }),
    );
  }

  bool _allowed(MergeQueueJob job, int epoch) =>
      !_disposed &&
      library.store.canDownload &&
      !library.suspended &&
      epoch == library.store.profileEpoch &&
      job.owner == library.store.profile.id &&
      library.store.allowsSource(job.drama.source) &&
      _find(job.id)?.active == true;

  Future<void> _pump() async {
    _pumping = true;
    try {
      while (!_disposed &&
          !library.busy &&
          !library.suspended &&
          library.store.canDownload) {
        final job = items.where((job) => job.state == 'queued').firstOrNull;
        if (job == null) break;
        final epoch = library.store.profileEpoch;
        _running = job.id;
        try {
          await _setState(job.id, 'running');
          if (!_allowed(job, epoch)) break;
          final item = await library._mergeQueued(job);
          if (!_allowed(job, epoch)) continue;
          if (job.cleanup) {
            await _setState(job.id, 'cleanup');
            await _cleanupSources(job, item, epoch);
          }
          if (!_allowed(job, epoch)) continue;
          await _setState(job.id, 'completed');
          await _cleanWorkspace(job.id);
        } catch (failure) {
          if (_find(job.id)?.active == true) {
            final message = failure.toString();
            if (message.contains('已有本地任务') && _allowed(job, epoch)) {
              await _setState(job.id, 'queued');
              break;
            }
            await _setState(
              job.id,
              !_allowed(job, epoch) || library._cancelled ? 'paused' : 'failed',
              message,
            );
          }
        } finally {
          if (_find(job.id)?.state == 'cancelled') {
            await _cleanWorkspace(job.id);
          }
          _running = null;
          library.notifyListeners();
        }
      }
    } finally {
      _pumping = false;
      _running = null;
    }
  }

  Future<void> _cleanupSources(
    MergeQueueJob task,
    LocalMediaItem output,
    int epoch,
  ) async {
    if (!_allowed(task, epoch)) throw AppFailure('原分集清理已停止');
    await library.repository.workLease('mergeCleanup', 'start');
    try {
      final file = File(library.fileFor(output));
      if (!output.decodeVerified ||
          !await file.exists() ||
          await file.length() != output.bytes) {
        throw AppFailure('合并成品未确认完整，已保留原分集');
      }
      final current = {
        for (final job in await library.repository.downloads()) job.id: job,
      };
      final ids = <String>[];
      for (final id in task.jobIds) {
        final job = current[id];
        if (job == null) continue;
        if (!job.completed ||
            library._sourceVersion(job) != task.versions[id]) {
          throw AppFailure('原分集已改变，已保留文件；合并成品仍可播放');
        }
        ids.add(id);
      }
      for (var offset = 0; offset < ids.length; offset += 500) {
        if (!_allowed(task, epoch)) throw AppFailure('原分集清理已停止，合并成品保留');
        final chunk = ids.skip(offset).take(500).toList();
        final result = await library.repository.controlDownloadBatch(
          'remove',
          chunk,
          expectedVersions: {for (final id in chunk) id: task.versions[id]!},
        );
        if (result.failures.isNotEmpty) {
          throw AppFailure('成品已保存，部分原分集未能清理：${result.failures.values.first}');
        }
      }
    } finally {
      await library.repository.workLease('mergeCleanup', 'end');
    }
  }

  Future<void> _cleanWorkspace(String id) async {
    if (_root == null) return;
    final directory = Directory(path.join(_root!, '.merge-work', id));
    try {
      if (await directory.exists()) await directory.delete(recursive: true);
    } catch (_) {}
  }

  void dispose() {
    _disposed = true;
    _timer?.cancel();
  }
}
