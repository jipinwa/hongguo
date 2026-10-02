import 'dart:math';

import 'models.dart';

enum FollowStatus {
  planned('想看'),
  watching('在看'),
  watched('已看');

  const FollowStatus(this.label);
  final String label;
}

class FollowState {
  const FollowState({
    this.status = FollowStatus.planned,
    this.manuallyWatched = false,
    this.knownEpisodes = 0,
    this.readEpisodes,
    this.seriesSeasons = const {},
  });

  final FollowStatus status;
  final bool manuallyWatched;
  final int knownEpisodes;
  final int? readEpisodes;
  final Map<String, SeriesSeasonNotice> seriesSeasons;

  int get newEpisodes =>
      readEpisodes == null ? 0 : max(0, knownEpisodes - readEpisodes!);
  int get newSeasons =>
      seriesSeasons.values.where((notice) => !notice.read).length;
  bool get hasUpdates => newEpisodes > 0 || newSeasons > 0;
  String get updateLabel => [
    if (newEpisodes > 0) '$newEpisodes 集更新',
    if (newSeasons > 0) '新季 $newSeasons 部',
  ].join(' · ');
  String get label => manuallyWatched ? '已看 · 手动标记' : status.label;

  FollowState copyWith({
    FollowStatus? status,
    bool? manuallyWatched,
    int? knownEpisodes,
    int? readEpisodes,
    Map<String, SeriesSeasonNotice>? seriesSeasons,
  }) => FollowState(
    status: status ?? this.status,
    manuallyWatched: manuallyWatched ?? this.manuallyWatched,
    knownEpisodes: knownEpisodes ?? this.knownEpisodes,
    readEpisodes: readEpisodes ?? this.readEpisodes,
    seriesSeasons: seriesSeasons ?? this.seriesSeasons,
  );

  factory FollowState.initial(Drama drama, WatchEntry? watch) {
    final count = max(0, drama.episodes);
    final state = FollowState(
      knownEpisodes: count,
      readEpisodes: count > 0 ? count : null,
    );
    return watch == null ? state : state.afterPlayback(watch);
  }

  FollowState observe(Drama drama) {
    final count = max(knownEpisodes, drama.episodes);
    return FollowState(
      status:
          !manuallyWatched &&
              status == FollowStatus.watched &&
              count > knownEpisodes
          ? FollowStatus.watching
          : status,
      manuallyWatched: manuallyWatched,
      knownEpisodes: count,
      readEpisodes: readEpisodes ?? (count > 0 ? count : null),
      seriesSeasons: seriesSeasons,
    );
  }

  FollowState afterPlayback(WatchEntry entry) {
    final state = observe(entry.drama);
    if (state.manuallyWatched || entry.position <= 0) return state;
    return FollowState(
      status:
          state.knownEpisodes > 0 &&
              entry.finished &&
              entry.episode >= state.knownEpisodes
          ? FollowStatus.watched
          : FollowStatus.watching,
      knownEpisodes: state.knownEpisodes,
      readEpisodes: state.readEpisodes,
      seriesSeasons: state.seriesSeasons,
    );
  }

  FollowState withStatus(FollowStatus value) => FollowState(
    status: value,
    manuallyWatched: value == FollowStatus.watched,
    knownEpisodes: knownEpisodes,
    readEpisodes: value == FollowStatus.watched && knownEpisodes > 0
        ? knownEpisodes
        : readEpisodes,
    seriesSeasons: seriesSeasons,
  );

  FollowState markRead() => FollowState(
    status: status,
    manuallyWatched: manuallyWatched,
    knownEpisodes: knownEpisodes,
    readEpisodes: knownEpisodes > 0 ? knownEpisodes : null,
    seriesSeasons: {
      for (final entry in seriesSeasons.entries)
        entry.key: entry.value.markRead(),
    },
  );

  FollowState markSeriesSeasonRead(String id) =>
      seriesSeasons[id]?.read != false
      ? this
      : copyWith(
          seriesSeasons: Map.of(seriesSeasons)
            ..[id] = seriesSeasons[id]!.markRead(),
        );

  Map<String, dynamic> toJson() {
    final value = {
      'status': status.name,
      'manuallyWatched': manuallyWatched,
      'knownEpisodes': knownEpisodes,
      'readEpisodes': readEpisodes,
    };
    if (seriesSeasons.isNotEmpty) {
      value['seriesSeasons'] = {
        for (final entry in seriesSeasons.entries)
          entry.key: entry.value.toJson(),
      };
    }
    return value;
  }

  factory FollowState.fromJson(Map<String, dynamic> json) {
    final status = FollowStatus.values
        .where((value) => value.name == json['status'])
        .firstOrNull;
    final known = json['knownEpisodes'];
    final read = json['readEpisodes'];
    final manual = json['manuallyWatched'];
    final rawSeasons = json['seriesSeasons'];
    final seasons = <String, SeriesSeasonNotice>{};
    if (rawSeasons != null) {
      if (rawSeasons is! Map || rawSeasons.length > 200) {
        throw const FormatException('系列剧提醒无效');
      }
      for (final entry in rawSeasons.entries) {
        if (entry.key is! String) throw const FormatException('系列剧提醒无效');
        if (entry.value is! Map) throw const FormatException('系列剧提醒无效');
        final notice = SeriesSeasonNotice.fromJson(
          Map<String, dynamic>.from(entry.value as Map),
        );
        if (entry.key != notice.id || seasons.containsKey(notice.id)) {
          throw const FormatException('系列剧提醒无效');
        }
        seasons[notice.id] = notice;
      }
    }
    if (status == null ||
        known is! int ||
        known < 0 ||
        known > 1000000 ||
        read != null && (read is! int || read < 0 || read > known) ||
        manual is! bool ||
        manual && status != FollowStatus.watched) {
      throw const FormatException('追剧状态无效');
    }
    return FollowState(
      status: status,
      manuallyWatched: manual,
      knownEpisodes: known,
      readEpisodes: read as int?,
      seriesSeasons: seasons,
    );
  }
}

class SeriesSeasonNotice {
  const SeriesSeasonNotice({
    required this.id,
    required this.title,
    required this.season,
    required this.unit,
    this.read = false,
  });

  final String id;
  final String title;
  final int season;
  final String unit;
  final bool read;

  SeriesSeasonNotice markRead() => read
      ? this
      : SeriesSeasonNotice(
          id: id,
          title: title,
          season: season,
          unit: unit,
          read: true,
        );

  Map<String, dynamic> toJson() => {
    'id': id,
    'title': title,
    'season': season,
    'unit': unit,
    'read': read,
  };

  factory SeriesSeasonNotice.fromJson(Map<String, dynamic> json) {
    final id = json['id'], title = json['title'], season = json['season'];
    final unit = json['unit'], read = json['read'];
    if (id is! String ||
        !id.startsWith('${SourceSite.hongguo.id}:') ||
        id.length > 512 ||
        title is! String ||
        title.trim().isEmpty ||
        title.length > 300 ||
        season is! int ||
        season < 1 ||
        season > 200 ||
        unit is! String ||
        !{'季', '部'}.contains(unit) ||
        read is! bool) {
      throw const FormatException('系列剧提醒无效');
    }
    return SeriesSeasonNotice(
      id: id,
      title: title,
      season: season,
      unit: unit,
      read: read,
    );
  }
}

int resumeEpisodeIndex(List<Episode> episodes, WatchEntry? watch) {
  if (episodes.isEmpty || watch == null) return 0;
  final current = episodes.indexWhere((item) => item.number == watch.episode);
  if (current >= 0 && !watch.finished) return current;
  final next = episodes.indexWhere((item) => item.number > watch.episode);
  if (next >= 0) return next;
  return current >= 0 ? current : episodes.length - 1;
}
