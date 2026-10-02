import 'models.dart';

enum CatalogSort {
  source('默认顺序'),
  name('名称'),
  season('自然季号'),
  date('上线日期'),
  heat('热度'),
  views('播放量');

  const CatalogSort(this.label);
  final String label;
}

class CatalogView {
  const CatalogView({
    this.sort = CatalogSort.source,
    this.release = '',
    this.allSources = false,
  });
  final CatalogSort sort;
  final String release;
  final bool allSources;

  CatalogView copyWith({
    CatalogSort? sort,
    String? release,
    bool? allSources,
  }) => CatalogView(
    sort: sort ?? this.sort,
    release: release ?? this.release,
    allSources: allSources ?? this.allSources,
  );

  Map<String, dynamic> toJson() => {
    'sort': sort.name,
    'release': release,
    'allSources': allSources,
  };

  factory CatalogView.fromJson(Map<String, dynamic> value) => CatalogView(
    sort:
        CatalogSort.values
            .where((sort) => sort.name == value['sort'])
            .firstOrNull ??
        CatalogSort.source,
    release: {'', 'finished', 'ongoing', 'unknown'}.contains(value['release'])
        ? value['release'] as String
        : '',
    allSources: value['allSources'] == true,
  );
}

String normalizedSearchText(String text) {
  final value = String.fromCharCodes(
    text.runes.map(
      (rune) => rune >= 0xff01 && rune <= 0xff5e ? rune - 0xfee0 : rune,
    ),
  );
  return value.toLowerCase().replaceAll(
    RegExp(r'[\s\u3000，。！？、：；·「」『』（）【】《》“”‘’.,!?:;\-_/()\[\]{}]+'),
    '',
  );
}

bool matchesDramaQuery(Drama drama, String query) {
  final fields = normalizedSearchText(
    '${drama.title} ${drama.description} ${drama.tags.join(' ')}',
  );
  final terms = query
      .trim()
      .split(RegExp(r'\s+'))
      .map(normalizedSearchText)
      .where((word) => word.isNotEmpty);
  return terms.every(fields.contains);
}

double? catalogMetric(String text) {
  final cleaned = text.toLowerCase().replaceAll(RegExp(r'[,，\s]'), '');
  final match = RegExp(
    r'^(\d+(?:\.\d+)?)([亿万千wkmb]?)(?:\+)?(?:次播放|次观看|人看过|热度|播放|观看|次)?\+?$',
  ).firstMatch(cleaned);
  if (match == null) return null;
  final number = double.tryParse(match[1]!);
  if (number == null || !number.isFinite) return null;
  return number *
      switch (match[2]) {
        '亿' => 100000000,
        '万' || 'w' => 10000,
        '千' || 'k' => 1000,
        'm' => 1000000,
        'b' => 1000000000,
        _ => 1,
      };
}

int _seasonNumber(String label) {
  final numeric = int.tryParse(label);
  if (numeric != null) return numeric;
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
  var digit = 0;
  for (final rune in label.runes) {
    final character = String.fromCharCode(rune);
    if (digits.containsKey(character)) {
      digit = digit * 10 + digits[character]!;
    } else {
      final multiplier = character == '百' ? 100 : 10;
      number += (digit == 0 ? 1 : digit) * multiplier;
      digit = 0;
    }
  }
  return number + digit;
}

String _naturalTitle(String title) =>
    normalizedSearchText(title).replaceAllMapped(
      RegExp(r'第([0-9零〇一二两兩三四五六七八九十百]+)([季部])'),
      (match) => '第${_seasonNumber(match[1]!)}${match[2]}',
    );

int naturalTitleCompare(String left, String right) {
  final pattern = RegExp(r'\d+|\D+');
  final a = pattern
      .allMatches(_naturalTitle(left))
      .map((match) => match[0]!)
      .toList();
  final b = pattern
      .allMatches(_naturalTitle(right))
      .map((match) => match[0]!)
      .toList();
  for (var index = 0; index < a.length && index < b.length; index++) {
    final numberA = int.tryParse(a[index]);
    final numberB = int.tryParse(b[index]);
    final comparison = numberA != null && numberB != null
        ? numberA.compareTo(numberB)
        : a[index].compareTo(b[index]);
    if (comparison != 0) return comparison;
  }
  return a.length.compareTo(b.length);
}

int _descending(num? a, num? b) => a == null
    ? (b == null ? 0 : 1)
    : b == null
    ? -1
    : b.compareTo(a);

List<Drama> sortCatalog(Iterable<Drama> rows, CatalogView view) {
  final items = rows.where((drama) {
    if (view.release.isEmpty) return true;
    final release = drama.releaseStatus == 'completed'
        ? 'finished'
        : drama.releaseStatus;
    return view.release == 'unknown'
        ? release.isEmpty || release == 'unknown'
        : release == view.release;
  }).toList();
  if (view.sort == CatalogSort.source) return items;
  final positions = {for (final entry in items.indexed) entry.$2.id: entry.$1};
  final metrics = <String, num?>{
    for (final drama in items)
      drama.id: switch (view.sort) {
        CatalogSort.heat => catalogMetric(drama.heat),
        CatalogSort.views => catalogMetric(drama.views),
        CatalogSort.date => DateTime.tryParse(
          drama.onlineDate,
        )?.millisecondsSinceEpoch,
        _ => null,
      },
  };
  items.sort((a, b) {
    final comparison = switch (view.sort) {
      CatalogSort.source => 0,
      CatalogSort.name => normalizedSearchText(
        a.title,
      ).compareTo(normalizedSearchText(b.title)),
      CatalogSort.season => naturalTitleCompare(a.title, b.title),
      _ => _descending(metrics[a.id], metrics[b.id]),
    };
    return comparison != 0
        ? comparison
        : positions[a.id]!.compareTo(positions[b.id]!);
  });
  return items;
}
