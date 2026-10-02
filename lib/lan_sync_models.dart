import 'dart:convert';
import 'dart:math';

import 'package:crypto/crypto.dart';

import 'follow_state.dart';
import 'models.dart';

String lanID() {
  final random = Random.secure();
  return List.generate(
    16,
    (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'),
  ).join();
}

Object? _canonical(Object? value) {
  if (value is Map) {
    final keys = value.keys.cast<String>().toList()..sort();
    return {for (final key in keys) key: _canonical(value[key])};
  }
  if (value is List) return value.map(_canonical).toList();
  return value;
}

String lanJSON(Object? value) => jsonEncode(_canonical(value));
String lanHash(Object? value) =>
    sha256.convert(utf8.encode(lanJSON(value))).toString();

Map<String, dynamic> lanMap(Object? value) {
  if (value is! Map) throw const FormatException('设备返回的记录格式无效');
  return Map<String, dynamic>.from(value);
}

String lanText(Object? value, int limit, {bool empty = false}) {
  if (value is! String ||
      (!empty && value.trim().isEmpty) ||
      value.length > limit ||
      value.contains(RegExp(r'[\x00-\x08\x0b\x0c\x0e-\x1f]'))) {
    throw const FormatException('设备返回的文字字段无效');
  }
  return value;
}

class LanVersion {
  LanVersion(Map<String, int> values) : values = Map.unmodifiable(values);
  final Map<String, int> values;

  factory LanVersion.fromJson(Object? value) {
    final map = lanMap(value);
    if (map.isEmpty || map.length > 64) {
      throw const FormatException('同步版本数量无效');
    }
    final result = <String, int>{};
    for (final entry in map.entries) {
      if (!RegExp(r'^[a-f0-9]{32}$').hasMatch(entry.key) ||
          entry.value is! int ||
          (entry.value as int) <= 0 ||
          (entry.value as int) >= 9007199254740000) {
        throw const FormatException('同步版本无效');
      }
      result[entry.key] = entry.value as int;
    }
    return LanVersion(result);
  }

  bool includes(LanVersion other) => other.values.entries.every(
    (entry) => (values[entry.key] ?? 0) >= entry.value,
  );

  LanVersion join(LanVersion other) {
    final keys = {...values.keys, ...other.values.keys};
    if (keys.length > 64) throw StateError('同步设备数量已达上限');
    return LanVersion({
      for (final key in keys)
        key: max(values[key] ?? 0, other.values[key] ?? 0),
    });
  }

  Map<String, int> toJson() => values;
}

class LanValue {
  const LanValue(this.version, this.value);
  final LanVersion version;
  final Object? value;
  Map<String, dynamic> toJson() => {'v': version.toJson(), 'value': value};
  factory LanValue.fromJson(Object? value) {
    final row = lanMap(value);
    return LanValue(LanVersion.fromJson(row['v']), row['value']);
  }
}

class LanCell {
  LanCell(Iterable<LanValue> values)
    : values = List.unmodifiable(
        values.toList()
          ..sort((a, b) => lanJSON(a.toJson()).compareTo(lanJSON(b.toJson()))),
      );
  final List<LanValue> values;
  bool get conflict => values.length > 1;
  Object? get value => values.first.value;
  LanVersion get version =>
      values.map((entry) => entry.version).reduce((a, b) => a.join(b));

  factory LanCell.fromJson(Object? value) {
    if (value is! List || value.isEmpty || value.length > 32) {
      throw const FormatException('同步字段无效');
    }
    final candidates = value.map(LanValue.fromJson).toList();
    for (var i = 0; i < candidates.length; i++) {
      for (var j = i + 1; j < candidates.length; j++) {
        if (candidates[i].version.includes(candidates[j].version) ||
            candidates[j].version.includes(candidates[i].version) ||
            lanJSON(candidates[i].value) == lanJSON(candidates[j].value)) {
          throw const FormatException('同步冲突候选重复或包含过期版本');
        }
      }
    }
    candidates
        .map((candidate) => candidate.version)
        .reduce((a, b) => a.join(b));
    return LanCell(candidates);
  }

  LanCell merge(LanCell other) {
    final candidates = <LanValue>[];
    for (final incoming in [...values, ...other.values]) {
      var current = incoming;
      if (candidates.any(
        (candidate) => candidate.version.includes(current.version),
      )) {
        final equalVersion = candidates.where(
          (candidate) => current.version.includes(candidate.version),
        );
        if (equalVersion.any(
          (candidate) => lanJSON(candidate.value) != lanJSON(current.value),
        )) {
          throw const FormatException('相同同步版本的内容不一致');
        }
        continue;
      }
      candidates.removeWhere(
        (candidate) => current.version.includes(candidate.version),
      );
      final identical = candidates
          .where(
            (candidate) => lanJSON(candidate.value) == lanJSON(current.value),
          )
          .toList();
      for (final candidate in identical) {
        current = LanValue(
          current.version.join(candidate.version),
          current.value,
        );
        candidates.remove(candidate);
      }
      candidates.removeWhere(
        (candidate) => current.version.includes(candidate.version),
      );
      candidates.add(current);
    }
    if (candidates.length > 32) {
      throw StateError('这条记录的冲突过多，请先处理已有冲突');
    }
    return LanCell(candidates);
  }

  LanCell edit(String replica, int counter, Object? value) {
    final next = Map.of(version.values);
    next[replica] = max(next[replica] ?? 0, counter);
    if (next.length > 64) throw StateError('同步设备数量已达上限');
    return LanCell([LanValue(LanVersion(next), value)]);
  }

  List<Map<String, dynamic>> toJson() =>
      values.map((entry) => entry.toJson()).toList();
}

Drama lanDrama(Object? input) {
  final row = lanMap(input);
  final source = lanText(row['source'], 32);
  if (!SourceSite.isKnown(source)) {
    throw const FormatException('记录站源无效');
  }
  final id = lanText(row['id'], 512);
  final sourceId = lanText(row['sourceId'], 450, empty: true);
  if (!id.startsWith('$source:') ||
      id.length <= source.length + 1 ||
      sourceId.isNotEmpty && id != '$source:$sourceId') {
    throw const FormatException('剧目身份不一致');
  }
  final count = row['episodes'];
  if (count is! int || count < 0 || count > 1000000) {
    throw const FormatException('剧目集数无效');
  }
  for (final key in [
    'description',
    'cover',
    'category',
    'heat',
    'views',
    'onlineDate',
    'releaseStatus',
  ]) {
    lanText(row[key] ?? '', key == 'description' ? 4000 : 2048, empty: true);
  }
  lanText(row['title'], 300);
  final tags = row['tags'] ?? [];
  if (tags is! List || tags.length > 32) {
    throw const FormatException('剧目标签无效');
  }
  for (final tag in tags) {
    lanText(tag, 100);
  }
  final cover = row['cover'] as String? ?? '';
  if (cover.isNotEmpty) {
    final uri = Uri.tryParse(cover);
    if (uri == null ||
        !{'http', 'https'}.contains(uri.scheme) ||
        uri.userInfo.isNotEmpty) {
      throw const FormatException('剧目封面地址无效');
    }
  }
  return Drama.fromJson(row);
}

Drama lanCompactDrama(Drama drama) {
  String cut(String text, int length) =>
      text.length <= length ? text : text.substring(0, length);
  final row = drama.toJson();
  row['description'] = cut(drama.description, 4000);
  row['title'] = cut(drama.title, 300);
  row['tags'] = drama.tags.take(32).map((tag) => cut(tag, 100)).toList();
  if (drama.cover.length > 2048 ||
      !{'http', 'https'}.contains(Uri.tryParse(drama.cover)?.scheme)) {
    row['cover'] = '';
  }
  return lanDrama(row);
}

Map<String, dynamic>? lanProgress(WatchEntry? entry) => entry == null
    ? null
    : {
        'episode': entry.episode,
        'position': (entry.position * 1000).round() / 1000,
        'duration': (entry.duration * 1000).round() / 1000,
        'updatedAt': entry.updatedAt.toUtc().toIso8601String(),
      };

void _validateCell(String field, LanCell cell) {
  for (final candidate in cell.values) {
    final value = candidate.value;
    if (field == 'member') {
      if (value is! bool) throw const FormatException('追剧成员字段无效');
    } else if (field == 'status') {
      final status = lanMap(value);
      if (status.length != 2 ||
          !FollowStatus.values.any((value) => value.name == status['status']) ||
          status['manual'] is! bool ||
          status['manual'] == true && status['status'] != 'watched') {
        throw const FormatException('追剧状态无效');
      }
    } else if (value != null) {
      final progress = lanMap(value);
      final episode = progress['episode'];
      final position = progress['position'];
      final duration = progress['duration'];
      if (progress.length != 4 ||
          episode is! int ||
          episode < 1 ||
          episode > 1000000 ||
          position is! num ||
          !position.isFinite ||
          position < 0 ||
          position > 604800 ||
          duration is! num ||
          !duration.isFinite ||
          duration < 0 ||
          duration > 604800 ||
          progress['updatedAt'] is! String ||
          (progress['updatedAt'] as String).length > 48 ||
          DateTime.tryParse(progress['updatedAt'] as String) == null) {
        throw const FormatException('续播进度无效');
      }
    }
  }
}

class LanRecord {
  LanRecord({
    required this.drama,
    required this.member,
    required this.status,
    required this.progress,
    this.known = 0,
    this.read,
  });
  final Drama drama;
  final LanCell member;
  final LanCell status;
  final LanCell progress;
  final int known;
  final int? read;
  String get id => drama.id;
  bool get followed => member.values.every((value) => value.value == true);
  int get conflicts =>
      [member, status, progress].where((cell) => cell.conflict).length;
  late final String hash = lanHash(toJson());
  Map<String, LanCell> get fields => {
    'member': member,
    'status': status,
    'progress': progress,
  };

  FollowState get following {
    final value = lanMap(status.value);
    return FollowState(
      status: FollowStatus.values.firstWhere(
        (status) => status.name == value['status'],
      ),
      manuallyWatched: value['manual'] == true,
      knownEpisodes: known,
      readEpisodes: read,
    );
  }

  WatchEntry? get watch => progress.value == null
      ? null
      : WatchEntry.fromJson({
          ...lanMap(progress.value),
          'drama': drama.toJson(),
        });

  factory LanRecord.fromJson(Object? value) {
    if (utf8.encode(jsonEncode(value)).length > 512 * 1024) {
      throw const FormatException('单条同步记录超过保存上限');
    }
    final row = lanMap(value);
    final drama = lanDrama(row['drama']);
    final member = LanCell.fromJson(row['member']);
    final status = LanCell.fromJson(row['status']);
    final progress = LanCell.fromJson(row['progress']);
    _validateCell('member', member);
    _validateCell('status', status);
    _validateCell('progress', progress);
    final known = row['known'];
    final read = row['read'];
    if (known is! int ||
        known < 0 ||
        known > 1000000 ||
        read != null && (read is! int || read < 0 || read > known)) {
      throw const FormatException('新集已读基准无效');
    }
    return LanRecord(
      drama: drama,
      member: member,
      status: status,
      progress: progress,
      known: known,
      read: read as int?,
    );
  }

  LanRecord withField(String field, LanCell cell) => LanRecord(
    drama: drama,
    member: field == 'member' ? cell : member,
    status: field == 'status' ? cell : status,
    progress: field == 'progress' ? cell : progress,
    known: known,
    read: read,
  );

  LanRecord merge(LanRecord other) {
    if (id != other.id) throw const FormatException('不能合并不同剧目');
    final a = lanJSON(drama.toJson()), b = lanJSON(other.drama.toJson());
    final metadata = a.compareTo(b) <= 0
        ? drama.merge(other.drama)
        : other.drama.merge(drama);
    return LanRecord(
      drama: lanCompactDrama(metadata),
      member: member.merge(other.member),
      status: status.merge(other.status),
      progress: progress.merge(other.progress),
      known: max(known, other.known),
      read: read == null && other.read == null
          ? null
          : max(read ?? 0, other.read ?? 0),
    );
  }

  Map<String, dynamic> toJson() => {
    'drama': drama.toJson(),
    'member': member.toJson(),
    'status': status.toJson(),
    'progress': progress.toJson(),
    'known': known,
    'read': read,
  };
}

class LanDocument {
  LanDocument({
    required this.replica,
    this.counter = 0,
    Map<String, LanRecord> records = const {},
  }) : records = Map.of(records);
  final String replica;
  int counter;
  final Map<String, LanRecord> records;
  static const limit = 25000;
  int get conflicts =>
      records.values.fold(0, (count, record) => count + record.conflicts);

  factory LanDocument.empty() => LanDocument(replica: lanID());
  factory LanDocument.fromJson(Object? value) {
    final row = lanMap(value);
    final replica = lanText(row['replica'], 32);
    final counter = row['counter'];
    final rows = row['records'];
    if (row['schema'] != 1 ||
        !RegExp(r'^[a-f0-9]{32}$').hasMatch(replica) ||
        counter is! int ||
        counter < 0 ||
        counter >= 9007199254740000 ||
        rows is! List ||
        rows.length > limit) {
      throw const FormatException('局域网同步记录损坏，原记录已保留');
    }
    final document = LanDocument(replica: replica, counter: counter);
    for (final raw in rows) {
      final record = LanRecord.fromJson(raw);
      if (document.records.containsKey(record.id)) {
        throw const FormatException('同步记录重复');
      }
      document.records[record.id] = record;
      for (final cell in record.fields.values) {
        document.counter = max(
          document.counter,
          cell.version.values[replica] ?? 0,
        );
      }
    }
    return document;
  }

  LanDocument copy() =>
      LanDocument(replica: replica, counter: counter, records: records);
  LanCell cell(Object? value) => LanCell([
    LanValue(LanVersion({replica: ++counter}), value),
  ]);
  LanCell edit(LanCell current, Object? value) {
    counter = max(counter, current.version.values[replica] ?? 0) + 1;
    return current.edit(replica, counter, value);
  }

  void reconcile({
    required Map<String, Drama> previous,
    required Map<String, Drama> favorites,
    required Map<String, FollowState> states,
    required Map<String, WatchEntry> history,
    required Map<String, WatchEntry> oldHistory,
    Set<String> clearProgress = const {},
  }) {
    for (final id in {...records.keys, ...favorites.keys}) {
      final old = records[id];
      final drama = favorites[id];
      if (old == null && drama != null) {
        final state = states[id] ?? FollowState.initial(drama, history[id]);
        records[id] = LanRecord(
          drama: lanCompactDrama(drama),
          member: cell(true),
          status: cell({
            'status': state.status.name,
            'manual': state.manuallyWatched,
          }),
          progress: cell(lanProgress(history[id])),
          known: state.knownEpisodes,
          read: state.readEpisodes,
        );
        continue;
      }
      if (old == null) continue;
      if (drama == null) {
        if (previous.containsKey(id)) {
          records[id] = old
              .withField('member', edit(old.member, false))
              .withField('progress', edit(old.progress, null));
        } else if (clearProgress.contains(id)) {
          records[id] = old.withField('progress', edit(old.progress, null));
        }
        continue;
      }
      final state = states[id] ?? old.following;
      var next = LanRecord(
        drama: lanCompactDrama(drama),
        member: !previous.containsKey(id) ? edit(old.member, true) : old.member,
        status: old.status,
        progress: old.progress,
        known: max(old.known, state.knownEpisodes),
        read: state.readEpisodes == null && old.read == null
            ? null
            : max(old.read ?? 0, state.readEpisodes ?? 0),
      );
      final status = {
        'status': state.status.name,
        'manual': state.manuallyWatched,
      };
      if (lanJSON(status) != lanJSON(old.status.value)) {
        next = next.withField('status', edit(old.status, status));
      }
      if (clearProgress.contains(id)) {
        next = next.withField('progress', edit(old.progress, null));
      } else if (history[id] != null &&
          (!previous.containsKey(id) ||
              lanJSON(lanProgress(history[id])) !=
                  lanJSON(lanProgress(oldHistory[id])))) {
        next = next.withField(
          'progress',
          edit(old.progress, lanProgress(history[id])),
        );
      }
      records[id] = next;
    }
    if (records.length > limit) throw StateError('同步记录已达保存上限，请先整理设备记录');
  }

  String hashFor(Set<String> sources) {
    final entries =
        records.values
            .where((record) => sources.contains(record.drama.source))
            .toList()
          ..sort((a, b) => a.id.compareTo(b.id));
    return lanHash([
      for (final entry in entries) [entry.id, entry.hash],
    ]);
  }

  Map<String, dynamic> toJson() => {
    'schema': 1,
    'replica': replica,
    'counter': counter,
    'records': records.values.map((record) => record.toJson()).toList(),
  };
}

enum LanSyncMode {
  merge('双向合并'),
  push('覆盖对方'),
  pull('覆盖本机');

  const LanSyncMode(this.label);
  final String label;
}

class LanChangeCount {
  const LanChangeCount({
    this.added = 0,
    this.updated = 0,
    this.removed = 0,
    this.conflicts = 0,
  });
  final int added, updated, removed, conflicts;
  factory LanChangeCount.between(
    Map<String, LanRecord> old,
    Map<String, LanRecord> next,
  ) {
    var added = 0, updated = 0, removed = 0, conflicts = 0;
    for (final record in next.values) {
      final previous = old[record.id];
      if (record.followed && previous?.followed != true) {
        added++;
      } else if (!record.followed && previous?.followed == true) {
        removed++;
      } else if (record.followed && previous?.hash != record.hash) {
        updated++;
      }
      conflicts += record.conflicts;
    }
    return LanChangeCount(
      added: added,
      updated: updated,
      removed: removed,
      conflicts: conflicts,
    );
  }
  String get label => '新增 $added · 更新 $updated · 移除 $removed · 冲突 $conflicts';
}
