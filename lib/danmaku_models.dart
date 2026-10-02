const danmakuWindowMs = 30000;
const danmakuLifetimeMs = 8000;
const danmakuMaxDurationMs = 86400000;

bool _danmakuEmojiModifier(int rune) => rune == 0xfe0f || rune == 0xfe0e;

bool _danmakuEmojiJoiner(int rune) => rune == 0x200d;

String trimDanmakuText(String text, int limit) {
  final runes = text.runes.toList();
  if (runes.length <= limit) return text;
  var cut = limit;
  while (cut > 0) {
    var retreat = false;
    if (cut < runes.length && _danmakuEmojiModifier(runes[cut])) {
      cut--;
      retreat = true;
    }
    if (cut > 0 && _danmakuEmojiJoiner(runes[cut - 1])) {
      cut--;
      retreat = true;
    }
    if (!retreat) break;
  }
  return String.fromCharCodes(runes.take(cut));
}

class DanmakuItem {
  const DanmakuItem({
    required this.id,
    required this.text,
    required this.timeMs,
  });
  final String id;
  final String text;
  final int timeMs;
}

class DanmakuPage {
  const DanmakuPage({
    required this.episodeId,
    required this.startMs,
    required this.nextMs,
    required this.items,
  });
  final String episodeId;
  final int startMs;
  final int nextMs;
  final List<DanmakuItem> items;

  factory DanmakuPage.fromJson(
    Map<String, dynamic> json, {
    required String episodeId,
    required int startMs,
    required int durationMs,
  }) {
    final next = json['nextMs'];
    final rows = json['items'];
    if (json['episodeId'] != episodeId ||
        json['startMs'] != startMs ||
        next is! int ||
        next <= startMs ||
        next > durationMs ||
        rows is! List) {
      throw const FormatException('弹幕分集或时间范围不符');
    }
    final items = <DanmakuItem>[];
    final seen = <String>{};
    for (final row in rows.take(90)) {
      if (row is! Map) continue;
      final id = row['id'], text = row['text'], time = row['timeMs'];
      if (id is! String ||
          id.isEmpty ||
          id.length > 120 ||
          text is! String ||
          time is! int ||
          time < startMs ||
          time >= next ||
          seen.contains(id)) {
        continue;
      }
      final cleaned = text
          .replaceAll(RegExp(r'[\x00-\x1f\x7f-\x9f\u2028\u2029]'), ' ')
          .trim();
      if (cleaned.isEmpty) continue;
      seen.add(id);
      items.add(
        DanmakuItem(
          id: id,
          text: trimDanmakuText(cleaned, 181),
          timeMs: time,
        ),
      );
    }
    items.sort((a, b) => a.timeMs.compareTo(b.timeMs));
    return DanmakuPage(
      episodeId: episodeId,
      startMs: startMs,
      nextMs: next,
      items: List.unmodifiable(items),
    );
  }
}
