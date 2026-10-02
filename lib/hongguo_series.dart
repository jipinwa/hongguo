import 'catalog_sort.dart';
import 'follow_state.dart';
import 'models.dart';

const _hongguoSeriesLimit = 200;

final _seasonSuffix = RegExp(
  r'第\s*([0-9零〇一二两兩三四五六七八九十百]+)\s*([季部])[\s\u3000，。！？、：；·「」『』（）【】《》“”‘’.,!?:;\-_/()\[\]{}]*$',
);

class HongguoSeriesInfo {
  const HongguoSeriesInfo({
    required this.baseTitle,
    required this.key,
    required this.season,
    required this.unit,
    required this.explicit,
  });

  final String baseTitle;
  final String key;
  final int season;
  final String unit;
  final bool explicit;
}

class HongguoSeriesEntry {
  const HongguoSeriesEntry({
    required this.drama,
    required this.season,
    required this.unit,
  });

  final Drama drama;
  final int season;
  final String unit;

  String get label => '第 $season $unit';
}

int? _seasonNumber(String label) {
  final numeric = int.tryParse(label);
  if (numeric != null) {
    return numeric >= 1 && numeric <= _hongguoSeriesLimit ? numeric : null;
  }
  const digits = {
    '零': 0,
    '〇': 0,
    '一': 1,
    '二': 2,
    '两': 2,
    '兩': 2,
    '三': 3,
    '四': 4,
    '五': 5,
    '六': 6,
    '七': 7,
    '八': 8,
    '九': 9,
  };
  var number = 0;
  if (!label.contains(RegExp('[十百]'))) {
    for (final rune in label.runes) {
      final digit = digits[String.fromCharCode(rune)];
      if (digit == null) return null;
      number = number * 10 + digit;
      if (number > _hongguoSeriesLimit) return null;
    }
    return number >= 1 ? number : null;
  }
  var digit = 0;
  var previous = 1000;
  for (final rune in label.runes) {
    final character = String.fromCharCode(rune);
    final value = digits[character];
    if (value != null) {
      digit = digit * 10 + value;
      continue;
    }
    final multiplier = character == '百' ? 100 : 10;
    if (multiplier >= previous || digit > 9) return null;
    number += (digit == 0 ? 1 : digit) * multiplier;
    digit = 0;
    previous = multiplier;
  }
  number += digit;
  return number >= 1 && number <= _hongguoSeriesLimit ? number : null;
}

HongguoSeriesInfo? hongguoSeriesInfo(Drama drama, {String? unit}) {
  if (drama.source != SourceSite.hongguo.id) return null;
  final title = drama.title.trim();
  final match = _seasonSuffix.firstMatch(title);
  if (match == null) {
    if (unit == null || title.isEmpty) return null;
    return HongguoSeriesInfo(
      baseTitle: title,
      key: normalizedSearchText(title),
      season: 1,
      unit: unit,
      explicit: false,
    );
  }
  final base = title.substring(0, match.start).trim();
  final season = _seasonNumber(match[1]!);
  if (base.isEmpty || season == null) return null;
  return HongguoSeriesInfo(
    baseTitle: base,
    key: normalizedSearchText(base),
    season: season,
    unit: match[2]!,
    explicit: true,
  );
}

List<HongguoSeriesEntry> hongguoSeriesEntries(
  Drama anchor,
  Iterable<Drama> candidates,
) {
  if (anchor.source != SourceSite.hongguo.id) return const [];
  var anchorInfo = hongguoSeriesInfo(anchor);
  String key, unit;
  int anchorSeason;
  if (anchorInfo == null) {
    final baseKey = normalizedSearchText(anchor.title);
    final groups = <String, List<HongguoSeriesInfo>>{};
    for (final drama in candidates) {
      final info = hongguoSeriesInfo(drama);
      if (info != null && info.key == baseKey) {
        (groups[info.unit] ??= []).add(info);
      }
    }
    if (groups.isEmpty) return const [];
    final best = groups.entries.toList()
      ..sort((a, b) {
        final count = b.value.length.compareTo(a.value.length);
        if (count != 0) return count;
        final maxA = a.value.fold<int>(
          0,
          (value, info) => value > info.season ? value : info.season,
        );
        final maxB = b.value.fold<int>(
          0,
          (value, info) => value > info.season ? value : info.season,
        );
        return maxB.compareTo(maxA);
      });
    key = baseKey;
    unit = best.first.key;
    anchorSeason = 1;
    anchorInfo = hongguoSeriesInfo(anchor, unit: unit);
  } else {
    key = anchorInfo.key;
    unit = anchorInfo.unit;
    anchorSeason = anchorInfo.season;
  }
  if (anchorInfo == null) return const [];
  final byId = <String, HongguoSeriesEntry>{};
  for (final drama in [anchor, ...candidates]) {
    if (drama.source != SourceSite.hongguo.id) continue;
    final info = hongguoSeriesInfo(drama, unit: unit);
    if (info == null || info.key != key || info.unit != unit) continue;
    byId[drama.id] = HongguoSeriesEntry(
      drama: drama,
      season: drama.id == anchor.id ? anchorSeason : info.season,
      unit: unit,
    );
  }
  final entries = byId.values.toList()
    ..sort((a, b) {
      final season = a.season.compareTo(b.season);
      return season != 0
          ? season
          : naturalTitleCompare(a.drama.title, b.drama.title);
    });
  return entries.length > 1 ? entries : const [];
}

FollowState observeHongguoSeriesSeasons({
  required Drama anchor,
  required FollowState state,
  required Iterable<Drama> candidates,
  required Set<String> favoriteIds,
}) {
  final entries = hongguoSeriesEntries(anchor, candidates);
  if (entries.isEmpty) return state;
  final anchorSeason = entries
      .where((entry) => entry.drama.id == anchor.id)
      .firstOrNull
      ?.season;
  if (anchorSeason == null) return state;
  var seasons = state.seriesSeasons;
  for (final id in seasons.keys.toList()) {
    if (id == anchor.id || favoriteIds.contains(id)) {
      seasons = Map.of(seasons)..remove(id);
    }
  }
  for (final entry in entries) {
    if (entry.drama.id == anchor.id ||
        favoriteIds.contains(entry.drama.id) ||
        entry.season <= anchorSeason) {
      continue;
    }
    final previous = seasons[entry.drama.id];
    final notice = SeriesSeasonNotice(
      id: entry.drama.id,
      title: entry.drama.title,
      season: entry.season,
      unit: entry.unit,
      read: previous?.read ?? false,
    );
    if (previous?.title == notice.title &&
        previous?.season == notice.season &&
        previous?.unit == notice.unit &&
        previous?.read == notice.read) {
      continue;
    }
    seasons = Map.of(seasons)..[notice.id] = notice;
  }
  return identical(seasons, state.seriesSeasons)
      ? state
      : state.copyWith(seriesSeasons: seasons);
}
