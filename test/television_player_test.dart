import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/player_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'player_fixtures.dart';
import 'remote_test_helpers.dart';

void main() {
  Future<void> settle(WidgetTester tester) async {
    for (var index = 0; index < 12; index++) {
      await tester.pump(const Duration(milliseconds: 10));
    }
  }

  Future<void> press(WidgetTester tester, LogicalKeyboardKey key) async {
    await tester.sendKeyEvent(key);
    await settle(tester);
  }

  Future<LocalStore> mount(
    WidgetTester tester,
    RouteRepository repository,
    ScriptedPlayer player, {
    int initialIndex = 0,
  }) async {
    tester.view.physicalSize = const Size(960, 540);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    final detail = DramaDetail(
      FixtureRepository.free,
      List.generate(100, (index) => Episode({'id': '${index + 1}'}, index + 1)),
    );
    await tester.pumpWidget(
      televisionHost(
        child: Builder(
          builder: (context) => Scaffold(
            body: Center(
              child: FilledButton(
                child: const Text('打开测试视频'),
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                    builder: (_) => PlayerScreen(
                      detail: detail,
                      initialIndex: initialIndex,
                      initialPosition: 7,
                      repository: repository,
                      store: store,
                      playerFactory: () => Player(platformPlayer: player),
                      videoBuilder: (controls) => controls,
                    ),
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.tap(find.text('打开测试视频'));
    await settle(tester);
    await tester.pump(const Duration(milliseconds: 350));
    await settle(tester);
    return store;
  }

  Future<void> leave(
    WidgetTester tester,
    RouteRepository repository,
    ScriptedPlayer player,
  ) async {
    await press(tester, LogicalKeyboardKey.escape);
    await tester.pump(const Duration(milliseconds: 350));
    await settle(tester);
    expect(find.text('打开测试视频'), findsOneWidget);
    expect(find.byType(PlayerScreen), findsNothing);
    expect(player.disposed, isTrue);
    expect(repository.active, isEmpty);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
    await settle(tester);
  }

  testWidgets(
    'TV remote pauses, seeks when hidden, seeks on the timeline and returns from fullscreen',
    (tester) async {
      final repository = RouteRepository();
      final player = ScriptedPlayer();
      final store = await mount(tester, repository, player);
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'tv-player-play');
      await press(tester, LogicalKeyboardKey.select);
      expect(player.state.playing, isFalse);
      await press(tester, LogicalKeyboardKey.select);
      expect(player.state.playing, isTrue);
      await tester.pump(const Duration(seconds: 6));
      await settle(tester);
      expect(find.byKey(const ValueKey('tv-play-pause')), findsNothing);
      await press(tester, LogicalKeyboardKey.arrowRight);
      expect(player.state.position, const Duration(seconds: 17));
      await press(tester, LogicalKeyboardKey.arrowLeft);
      await press(tester, LogicalKeyboardKey.arrowLeft);
      expect(player.state.position, Duration.zero);
      await press(tester, LogicalKeyboardKey.arrowUp);
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'tv-player-play');
      await press(tester, LogicalKeyboardKey.arrowUp);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'tv-player-progress',
      );
      await press(tester, LogicalKeyboardKey.arrowRight);
      expect(player.state.position, const Duration(seconds: 10));
      await leave(tester, repository, player);
      expect(store.watched(FixtureRepository.free.id)?.position, 10);
    },
  );

  testWidgets(
    'TV selection restores the current episode and panels keep focus during playback',
    (tester) async {
      final repository = RouteRepository();
      final player = ScriptedPlayer();
      await mount(tester, repository, player, initialIndex: 70);
      await press(tester, LogicalKeyboardKey.arrowRight);
      await press(tester, LogicalKeyboardKey.arrowRight);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'tv-player-episodes',
      );
      await press(tester, LogicalKeyboardKey.select);
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'remote-71');
      await tester.pump(const Duration(seconds: 6));
      await settle(tester);
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'remote-71');
      await press(tester, LogicalKeyboardKey.arrowRight);
      await press(tester, LogicalKeyboardKey.select);
      expect(repository.requestedEpisodes.last, 72);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'tv-player-episodes',
      );
      expect(repository.active.length, 1);
      await press(tester, LogicalKeyboardKey.arrowRight);
      await press(tester, LogicalKeyboardKey.select);
      expect(find.text('倍速'), findsOneWidget);
      await press(tester, LogicalKeyboardKey.arrowRight);
      await press(tester, LogicalKeyboardKey.select);
      expect(player.state.rate, 1.25);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'tv-player-settings',
      );
      await player.seek(const Duration(seconds: 28));
      await player.pause();
      await settle(tester);
      await press(tester, LogicalKeyboardKey.select);
      focusRemote(tester, find.byKey(const ValueKey('tv-quality-1080')));
      await settle(tester);
      await press(tester, LogicalKeyboardKey.arrowRight);
      await press(tester, LogicalKeyboardKey.select);
      expect(repository.requestedQualities.last, 720);
      expect(player.state.position, const Duration(seconds: 28));
      expect(player.state.rate, 1.25);
      expect(player.state.playing, isFalse);
      expect(
        FocusManager.instance.primaryFocus?.debugLabel,
        'tv-player-settings',
      );
      await leave(tester, repository, player);
    },
  );

  testWidgets(
    'TV playback error puts remote focus on retry and restores controls after retry',
    (tester) async {
      final repository = RouteRepository()..broken = true;
      final player = ScriptedPlayer();
      await mount(tester, repository, player);
      for (var attempt = 0; attempt < 5; attempt++) {
        await tester.pump(const Duration(seconds: 1));
        await settle(tester);
      }
      expect(find.text('重试播放'), findsOneWidget);
      expect(find.byKey(const ValueKey('tv-play-pause')), findsNothing);
      repository.broken = false;
      await press(tester, LogicalKeyboardKey.select);
      expect(find.text('重试播放'), findsNothing);
      expect(player.state.position, const Duration(seconds: 7));
      expect(FocusManager.instance.primaryFocus?.debugLabel, 'tv-player-play');
      await leave(tester, repository, player);
    },
  );
}
