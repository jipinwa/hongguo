import 'package:duanju_app/models.dart';
import 'package:duanju_app/playback_recovery.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  PlaybackPlan route(int index, {int count = 3}) => PlaybackPlan(
    url: 'https://media.test/$index.mp4',
    session: 'session-$index',
    routeIndex: index,
    routeCount: count,
  );

  test('tries alternate routes before one fresh resolution, then stops', () {
    final recovery = PlaybackRecovery();
    expect(recovery.next(route(0)), PlaybackRecoveryAction.alternative);
    expect(recovery.next(route(1)), PlaybackRecoveryAction.alternative);
    expect(recovery.next(route(2)), PlaybackRecoveryAction.refresh);
    expect(recovery.next(route(0)), PlaybackRecoveryAction.stop);
    recovery.reset();
    expect(recovery.next(route(0)), PlaybackRecoveryAction.alternative);
  });

  test('one-route sources refresh once without endless retrying', () {
    final recovery = PlaybackRecovery();
    expect(recovery.next(route(0, count: 1)), PlaybackRecoveryAction.refresh);
    expect(recovery.next(route(0, count: 1)), PlaybackRecoveryAction.stop);
    expect(recovery.next(route(0, count: 1)), PlaybackRecoveryAction.stop);
  });

  test('many failing alternatives cannot exceed the automatic retry limit', () {
    final recovery = PlaybackRecovery();
    for (var i = 0; i < 3; i++) {
      expect(
        recovery.next(route(i, count: 10)),
        PlaybackRecoveryAction.alternative,
      );
    }
    expect(recovery.next(route(3, count: 10)), PlaybackRecoveryAction.stop);
  });

  test(
    'a stalled foreground stream times out but paused and background playback do not',
    () {
      final health = PlaybackHealth();
      final start = DateTime(2026, 9, 18);
      bool stalled(
        int seconds, {
        bool playing = true,
        bool foreground = true,
        int position = 5,
      }) => health.stalled(
        position: Duration(seconds: position),
        playing: playing,
        foreground: foreground,
        now: start.add(Duration(seconds: seconds)),
      );
      expect(stalled(0), isFalse);
      expect(stalled(19), isFalse);
      expect(stalled(20), isTrue);
      expect(stalled(21, position: 6), isFalse);
      expect(stalled(40, position: 6), isFalse);
      expect(stalled(60, playing: false, position: 6), isFalse);
      expect(stalled(80, playing: false, position: 6), isFalse);
      expect(stalled(100, foreground: false, position: 6), isFalse);
      expect(stalled(101, position: 6), isFalse);
      health.reset();
      expect(stalled(200, position: 6), isFalse);
    },
  );
}
