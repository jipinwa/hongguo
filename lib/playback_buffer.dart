import 'dart:convert';

class PlaybackBufferRange {
  const PlaybackBufferRange(this.start, this.end);
  final double start, end;
}

List<PlaybackBufferRange> playbackBufferRanges(String raw) {
  try {
    final data = jsonDecode(raw) as Map;
    final ranges = <PlaybackBufferRange>[];
    for (final row in (data['seekable-ranges'] as List? ?? []).take(64)) {
      if (row is! Map || row['start'] is! num || row['end'] is! num) continue;
      final start = (row['start'] as num).toDouble(),
          end = (row['end'] as num).toDouble();
      if (!start.isFinite || !end.isFinite || start < 0 || end <= start) {
        continue;
      }
      ranges.add(PlaybackBufferRange(start, end));
    }
    ranges.sort((a, b) => a.start.compareTo(b.start));
    final merged = <PlaybackBufferRange>[];
    for (final range in ranges) {
      if (merged.isEmpty || range.start > merged.last.end) {
        merged.add(range);
      } else if (range.end > merged.last.end) {
        merged[merged.length - 1] = PlaybackBufferRange(
          merged.last.start,
          range.end,
        );
      }
    }
    return merged;
  } catch (_) {
    return const [];
  }
}

PlaybackBufferRange? continuousPlaybackBuffer(
  List<PlaybackBufferRange> ranges,
  double position,
) {
  if (!position.isFinite || position < 0) return null;
  for (final range in ranges) {
    if (position >= range.start && position <= range.end) return range;
  }
  return null;
}
