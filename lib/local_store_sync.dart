part of 'local_store.dart';

extension LocalStoreSync on LocalStore {
  Map<String, dynamic> _backupFollowSync(String profile) {
    final raw = _string(_key('lanRecords', profile));
    if (raw == null) return {};
    final document = LanDocument.fromJson(jsonDecode(raw));
    return {
      'followSync': document.records.values
          .map((record) => record.toJson())
          .toList(),
    };
  }

  Map<String, LanRecord>? _readBackupFollowSync(Map library) {
    final raw = library['followSync'];
    if (raw == null) return null;
    if (raw is! List || raw.length > LanDocument.limit) {
      throw const FormatException('备份同步记录数量无效');
    }
    final records = <String, LanRecord>{};
    for (final row in raw) {
      final record = LanRecord.fromJson(row);
      if (records.containsKey(record.id))
        throw const FormatException('备份同步记录重复');
      records[record.id] = record;
    }
    final favorites = (library['favorites'] as List)
        .map((row) => (row as Map)['id'])
        .toSet();
    final followed = records.values
        .where((record) => record.followed)
        .map((record) => record.id)
        .toSet();
    if (favorites.length != followed.length ||
        !followed.containsAll(favorites)) {
      throw const FormatException('备份同步记录与追剧列表不一致');
    }
    return records;
  }

  LanDocument get lanDocument {
    if (locked) throw StateError('请先解锁当前用户');
    final cached = _lanDocumentCache;
    if (cached != null) return cached.copy();
    final raw = _string(_key('lanRecords'));
    final document = raw == null
        ? LanDocument.empty()
        : LanDocument.fromJson(jsonDecode(raw));
    if (raw == null) {
      document.reconcile(
        previous: const {},
        favorites: _favorites,
        states: _followStates,
        history: _history,
        oldHistory: const {},
      );
    }
    _lanDocumentCache = document;
    return document.copy();
  }

  Map<String, dynamic> get lanSettings {
    if (locked) return {};
    return lanMap(jsonDecode(_string(_key('lanSettings')) ?? '{}'));
  }

  Future<void> saveLanSettings(Map<String, dynamic> value) =>
      _setting(_key('lanSettings'), jsonEncode(value));

  Future<void> ensureLanRecords() {
    final epoch = _epoch;
    return _queue(() async {
      if (locked || epoch != _epoch) throw StateError('当前用户已变更');
      final document = lanDocument;
      await _commit({
        _key('lanRecords'): jsonEncode(document.toJson()),
      }, trackSync: false);
      _lanDocumentCache = document;
    });
  }

  void _trackLanChanges(
    Map<String, Object> values, {
    required Set<String> keys,
    Set<String> clearProgress = const {},
  }) {
    if (locked ||
        !keys.any(
          (key) =>
              key == _key('favorites') ||
              key == _key('history') ||
              key == _key('followStates'),
        )) {
      return;
    }
    final document = lanDocument;
    final favorites = <String, Drama>{};
    final history = <String, WatchEntry>{};
    final states = <String, FollowState>{};
    for (final row in readJsonList(values[_key('favorites')] as String?)) {
      final drama = Drama.fromJson(row);
      favorites[drama.id] = drama;
    }
    for (final row in readJsonList(values[_key('history')] as String?)) {
      final watch = WatchEntry.fromJson(row);
      history[watch.drama.id] = watch;
    }
    final rawStates = lanMap(
      jsonDecode(values[_key('followStates')] as String? ?? '{}'),
    );
    for (final drama in favorites.values) {
      states[drama.id] = rawStates[drama.id] == null
          ? FollowState.initial(drama, history[drama.id])
          : FollowState.fromJson(lanMap(rawStates[drama.id]));
    }
    document.reconcile(
      previous: _favorites,
      favorites: favorites,
      states: states,
      history: history,
      oldHistory: _history,
      clearProgress: clearProgress,
    );
    values[_key('lanRecords')] = jsonEncode(document.toJson());
  }

  Map<String, dynamic>? lanReceipt(String operation) {
    final rows = readJsonList(_string(_key('lanReceipts')));
    return rows.where((row) => row['operation'] == operation).firstOrNull;
  }

  Future<void> applyLanRecords({
    required String operation,
    required String base,
    required Set<String> sources,
    required Iterable<LanRecord> records,
    required int epoch,
    bool Function()? isCurrent,
  }) {
    final incoming = records.toList();
    return _queue(() async {
      if (locked ||
          epoch != _epoch ||
          isCurrent?.call() == false ||
          sources.isEmpty ||
          !sources.every(allowsSource)) {
        throw StateError('当前用户或站源权限已变更，请重新连接');
      }
      if (lanReceipt(operation) != null) return;
      final document = lanDocument;
      if (document.hashFor(sources) != base) {
        throw StateError('记录刚刚发生变化，请重新同步或预览');
      }
      if (incoming.length > LanDocument.limit ||
          incoming.map((record) => record.id).toSet().length !=
              incoming.length) {
        throw const FormatException('同步记录数量无效');
      }
      final favorites = Map.of(_favorites);
      final states = Map.of(_followStates);
      final history = Map.of(_history);
      for (final record in incoming) {
        if (!sources.contains(record.drama.source)) {
          throw StateError('同步包含本次范围外的站源');
        }
        final checked = LanRecord.fromJson(record.toJson());
        document.records[checked.id] = checked;
        if (checked.followed) {
          favorites[checked.id] = checked.drama;
          states[checked.id] = checked.following.copyWith(
            seriesSeasons: states[checked.id]?.seriesSeasons ?? const {},
          );
          final watch = checked.watch;
          if (watch == null) {
            history.remove(checked.id);
          } else {
            history[checked.id] = watch;
          }
        } else {
          favorites.remove(checked.id);
          states.remove(checked.id);
        }
      }
      if (favorites.length > 20000 ||
          document.records.length > LanDocument.limit) {
        throw StateError('合并后的追剧记录超过本机上限，原记录已保留');
      }
      final sorted = history.values.toList()
        ..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
      final receipts = readJsonList(_string(_key('lanReceipts')))
        ..add({
          'operation': operation,
          'hash': document.hashFor(sources),
          'savedAt': DateTime.now().toUtc().toIso8601String(),
        });
      await _commit({
        _key('favorites'): jsonEncode(
          favorites.values.map((drama) => drama.toJson()).toList(),
        ),
        _key('followStates'): _encodeFollowStates(states),
        _key('history'): jsonEncode(
          sorted.take(300).map((entry) => entry.toJson()).toList(),
        ),
        _key('lanRecords'): jsonEncode(document.toJson()),
        _key('lanReceipts'): jsonEncode(
          receipts.reversed.take(64).toList().reversed.toList(),
        ),
      }, trackSync: false);
      _loadLibrary();
      _notify();
    });
  }

  Future<void> resolveLanConflict(
    String id,
    String field,
    LanValue choice,
    String base,
  ) {
    final epoch = _epoch;
    return _queue(() async {
      if (locked || epoch != _epoch) throw StateError('当前用户已变更');
      final document = lanDocument;
      final record = document.records[id];
      final cell = record?.fields[field];
      if (record == null ||
          cell == null ||
          record.hash != base ||
          !allowsSource(record.drama.source)) {
        throw StateError('记录已更新，请重新选择');
      }
      if (!cell.values.any(
        (candidate) => lanJSON(candidate.toJson()) == lanJSON(choice.toJson()),
      )) {
        throw StateError('此冲突选项已失效');
      }
      final next = record.withField(field, document.edit(cell, choice.value));
      document.records[id] = next;
      final favorites = Map.of(_favorites);
      final states = Map.of(_followStates);
      final history = Map.of(_history);
      if (next.followed) {
        favorites[id] = next.drama;
        states[id] = next.following.copyWith(
          seriesSeasons: states[id]?.seriesSeasons ?? const {},
        );
        if (next.watch case final watch?) {
          history[id] = watch;
        } else {
          history.remove(id);
        }
      } else {
        favorites.remove(id);
        states.remove(id);
      }
      final sorted = history.values.toList()
        ..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));
      await _commit({
        _key('favorites'): jsonEncode(
          favorites.values.map((drama) => drama.toJson()).toList(),
        ),
        _key('followStates'): _encodeFollowStates(states),
        _key('history'): jsonEncode(
          sorted.take(300).map((entry) => entry.toJson()).toList(),
        ),
        _key('lanRecords'): jsonEncode(document.toJson()),
      }, trackSync: false);
      _lanRevision++;
      _lanUrgentRevision++;
      _loadLibrary();
      _notify();
    });
  }
}
