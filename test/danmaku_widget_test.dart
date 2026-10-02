import 'dart:convert';

import 'package:duanju_app/danmaku_controller.dart';
import 'package:duanju_app/danmaku_overlay.dart';
import 'package:duanju_app/local_profiles.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/player_menu.dart';
import 'package:duanju_app/player_screen.dart';
import 'package:duanju_app/playback_preferences.dart';
import 'package:duanju_app/television_controls.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'danmaku_fixtures.dart';
import 'danmaku_test.dart' show danmakuPlan, updateDanmaku, flushDanmaku;
import 'fixtures.dart';
import 'player_fixtures.dart';

void main() {
  Future<void> settle(WidgetTester tester) async {
    for (var i = 0; i < 15; i++) {
      await tester.pump(const Duration(milliseconds: 20));
    }
  }

  Future<LocalStore> createStore([
    Map<String, Object> values = const {},
  ]) async {
    SharedPreferences.setMockInitialValues(values);
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    return store;
  }

  Future<LocalStore> mount(
    WidgetTester tester,
    DanmakuFixtureRepository repository,
    ScriptedPlayer player, {
    LocalStore? store,
  }) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(420, 880);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);
    final local = store ?? await createStore();
    final detail = await repository.detail(FixtureRepository.free);
    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark(),
        home: PlayerScreen(
          detail: detail,
          initialIndex: 0,
          repository: repository,
          store: local,
          playerFactory: () => Player(platformPlayer: player),
          videoBuilder: (controls) => controls,
        ),
      ),
    );
    await settle(tester);
    return local;
  }

  Future<void> unmount(WidgetTester tester, ScriptedPlayer player) async {
    await tester.pumpWidget(const SizedBox.shrink());
    await settle(tester);
    expect(player.disposed, isTrue);
    expect(tester.takeException(), isNull);
  }

  testWidgets(
    'danmaku uses continuous linear motion and synchronizes pause, buffering, speed and seek',
    (tester) async {
      var now = DateTime(2026);
      final controller = DanmakuController(
        DanmakuFixtureRepository(),
        now: () => now,
      )..setPlan(danmakuPlan);
      addTearDown(controller.dispose);
      updateDanmaku(controller);
      await flushDanmaku();
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Center(
              child: SizedBox(
                width: 400,
                height: 300,
                child: DanmakuOverlay(
                  controller: controller,
                  aspectRatio: 4 / 3,
                ),
              ),
            ),
          ),
        ),
      );
      await tester.pump();
      double x() => tester
          .widget<Transform>(
            find.byKey(const ValueKey('danmaku-motion-1001-0')),
          )
          .transform
          .storage[12];
      final start = x();
      now = now.add(const Duration(seconds: 2));
      await tester.pump(const Duration(seconds: 2));
      final afterTwo = x();
      expect(afterTwo, lessThan(start));
      final travel = (start - afterTwo) * 4;
      updateDanmaku(controller, position: 2000, playing: false);
      await tester.pump();
      final paused = x();
      now = now.add(const Duration(seconds: 2));
      await tester.pump(const Duration(seconds: 2));
      expect(x(), closeTo(paused, .01));
      updateDanmaku(controller, position: 2000, speed: 2);
      await tester.pump();
      now = now.add(const Duration(seconds: 1));
      await tester.pump(const Duration(seconds: 1));
      expect(x(), closeTo(start - travel * .5, .1));
      updateDanmaku(controller, position: 4000, speed: 2, buffering: true);
      await tester.pump();
      final buffered = x();
      now = now.add(const Duration(seconds: 2));
      await tester.pump(const Duration(seconds: 2));
      expect(x(), closeTo(buffered, .01));
      controller.beginSeek();
      await tester.pump();
      expect(find.text('合成弹幕 1001'), findsNothing);
      controller.endSeek(const Duration(seconds: 6));
      await tester.pump();
      expect(x(), closeTo(start - travel * .75, .1));
      controller.setEnabled(false);
      await tester.pump();
      expect(find.text('合成弹幕 1001'), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets(
    'optional requests do not delay video and late replies never appear on another episode',
    (tester) async {
      final repository = DanmakuFixtureRepository()..holdDanmaku = true;
      final player = ScriptedPlayer();
      final store = await mount(tester, repository, player);
      expect(player.opened.length, 1);
      expect(player.state.playing, isTrue);
      expect(find.text('正在准备播放'), findsNothing);
      final old = repository.danmakuCalls.single;
      await tester.tap(find.byKey(const ValueKey('play-episode-2')));
      await settle(tester);
      final current = repository.danmakuCalls.last;
      expect(current.plan.danmakuId, '1002');
      old.result.complete(DanmakuFixtureRepository.page(old));
      await settle(tester);
      expect(find.text('合成弹幕 1001'), findsNothing);
      current.result.complete(DanmakuFixtureRepository.page(current));
      await settle(tester);
      expect(find.text('合成弹幕 1002'), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('player-danmaku-toggle')));
      await settle(tester);
      expect(store.playbackPreferences.danmaku, isFalse);
      expect(find.text('合成弹幕 1002'), findsNothing);
      expect(repository.primaryCalls, 2);
      expect(repository.fallbackCalls, 0);
      await unmount(tester, player);
    },
  );

  testWidgets(
    'danmaku failure and manual retry stay on player controls without interrupting playback',
    (tester) async {
      final repository = DanmakuFixtureRepository()..failDanmaku = true;
      final player = ScriptedPlayer();
      await mount(tester, repository, player);
      expect(player.state.playing, isTrue);
      expect(find.text('暂时无法播放'), findsNothing);
      repository.failDanmaku = false;
      await tester.tap(find.byKey(const ValueKey('player-danmaku-toggle')));
      await settle(tester);
      expect(repository.danmakuCalls.length, 2);
      expect(repository.primaryCalls, 1);
      expect(repository.fallbackCalls, 0);
      expect(player.state.playing, isTrue);
      expect(find.text('合成弹幕 1001'), findsOneWidget);
      await unmount(tester, player);
    },
  );

  testWidgets('local playback skips danmaku even when the switch is on', (
    tester,
  ) async {
    final repository = DanmakuFixtureRepository()..local = true;
    final player = ScriptedPlayer();
    final store = await mount(tester, repository, player);
    expect(repository.danmakuCalls, isEmpty);
    await tester.tap(find.byKey(const ValueKey('player-danmaku-toggle')));
    await settle(tester);
    expect(store.playbackPreferences.danmaku, isFalse);
    expect(repository.danmakuCalls, isEmpty);
    await unmount(tester, player);
  });

  testWidgets(
    'switching users clears in-flight danmaku before a delayed response returns',
    (tester) async {
      final store = await createStore({
        'profiles': jsonEncode(
          [
            LocalProfile(
              id: 'default',
              name: '管理员',
              admin: true,
              salt: ''.padLeft(32, 'a'),
              pinHash: ''.padLeft(64, 'b'),
            ),
            const LocalProfile(
              id: 'viewer',
              name: '观看用户',
              sources: ['hongguo'],
            ),
            const LocalProfile(id: 'visitor', name: '访客', download: false),
          ].map((profile) => profile.toJson()).toList(),
        ),
        'activeProfile': 'viewer',
      });
      final repository = DanmakuFixtureRepository()..holdDanmaku = true;
      final player = ScriptedPlayer();
      await mount(tester, repository, player, store: store);
      final pending = repository.danmakuCalls.single;
      await store.switchProfile('visitor');
      pending.result.complete(DanmakuFixtureRepository.page(pending));
      await settle(tester);
      expect(repository.danmakuCancellations, 1);
      expect(find.text('合成弹幕 1001'), findsNothing);
      await unmount(tester, player);
    },
    timeout: const Timeout(Duration(seconds: 45)),
  );

  testWidgets(
    'large text settings and TV controls expose the danmaku preference',
    (tester) async {
      tester.view.devicePixelRatio = 1;
      tester.view.physicalSize = const Size(360, 640);
      addTearDown(tester.view.resetDevicePixelRatio);
      addTearDown(tester.view.resetPhysicalSize);
      PlaybackPreferences? chosen;
      await tester.pumpWidget(
        MaterialApp(
          home: MediaQuery(
            data: const MediaQueryData(
              size: Size(360, 640),
              textScaler: TextScaler.linear(1.8),
            ),
            child: Scaffold(
              body: PlayerMenu(
                section: PlayerMenuSection.settings,
                episodes: const [],
                currentIndex: 0,
                preferences: const PlaybackPreferences(),
                qualities: const [1080, 720],
                actualQuality: 1080,
                local: false,
                favorite: false,
                mobile: true,
                showDanmaku: true,
                danmakuStatus: '这段暂无弹幕',
                onEpisode: (_) {},
                onFavorite: () async {},
                onPreferences: (value) async {
                  chosen = value;
                },
              ),
            ),
          ),
        ),
      );
      await tester.ensureVisible(
        find.byKey(const ValueKey('player-danmaku-enabled')),
      );
      await tester.tap(find.byKey(const ValueKey('player-danmaku-enabled')));
      await settle(tester);
      expect(chosen?.danmaku, isFalse);
      expect(tester.takeException(), isNull);
      TelevisionPlaybackSetting? selected;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (context) => TextButton(
                onPressed: () async {
                  selected = await showDialog<TelevisionPlaybackSetting>(
                    context: context,
                    builder: (_) => TelevisionSettingsDialog(
                      speed: 1,
                      quality: 0,
                      qualities: const [],
                      favorite: false,
                      onFavorite: () {},
                      showDanmaku: true,
                      danmaku: true,
                      danmakuStatus: '这段暂无弹幕',
                    ),
                  );
                },
                child: const Text('打开电视设置'),
              ),
            ),
          ),
        ),
      );
      await tester.tap(find.text('打开电视设置'));
      await settle(tester);
      await tester.ensureVisible(
        find.byKey(const ValueKey('tv-danmaku-enabled')),
      );
      await tester.tap(find.byKey(const ValueKey('tv-danmaku-enabled')));
      await settle(tester);
      expect(selected?.danmaku, isFalse);
      expect(tester.takeException(), isNull);
    },
  );
}
