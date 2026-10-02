part of 'lan_controller.dart';

Iterable<List<LanRecord>> _lanRecordBatches(Iterable<LanRecord> records) sync* {
  var batch = <LanRecord>[];
  var bytes = 0;
  for (final record in records) {
    final size = utf8.encode(jsonEncode(record.toJson())).length + 1;
    if (size > 512 * 1024) throw StateError('单条同步记录过大，请先处理记录冲突');
    if (batch.isNotEmpty && (batch.length == 16 || bytes + size > 640 * 1024)) {
      yield batch;
      batch = [];
      bytes = 0;
    }
    batch.add(record);
    bytes += size;
  }
  if (batch.isNotEmpty) yield batch;
}

class _LanIncomingSync {
  _LanIncomingSync({
    required this.operation,
    required this.base,
    required this.finalHash,
    required this.sources,
    required this.remote,
    required this.count,
    required this.automatic,
  });
  final String operation, base, finalHash;
  final Set<String> sources;
  final LanConnection remote;
  final int count;
  final bool automatic;
  final Map<String, LanRecord> records = {};
  DateTime updated = DateTime.now();
  int bytes = 0;
  bool committing = false;
}

extension LanSynchronization on LanController {
  Set<String> _scope(LanConnection remote) => remote.sources.intersection(
    store.sources.map((source) => source.id).toSet(),
  );

  void _checkSync(int ticket, LanConnection remote) {
    if (_disposed ||
        ticket != _syncTicket ||
        connection != remote ||
        !_foreground ||
        store.locked ||
        store.profileEpoch != _sessionEpoch) {
      throw StateError('同步已取消，已经保存的记录会保留');
    }
  }

  Future<void> beginManual() async {
    _manual = true;
    await cancelSync();
    _manual = true;
    _notify();
  }

  void endManual() {
    _manual = false;
    _notify();
    flush();
  }

  Future<void> cancelSync() async {
    _syncTicket++;
    if (_syncing) syncMessage = '正在取消，已经保存的记录会保留';
    _notify();
    final requests = _syncRequests.toList();
    for (final request in requests) {
      if (!receiving) break;
      await _native('cancel', {
        'requestId': request,
      }).catchError((Object _) => <String, dynamic>{});
    }
    if (_incomingSync != null) {
      final committing = _incomingSync!.committing;
      _incomingSync = null;
      syncMessage = committing
          ? '已取消后续同步，正在保存的记录可能已写入；再次合并可确认'
          : '已取消接收，未提交的记录已丢弃';
    }
    await _syncWork;
    _notify();
  }

  Future<T> _syncOperation<T>(Future<T> Function(int ticket) operation) async {
    if (_syncing || _incomingSync != null) throw StateError('正在同步，请稍候');
    _syncing = true;
    final ticket = ++_syncTicket;
    final run = _run;
    final epoch = store.profileEpoch;
    final completed = Completer<void>();
    _syncWork = completed.future;
    _notify();
    try {
      return await operation(ticket);
    } catch (failure) {
      if (run == _run && epoch == store.profileEpoch)
        syncMessage = failure.toString();
      rethrow;
    } finally {
      _syncing = false;
      _syncWork = null;
      completed.complete();
      _notify();
    }
  }

  Future<LanPreview> preview(LanSyncMode mode) =>
      _syncOperation((ticket) => _planSync(mode, ticket));

  Future<void> synchronize({bool automatic = false}) async {
    if (automatic &&
        (_manual || !autoSync || connection?.autoSync != true || syncing))
      return;
    await _syncOperation((ticket) async {
      final preview = await _planSync(LanSyncMode.merge, ticket);
      await _commitSync(preview, ticket, automatic: automatic);
    });
  }

  Future<void> applyPreview(LanPreview preview) => _syncOperation(
    (ticket) => _commitSync(preview, ticket, automatic: false),
  );

  Future<LanPreview> _planSync(LanSyncMode mode, int ticket) async {
    final remote = connection;
    if (remote == null) throw StateError('请先连接设备');
    final sources = _scope(remote);
    if (sources.isEmpty) throw StateError('两端当前用户没有共同可用的站源');
    _checkSync(ticket, remote);
    final localDocument = store.lanDocument;
    final localBase = localDocument.hashFor(sources);
    final localRecords = {
      for (final entry in localDocument.records.entries)
        if (sources.contains(entry.value.drama.source)) entry.key: entry.value,
    };
    final hashes = <String, String>{};
    var offset = 0;
    var remoteBase = '';
    var remoteSkipped = 0;
    syncMessage = '正在比较双方记录';
    _notify();
    while (true) {
      _checkSync(ticket, remote);
      final summary = await _request('sync/summary', {
        'sources': sources.toList(),
        'offset': offset,
        if (remoteBase.isNotEmpty) 'base': remoteBase,
      }, owner: 'sync');
      _checkSync(ticket, remote);
      final base = lanText(summary['base'], 64);
      if (remoteBase.isNotEmpty && remoteBase != base) {
        throw StateError('对方记录已更新，请重新同步');
      }
      remoteBase = base;
      final items = lanMap(summary['items']);
      if (items.length > 128 ||
          hashes.length + items.length > LanDocument.limit) {
        throw const FormatException('对方记录超过同步上限');
      }
      for (final entry in items.entries) {
        if (hashes.containsKey(entry.key))
          throw const FormatException('对方版本摘要重复');
        hashes[lanText(entry.key, 512)] = lanText(entry.value, 64);
      }
      remoteSkipped = intValue(summary['skipped']).clamp(0, LanDocument.limit);
      final next = summary['next'];
      if (next == null) break;
      if (next is! int || next != offset + items.length || next <= offset) {
        throw const FormatException('对方分页摘要无效');
      }
      offset = next;
    }
    final remoteRecords = <String, LanRecord>{};
    final fetch = <String>[];
    for (final entry in hashes.entries) {
      final local = localRecords[entry.key];
      if (local?.hash == entry.value) {
        remoteRecords[entry.key] = local!;
      } else {
        fetch.add(entry.key);
      }
    }
    for (var start = 0; start < fetch.length;) {
      _checkSync(ticket, remote);
      final keys = fetch.sublist(start, min(start + 16, fetch.length));
      final response = await _request('sync/records', {
        'base': remoteBase,
        'sources': sources.toList(),
        'ids': keys,
      }, owner: 'sync');
      _checkSync(ticket, remote);
      final rows = response['records'];
      if (rows is! List || rows.isEmpty || rows.length > keys.length)
        throw const FormatException('对方差异记录不完整');
      final returnedKeys = keys.take(rows.length).toSet();
      for (final row in rows) {
        final record = LanRecord.fromJson(row);
        if (!returnedKeys.contains(record.id) ||
            record.hash != hashes[record.id] ||
            remoteRecords.containsKey(record.id) ||
            !sources.contains(record.drama.source)) {
          throw const FormatException('对方记录与版本摘要不一致');
        }
        remoteRecords[record.id] = record;
      }
      start += rows.length;
    }
    final result = <String, LanRecord>{};
    final writer = localDocument.copy();
    for (final id in {...localRecords.keys, ...remoteRecords.keys}) {
      final a = localRecords[id], b = remoteRecords[id];
      var record = a == null
          ? b!
          : b == null
          ? a
          : a.merge(b);
      if (mode != LanSyncMode.merge) {
        final authority = mode == LanSyncMode.push ? a : b;
        final followed = authority?.followed == true;
        record = LanRecord(
          drama: authority?.drama ?? record.drama,
          member: writer.edit(record.member, followed),
          status: writer.edit(
            record.status,
            authority?.status.value ?? record.status.value,
          ),
          progress: writer.edit(
            record.progress,
            followed ? authority?.progress.value : null,
          ),
          known: max(record.known, authority?.known ?? 0),
          read: record.read == null && authority?.read == null
              ? null
              : max(record.read ?? 0, authority?.read ?? 0),
        );
      }
      result[id] = record;
    }
    if (result.length > LanDocument.limit ||
        result.values.where((record) => record.followed).length > 20000) {
      throw StateError('合并后的追剧记录超过保存上限，请先整理追剧');
    }
    _checkSync(ticket, remote);
    if (store.lanDocument.hashFor(sources) != localBase) {
      throw StateError('本机记录刚刚更新，请重新同步或预览');
    }
    return LanPreview(
      operation: lanID(),
      mode: mode,
      connection: remote,
      epoch: store.profileEpoch,
      sources: sources,
      localBase: localBase,
      remoteBase: remoteBase,
      result: result,
      localChanges: result.values
          .where((record) => localRecords[record.id]?.hash != record.hash)
          .toList(),
      remoteChanges: result.values
          .where((record) => hashes[record.id] != record.hash)
          .toList(),
      localCount: LanChangeCount.between(localRecords, result),
      remoteCount: LanChangeCount.between(remoteRecords, result),
      skipped:
          localDocument.records.length - localRecords.length + remoteSkipped,
    );
  }

  Future<void> _commitSync(
    LanPreview preview,
    int ticket, {
    required bool automatic,
  }) async {
    final remote = preview.connection;
    _checkSync(ticket, remote);
    if (preview.epoch != store.profileEpoch ||
        DateTime.now().difference(preview.created) >
            const Duration(minutes: 2) ||
        store.lanDocument.hashFor(preview.sources) != preview.localBase ||
        !_scope(remote).containsAll(preview.sources)) {
      throw StateError('预览已过期或记录已变化，请重新预览');
    }
    var remoteSaved = false;
    var remoteCommitRequested = false;
    var localSaved = false;
    var began = false;
    final operation = preview.operation;
    final finalHash = LanDocument(
      replica: store.lanDocument.replica,
      records: preview.result,
    ).hashFor(preview.sources);
    try {
      if (preview.remoteChanges.isNotEmpty) {
        syncMessage = '正在向对方发送差异';
        _notify();
        await _request('sync/begin', {
          'operation': operation,
          'base': preview.remoteBase,
          'finalHash': finalHash,
          'sources': preview.sources.toList(),
          'count': preview.remoteChanges.length,
          'automatic': automatic,
        }, owner: 'sync');
        began = true;
        var start = 0;
        for (final batch in _lanRecordBatches(preview.remoteChanges)) {
          _checkSync(ticket, remote);
          final end = start + batch.length;
          await _request('sync/chunk', {
            'operation': operation,
            'offset': start,
            'records': batch.map((record) => record.toJson()).toList(),
          }, owner: 'sync');
          syncMessage =
              '正在同步 ' +
              end.toString() +
              ' / ' +
              preview.remoteChanges.length.toString();
          _notify();
          start = end;
        }
        _checkSync(ticket, remote);
        if (store.lanDocument.hashFor(preview.sources) != preview.localBase) {
          throw StateError('本机记录已变化，本批未提交，请重新同步');
        }
        try {
          remoteCommitRequested = true;
          final saved = await _request('sync/commit', {
            'operation': operation,
          }, owner: 'sync');
          if (lanMap(saved['receipt'])['hash'] != finalHash)
            throw StateError('未能确认对方保存的版本');
          remoteSaved = true;
        } catch (_) {
          final saved = await _request('sync/receipt', {
            'operation': operation,
          }, owner: 'sync');
          if (saved['receipt'] == null ||
              lanMap(saved['receipt'])['hash'] != finalHash)
            rethrow;
          remoteSaved = true;
        }
      }
      _checkSync(ticket, remote);
      syncMessage = remoteSaved ? '对方已保存，正在保存本机' : '正在保存本机记录';
      _notify();
      if (preview.localChanges.isNotEmpty) {
        await store.applyLanRecords(
          operation: operation,
          base: preview.localBase,
          sources: preview.sources,
          records: preview.localChanges,
          epoch: preview.epoch,
          isCurrent: () =>
              ticket == _syncTicket &&
              connection == remote &&
              (!automatic || autoSync && remote.autoSync),
        );
      }
      localSaved = true;
      _checkSync(ticket, remote);
      lastSync = DateTime.now();
      lastLocalCount = preview.localCount;
      lastRemoteCount = preview.remoteCount;
      skipped = preview.skipped;
      final conflicts = preview.result.values.fold<int>(
        0,
        (count, record) => count + record.conflicts,
      );
      syncMessage = conflicts == 0 ? '双方记录已同步' : '记录已同步 · $conflicts 项冲突待处理';
      error = null;
      unawaited(
        _request('status', {
          'synced': operation,
          'hash': finalHash,
        }).catchError((Object _) => <String, dynamic>{}),
      );
    } catch (failure) {
      if (began && connection == remote) {
        await _request('sync/cancel', {
          'operation': operation,
        }).catchError((Object _) => <String, dynamic>{});
      }
      if (remoteSaved) {
        throw StateError(
          (localSaved ? '双方记录已保存，连接状态已变化。' : '对方已保存，本机尚未保存；再次合并可补齐。') +
              failure.toString(),
        );
      }
      if (remoteCommitRequested)
        throw StateError('尚未确认对方是否保存，重连后再次合并可补齐；本机原记录保留。' + failure.toString());
      rethrow;
    }
    _notify();
  }

  Set<String> _receiveScope(Map<String, dynamic> body, LanConnection remote) {
    final sources = lanSources(body['sources']);
    if (sources.isEmpty || !_scope(remote).containsAll(sources)) {
      throw StateError('同步范围超出双方当前站源权限');
    }
    return sources;
  }

  Future<Map<String, dynamic>> _receiveSync(
    String path,
    Map<String, dynamic> body,
    LanConnection remote,
  ) async {
    final document = store.lanDocument;
    if (path == 'sync/summary') {
      final sources = _receiveScope(body, remote);
      final base = document.hashFor(sources);
      if (body['base'] != null && body['base'] != base)
        throw StateError('对方记录刚刚变化，请重新同步');
      final offset = body['offset'];
      final records =
          document.records.values
              .where((record) => sources.contains(record.drama.source))
              .toList()
            ..sort((a, b) => a.id.compareTo(b.id));
      if (offset is! int || offset < 0 || offset > records.length)
        throw const FormatException('同步分页无效');
      final end = min(offset + 128, records.length);
      return {
        'base': base,
        'items': {
          for (final record in records.sublist(offset, end))
            record.id: record.hash,
        },
        'next': end < records.length ? end : null,
        'skipped': document.records.length - records.length,
      };
    }
    if (path == 'sync/records') {
      final sources = _receiveScope(body, remote);
      if (document.hashFor(sources) != body['base'])
        throw StateError('对方记录刚刚变化，请重新同步');
      final ids = body['ids'];
      if (ids is! List ||
          ids.isEmpty ||
          ids.length > 16 ||
          ids.toSet().length != ids.length) {
        throw const FormatException('同步分批范围无效');
      }
      final records = <Map<String, dynamic>>[];
      var bytes = 0;
      for (final id in ids) {
        final record = document.records[id];
        if (record == null || !sources.contains(record.drama.source))
          throw StateError('分集记录已变化，请重新同步');
        final row = record.toJson();
        final size = utf8.encode(jsonEncode(row)).length + 1;
        if (size > 512 * 1024) throw StateError('单条同步记录过大，请先处理记录冲突');
        if (records.isNotEmpty && bytes + size > 640 * 1024) break;
        records.add(row);
        bytes += size;
      }
      return {'records': records};
    }
    final operation = lanText(body['operation'], 32);
    if (!RegExp(r'^[a-f0-9]{32}$').hasMatch(operation))
      throw const FormatException('同步操作标识无效');
    final receipt = store.lanReceipt(operation);
    if (path == 'sync/receipt') return {'receipt': receipt};
    if (path == 'sync/cancel') {
      if (_incomingSync?.operation == operation) {
        _incomingSync = null;
        syncMessage = '同步已取消，已保存的记录保留';
        _notify();
      }
      return {'receipt': receipt};
    }
    if (receipt != null) return {'receipt': receipt};
    if (path == 'sync/begin') {
      if (_syncing || _manual) throw StateError('对方正在操作同步，请稍后重试');
      if (_incomingSync != null && _incomingSync!.operation != operation)
        throw StateError('对方正在接收其他同步批次');
      final sources = _receiveScope(body, remote);
      final count = body['count'];
      final base = lanText(body['base'], 64);
      final finalHash = lanText(body['finalHash'], 64);
      if (count is! int ||
          count < 1 ||
          count > LanDocument.limit ||
          document.hashFor(sources) != base) {
        throw StateError('对方记录已变化或批次数量无效，请重新同步');
      }
      if (body['automatic'] == true && !autoSync) throw StateError('对方已暂停自动同步');
      _incomingSync ??= _LanIncomingSync(
        operation: operation,
        base: base,
        finalHash: finalHash,
        sources: sources,
        remote: remote,
        count: count,
        automatic: body['automatic'] == true,
      );
      syncMessage = '正在接收差异记录';
      _notify();
      return {'ready': true};
    }
    final pending = _incomingSync;
    if (pending == null ||
        pending.operation != operation ||
        pending.remote != remote) {
      throw StateError('接收批次已失效，请重新同步');
    }
    pending.updated = DateTime.now();
    if (pending.automatic && !autoSync) {
      _incomingSync = null;
      throw StateError('自动同步已暂停');
    }
    if (path == 'sync/chunk') {
      final rows = body['records'];
      final offset = body['offset'];
      if (rows is! List ||
          rows.isEmpty ||
          rows.length > 16 ||
          offset is! int ||
          offset < 0 ||
          offset > pending.records.length) {
        throw const FormatException('同步分批顺序无效');
      }
      final records = rows.map(LanRecord.fromJson).toList();
      if (offset < pending.records.length) {
        if (records.any(
          (record) => pending.records[record.id]?.hash != record.hash,
        )) {
          throw const FormatException('重复同步的内容不一致');
        }
        return {'received': pending.records.length};
      }
      for (final record in records) {
        if (!pending.sources.contains(record.drama.source) ||
            pending.records.containsKey(record.id)) {
          throw const FormatException('同步记录重复或超出范围');
        }
        pending.records[record.id] = record;
      }
      pending.bytes += utf8.encode(jsonEncode(rows)).length;
      if (pending.bytes > 12 * 1024 * 1024 ||
          pending.records.length > pending.count) {
        _incomingSync = null;
        throw StateError('接收记录超过保存上限，原记录已保留');
      }
      syncMessage =
          '正在接收 ' +
          pending.records.length.toString() +
          ' / ' +
          pending.count.toString();
      _notify();
      return {'received': pending.records.length};
    }
    if (path == 'sync/commit') {
      if (pending.records.length != pending.count ||
          document.hashFor(pending.sources) != pending.base ||
          !_scope(remote).containsAll(pending.sources)) {
        _incomingSync = null;
        throw StateError('本机记录已变化或批次不完整，原记录已保留');
      }
      final next = document.copy();
      for (final record in pending.records.values) {
        final old = document.records[record.id];
        if (old != null) {
          for (final field in old.fields.entries) {
            final incoming = record.fields[field.key]!;
            if (lanJSON(field.value.merge(incoming).toJson()) !=
                lanJSON(incoming.toJson())) {
              throw StateError('同步版本未包含本机的最新修改，请重新合并');
            }
          }
        }
        next.records[record.id] = record;
      }
      if (next.hashFor(pending.sources) != pending.finalHash)
        throw StateError('同步结果与预览版本不一致');
      final count = LanChangeCount.between(document.records, next.records);
      pending.committing = true;
      await store.applyLanRecords(
        operation: operation,
        base: pending.base,
        sources: pending.sources,
        records: pending.records.values,
        epoch: _sessionEpoch,
        isCurrent: () =>
            identical(_incomingSync, pending) &&
            connection == remote &&
            (!pending.automatic || autoSync),
      );
      if (connection != remote || store.profileEpoch != _sessionEpoch)
        throw StateError('记录已保存，连接状态已变更');
      if (identical(_incomingSync, pending)) _incomingSync = null;
      lastSync = DateTime.now();
      lastLocalCount = count;
      syncMessage = '本机已保存，等待双方同步完成';
      _notify();
      return {'receipt': store.lanReceipt(operation)};
    }
    throw StateError('不支持此同步操作');
  }
}
