import 'dart:convert';
import 'dart:io';

import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/downloads_screen.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/media_library.dart';
import 'package:duanju_app/media_pipeline.dart';
import 'package:flutter_foreground_task/flutter_foreground_task.dart';
import 'package:duanju_app/main.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/player_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';
import 'package:shared_preferences/shared_preferences.dart';

const fixtureBase = String.fromEnvironment('FIXTURE_BASE_URL');

class DeviceFixtureRepository extends AppRepository {
  final native = NativeRepository();
  final resolved = <int>[];
  final released = <String>[];
  static const drama = Drama(
    id: 'hongguo:700001',
    source: 'hongguo',
    title: '设备播放验证',
    episodes: 3,
  );

  @override
  Future<void> initialize() => native.initialize();
  @override
  Future<CatalogPage> cached(String source, {String category = ''}) async =>
      CatalogPage([]);
  @override
  Future<String> cover(Drama drama, {bool force = false}) =>
      native.cover(drama, force: force);
  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async => CatalogPage([drama]);
  @override
  Future<DramaDetail> detail(Drama drama) async => DramaDetail(drama, [
    for (final entry in {
      1: 'clear.mp4',
      2: 'index.m3u8',
      3: 'encrypted.mp4',
    }.entries)
      Episode({
        'id': '${entry.key}',
        'source': 'hongguo',
        'currentEpisode': entry.key,
        'title': '第${entry.key}集',
        'videoUrl': '$fixtureBase/${entry.value}',
        'referer': '$fixtureBase/',
      }, entry.key),
  ]);
  @override
  Future<PlaybackPlan> resolve(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) async {
    final plan = await native.resolve(drama, episode, quality: quality);
    resolved.add(episode.number);
    if (episode.number == 3) {
      return PlaybackPlan(
        url: plan.url,
        local: plan.local,
        headers: plan.headers,
        decryptionKey: '00112233445566778899aabbccddeeff',
        session: plan.session,
        routeIndex: plan.routeIndex,
        routeCount: plan.routeCount,
      );
    }
    return plan;
  }

  @override
  Future<PlaybackPlan> fallback(PlaybackPlan current) =>
      native.fallback(current);

  @override
  Future<void> cancelPlayback() => native.cancelPlayback();
  @override
  Future<void> release(String session) async {
    if (session.isNotEmpty) released.add(session);
    await native.release(session);
  }
}

class DownloadFixtureRepository extends DeviceFixtureRepository {
  final locallyOpened = <int>[];
  @override
  Future<String> downloadDirectory() => native.downloadDirectory();
  @override
  Future<int> workLease(String id, String command) =>
      native.workLease(id, command);

  @override
  bool get supportsDownloads => true;
  @override
  Future<List<DownloadJob>> downloads() => native.downloads();
  @override
  Future<int> enqueueDownloads(
    DramaDetail detail,
    List<Episode> episodes, {
    int quality = 0,
  }) => native.enqueueDownloads(detail, episodes, quality: quality);
  @override
  Future<void> controlDownloads(String command, {String id = ''}) =>
      native.controlDownloads(command, id: id);
  @override
  Future<PlaybackPlan?> localPlayback(Drama drama, Episode episode) async {
    final plan = await native.localPlayback(drama, episode);
    if (plan == null) return null;
    locallyOpened.add(episode.number);
    return episode.number == 3
        ? PlaybackPlan(
            url: plan.url,
            local: plan.local,
            decryptionKey: '00112233445566778899aabbccddeeff',
          )
        : plan;
  }
}

Future<Map<String, dynamic>> fixtureControl(String action) async {
  final client = HttpClient();
  try {
    final request = await client.openUrl(
      action == 'status' ? 'GET' : 'POST',
      Uri.parse('$fixtureBase/_test/$action'),
    );
    final response = await request.close();
    if (response.statusCode != 200) {
      throw StateError('Fixture control unavailable');
    }
    return jsonDecode(await utf8.decoder.bind(response).join())
        as Map<String, dynamic>;
  } finally {
    client.close(force: true);
  }
}

class RecoveryFixtureRepository extends DeviceFixtureRepository {
  int primaryCalls = 0;
  int fallbackCalls = 0;
  bool failAll = false;
  final activeSessions = <String>{};

  Future<PlaybackPlan> _route(
    Drama drama,
    Episode episode,
    bool alternate,
  ) async {
    final media = alternate && !failAll ? 'index.m3u8' : 'missing.mp4';
    final plan = await native.resolve(
      drama,
      Episode({
        ...episode.raw,
        'videoUrl': '$fixtureBase/$media',
      }, episode.number),
    );
    activeSessions.add(plan.session);
    return PlaybackPlan(
      url: plan.url,
      headers: plan.headers,
      decryptionKey: plan.decryptionKey,
      session: plan.session,
      quality: 1080,
      qualities: const [1080, 720],
      routeIndex: alternate ? 1 : 0,
      routeCount: 2,
    );
  }

  @override
  Future<PlaybackPlan> resolve(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) {
    primaryCalls++;
    return _route(drama, episode, false);
  }

  @override
  Future<PlaybackPlan> fallback(PlaybackPlan current) {
    fallbackCalls++;
    return _route(
      DeviceFixtureRepository.drama,
      Episode({'id': '1', 'source': 'hongguo', 'currentEpisode': 1}, 1),
      true,
    );
  }

  @override
  Future<void> release(String session) async {
    await super.release(session);
    activeSessions.remove(session);
  }
}

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('native MP4, encrypted HLS, CENC, swipe, rotation and resume', (
    tester,
  ) async {
    expect(fixtureBase, startsWith('http://127.0.0.1:'));
    expect(const bool.fromEnvironment('DISABLE_REMOTE_IMAGES'), isTrue);
    MediaKit.ensureInitialized();
    await SystemChrome.setPreferredOrientations([DeviceOrientation.portraitUp]);
    final repository = DeviceFixtureRepository();
    await repository.initialize();
    final store = LocalStore(await SharedPreferences.getInstance());
    final samples = <Map<String, Object?>>[];

    Future<void> until(bool Function() ready, String step) async {
      final timer = Stopwatch()..start();
      while (!ready()) {
        if (timer.elapsed > const Duration(seconds: 35)) {
          fail('Timed out: $step; resolved=${repository.resolved}');
        }
        await tester.pump(const Duration(milliseconds: 200));
      }
    }

    Player player() =>
        tester.widget<Video>(find.byType(Video)).controller.player;

    Future<void> decoded(int episode) async {
      await until(
        () =>
            repository.resolved.isNotEmpty &&
            repository.resolved.last == episode &&
            (player().state.width ?? 0) > 0 &&
            player().state.duration.inSeconds >= 18 &&
            player().state.position.inMilliseconds > 500,
        'decode episode $episode',
      );
      expect(find.text('暂时无法播放'), findsNothing);
      expect(tester.takeException(), isNull);
      samples.add({
        'episode': episode,
        'width': player().state.width,
        'height': player().state.height,
        'positionMs': player().state.position.inMilliseconds,
        'durationMs': player().state.duration.inMilliseconds,
      });
      binding.reportData ??= {};
      binding.reportData!['playbackSamples'] = samples;
    }

    await tester.pumpWidget(DuanjuApp(repository: repository, store: store));
    await until(() => find.text('设备播放验证').evaluate().isNotEmpty, 'catalog');
    await tester.tap(find.text('设备播放验证'));
    await until(
      () => find.byKey(const ValueKey('episode-1')).evaluate().isNotEmpty,
      'detail',
    );
    await tester.tap(find.byKey(const ValueKey('episode-1')));
    await until(() => find.byType(Video).evaluate().isNotEmpty, 'player');
    await player().setVolume(0);
    await decoded(1);
    await player().seek(const Duration(seconds: 5));
    await until(() => player().state.position.inSeconds >= 5, 'seek');
    await tester.fling(find.byType(Video), const Offset(0, -160), 900);
    await decoded(2);

    await tester.tap(find.byTooltip('旋转与全屏').first);
    await until(
      () =>
          MediaQuery.orientationOf(tester.element(find.byType(Video))) ==
          Orientation.landscape,
      'landscape',
    );
    await binding.convertFlutterSurfaceToImage();
    await tester.pump(const Duration(milliseconds: 400));
    await binding.takeScreenshot('android-landscape-playback');
    await tester.tap(find.byTooltip('退出全屏').first);
    await until(
      () =>
          MediaQuery.orientationOf(tester.element(find.byType(Video))) ==
          Orientation.portrait,
      'portrait',
    );
    await tester.tap(find.byKey(const ValueKey('play-episode-3')));
    await decoded(3);
    await tester.tap(find.byKey(const ValueKey('play-episode-1')));
    await decoded(1);
    await player().seek(player().state.duration - const Duration(seconds: 1));
    await decoded(2);
    final backTooltip = MaterialLocalizations.of(
      tester.element(find.byType(Video)),
    ).backButtonTooltip;
    await tester.tap(find.byTooltip(backTooltip));
    await until(
      () => store.watched(DeviceFixtureRepository.drama.id)?.episode == 2,
      'saved history',
    );
    expect(
      store.watched(DeviceFixtureRepository.drama.id)!.position,
      greaterThan(0),
    );
    expect(repository.released, isNotEmpty);
    await tester.pump(const Duration(milliseconds: 300));
    await binding.takeScreenshot('android-resume-detail');
    binding.reportData ??= {};
    binding.reportData!['playbackSamples'] = samples;
    binding.reportData!['releasedSessions'] = repository.released.length;
    binding.reportData!['savedEpisode'] = store
        .watched(DeviceFixtureRepository.drama.id)!
        .episode;
    if (const bool.fromEnvironment('CHECK_LIVE_CATALOG')) {
      final catalog = await repository.native.catalog('hongguo');
      expect(catalog.items, isNotEmpty);
      binding.reportData!['hongguoCatalogCount'] = catalog.items.length;
    }
  }, timeout: const Timeout(Duration(minutes: 4)));

  testWidgets(
    'failed media switches routes, keeps progress and stops retrying at the limit',
    (tester) async {
      expect(fixtureBase, startsWith('http://127.0.0.1:'));
      expect(const bool.fromEnvironment('DISABLE_REMOTE_IMAGES'), isTrue);
      MediaKit.ensureInitialized();
      final repository = RecoveryFixtureRepository();
      await repository.initialize();
      final store = LocalStore(await SharedPreferences.getInstance());
      final detail = await repository.detail(DeviceFixtureRepository.drama);
      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData.dark(),
          home: PlayerScreen(
            detail: detail,
            initialIndex: 0,
            initialPosition: 6,
            repository: repository,
            store: store,
          ),
        ),
      );

      Player player() =>
          tester.widget<Video>(find.byType(Video)).controller.player;
      Future<void> until(bool Function() ready, String step) async {
        final timer = Stopwatch()..start();
        while (!ready()) {
          if (timer.elapsed > const Duration(seconds: 40)) {
            fail(
              'Timed out: $step; primary=${repository.primaryCalls}, fallback=${repository.fallbackCalls}',
            );
          }
          await tester.pump(const Duration(milliseconds: 200));
        }
      }

      await player().setVolume(0);
      await until(
        () =>
            repository.fallbackCalls == 1 &&
            player().state.position.inMilliseconds >= 6000 &&
            (player().state.width ?? 0) > 0,
        'automatic backup and resume',
      );
      expect(repository.primaryCalls, 1);
      expect(find.text('暂时无法播放'), findsNothing);
      expect(repository.activeSessions.length, 1);
      await tester.tap(find.byTooltip('播放倍速'));
      await tester.pump(const Duration(milliseconds: 200));
      await tester.tap(find.text('1.5x').last);
      await until(() => player().state.rate == 1.5, 'playback speed');
      final resumePosition = player().state.position;

      repository.failAll = true;
      await tester.tap(find.byTooltip('清晰度'));
      await tester.pump(const Duration(milliseconds: 200));
      await tester.tap(find.text('1080P').last);
      await until(
        () => find.text('暂时无法播放').evaluate().isNotEmpty,
        'bounded retry failure',
      );
      expect(repository.primaryCalls, 3);
      expect(repository.fallbackCalls, 3);
      final attempts = repository.primaryCalls + repository.fallbackCalls;
      await tester.pump(const Duration(seconds: 2));
      expect(repository.primaryCalls + repository.fallbackCalls, attempts);
      expect(repository.activeSessions, isEmpty);

      repository.failAll = false;
      await tester.tap(find.text('重试播放'));
      await until(
        () =>
            repository.fallbackCalls == 4 &&
            player().state.position >= resumePosition &&
            player().state.rate == 1.5 &&
            (player().state.width ?? 0) > 0,
        'manual retry keeps progress and speed',
      );
      expect(find.text('暂时无法播放'), findsNothing);
      expect(tester.takeException(), isNull);
      binding.reportData ??= {};
      binding.reportData!['recovery'] = {
        'primaryCalls': repository.primaryCalls,
        'fallbackCalls': repository.fallbackCalls,
        'resumePositionMs': player().state.position.inMilliseconds,
        'rate': player().state.rate,
      };
      await tester.pumpWidget(const SizedBox.shrink());
      await until(
        () => repository.activeSessions.isEmpty,
        'fallback session cleanup',
      );
    },
    timeout: const Timeout(Duration(minutes: 3)),
  );

  testWidgets(
    'native downloads play MP4, AES HLS and CENC with the source offline',
    (tester) async {
      expect(fixtureBase, startsWith('http://127.0.0.1:'));
      expect(const bool.fromEnvironment('DISABLE_REMOTE_IMAGES'), isTrue);
      MediaKit.ensureInitialized();
      await SystemChrome.setPreferredOrientations([
        DeviceOrientation.portraitUp,
      ]);
      final repository = DownloadFixtureRepository();
      await repository.initialize();
      final store = LocalStore(await SharedPreferences.getInstance());
      final detail = await repository.detail(DeviceFixtureRepository.drama);
      await fixtureControl('online');
      for (final job in await repository.downloads()) {
        if (job.drama.id == detail.drama.id) {
          await repository.controlDownloads('remove', id: job.id);
        }
      }
      final samples = <Map<String, Object?>>[];

      Future<void> until(bool Function() ready, String step) async {
        final timer = Stopwatch()..start();
        while (!ready()) {
          if (timer.elapsed > const Duration(seconds: 40)) {
            fail('Timed out: $step');
          }
          await tester.pump(const Duration(milliseconds: 200));
        }
      }

      Player player() =>
          tester.widget<Video>(find.byType(Video)).controller.player;

      try {
        await tester.pumpWidget(
          DuanjuApp(repository: repository, store: store),
        );
        await until(
          () => find.text('设备播放验证').evaluate().isNotEmpty,
          'download catalog',
        );
        await tester.tap(find.text('设备播放验证'));
        await until(
          () => find.byTooltip('下载选集').evaluate().isNotEmpty,
          'download detail',
        );
        await tester.tap(find.byTooltip('下载选集'));
        await until(
          () => find
              .byKey(const ValueKey('enqueue-downloads'))
              .evaluate()
              .isNotEmpty,
          'episode picker',
        );
        await tester.tap(find.byKey(const ValueKey('enqueue-downloads')));
        await until(
          () => find.text('查看').evaluate().isNotEmpty,
          'enqueue feedback',
        );
        final timer = Stopwatch()..start();
        List<DownloadJob> jobs;
        do {
          jobs = await repository.downloads();
          expect(
            jobs.where((job) => job.state == 'failed'),
            isEmpty,
            reason: jobs.map((job) => job.error).join(', '),
          );
          if (timer.elapsed > const Duration(seconds: 40)) {
            fail('download completion timed out');
          }
          await tester.pump(const Duration(milliseconds: 200));
        } while (jobs.length != 3 || jobs.any((job) => !job.completed));
        expect(await repository.enqueueDownloads(detail, detail.episodes), 0);
        await fixtureControl('offline');
        await tester.pumpWidget(
          MaterialApp(
            theme: ThemeData.dark(),
            home: DownloadsScreen(repository: repository, store: store),
          ),
        );
        await until(
          () => find.text('本地播放').evaluate().isNotEmpty,
          'local playback buttons',
        );
        final first = jobs.firstWhere((job) => job.episode.number == 1);
        await tester.tap(find.byKey(ValueKey('local-play-${first.id}')));
        await until(
          () => find.byType(Video).evaluate().isNotEmpty,
          'offline player',
        );
        await player().setVolume(0);
        for (final number in [1, 2, 3]) {
          if (number != 1) {
            await tester.tap(find.byKey(ValueKey('play-episode-$number')));
          }
          await until(
            () =>
                repository.locallyOpened.isNotEmpty &&
                repository.locallyOpened.last == number &&
                (player().state.width ?? 0) > 0 &&
                player().state.duration.inSeconds >= 18 &&
                player().state.position.inMilliseconds > 500,
            'offline decode $number',
          );
          expect(find.text('暂时无法播放'), findsNothing);
          await player().seek(const Duration(seconds: 6));
          await until(
            () => player().state.position.inMilliseconds >= 6000,
            'offline seek $number',
          );
          samples.add({
            'episode': number,
            'width': player().state.width,
            'height': player().state.height,
            'positionMs': player().state.position.inMilliseconds,
            'durationMs': player().state.duration.inMilliseconds,
          });
        }
        await binding.takeScreenshot('android-offline-playback');
        await tester.tap(
          find.byTooltip(
            MaterialLocalizations.of(
              tester.element(find.byType(Video)),
            ).backButtonTooltip,
          ),
        );
        await until(
          () => find.text('本地播放').evaluate().isNotEmpty,
          'return to downloads',
        );
        await binding.takeScreenshot('android-offline-downloads');
        final library = MediaLibrary(repository, store);
        try {
          final merged = await library.merge(jobs);
          expect(merged.videoTranscodes, 0);
          final probe = await FFmpegExecutor().probe(library.fileFor(merged));
          verifyMediaDuration(probe, 60);
          await library.exportJobs(jobs);
          expect(library.items.where((item) => !item.merged), hasLength(3));
          for (final item in library.items) {
            final inspected = await FFmpegExecutor().probe(
              library.fileFor(item),
            );
            verifyMediaDuration(inspected, item.merged ? 60 : 20);
          }
          binding.reportData ??= {};
          binding.reportData!['localMedia'] = {
            'mergeDuration': probe.duration,
            'videoTranscodes': merged.videoTranscodes,
            'exports': 3,
          };
          for (final item in library.items.toList()) {
            await library.remove(item);
          }
        } finally {
          library.dispose();
        }
        final status = await fixtureControl('status');
        expect(
          status['deniedRequests'],
          0,
          reason: 'offline playback contacted the source',
        );
        final plan = await repository.native.localPlayback(
          detail.drama,
          detail.episodes.first,
        );
        await File(plan!.url).delete();
        await expectLater(
          repository.native.localPlayback(detail.drama, detail.episodes.first),
          throwsA(
            isA<AppFailure>().having(
              (error) => error.code,
              'code',
              'local_media',
            ),
          ),
        );
        binding.reportData ??= {};
        binding.reportData!['offlineDownloads'] = {
          'samples': samples,
          'deniedRequests': status['deniedRequests'],
          'completedCount': jobs.length,
          'missingFileDetected': true,
        };
        expect(tester.takeException(), isNull);
      } finally {
        await tester.pumpWidget(const SizedBox.shrink());
        await tester.pump(const Duration(milliseconds: 400));
        await fixtureControl('online');
        for (final job in await repository.downloads()) {
          if (job.drama.id == detail.drama.id) {
            await repository.controlDownloads('remove', id: job.id);
          }
        }
        store.dispose();
      }
    },
    timeout: const Timeout(Duration(minutes: 4)),
  );

  testWidgets(
    'Android foreground service continues a slow download after minimizing',
    (tester) async {
      final repository = DownloadFixtureRepository();
      await repository.initialize();
      final preferences = await SharedPreferences.getInstance();
      await preferences.setBool('autoExport', false);
      final store = LocalStore(preferences);
      const drama = Drama(
        id: 'hongguo:700009',
        source: 'hongguo',
        title: '后台下载合成验证',
        episodes: 1,
      );
      final episode = Episode({
        'id': 'slow',
        'currentEpisode': 1,
        'videoUrl': '$fixtureBase/clear.mp4?slow=1',
        'referer': '$fixtureBase/',
      }, 1);
      await fixtureControl('online');
      for (final job in await repository.downloads()) {
        if (job.drama.id == drama.id) {
          await repository.controlDownloads('remove', id: job.id);
        }
      }
      try {
        await tester.pumpWidget(
          MaterialApp(
            home: DownloadsScreen(repository: repository, store: store),
          ),
        );
        await repository.enqueueDownloads(DramaDetail(drama, [episode]), [
          episode,
        ]);
        expect(await FlutterForegroundTask.isRunningService, isTrue);
        final before = (await repository.downloads())
            .firstWhere((job) => job.drama.id == drama.id)
            .bytes;
        FlutterForegroundTask.minimizeApp();
        await Future<void>.delayed(const Duration(seconds: 4));
        final after = (await repository.downloads()).firstWhere(
          (job) => job.drama.id == drama.id,
        );
        expect(after.bytes, greaterThan(before));
        expect(after.state, isNot('failed'), reason: after.error);
        expect(await FlutterForegroundTask.isRunningService, isTrue);
        FlutterForegroundTask.launchApp();
        await tester.pump(const Duration(seconds: 1));
        await repository.controlDownloads('pause', id: after.id);
        final paused = (await repository.downloads()).firstWhere(
          (job) => job.id == after.id,
        );
        expect(paused.state, anyOf('paused', 'completed'));
        binding.reportData ??= {};
        binding.reportData!['backgroundDownload'] = {
          'before': before,
          'after': after.bytes,
          'serviceRunning': true,
        };
      } finally {
        FlutterForegroundTask.launchApp();
        await tester.pumpWidget(const SizedBox.shrink());
        for (final job in await repository.downloads()) {
          if (job.drama.id == drama.id) {
            await repository.controlDownloads('remove', id: job.id);
          }
        }
        store.dispose();
      }
    },
    timeout: const Timeout(Duration(minutes: 2)),
  );
}
