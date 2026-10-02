import 'dart:convert';

import 'package:duanju_app/follow_state.dart';
import 'package:duanju_app/local_snapshot.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:shared_preferences_platform_interface/shared_preferences_platform_interface.dart';

class _RejectingStore extends InMemorySharedPreferencesStore {
  _RejectingStore() : super.empty();
  bool reject = false;

  @override
  Future<bool> setValue(String valueType, String key, Object value) =>
      reject && key.endsWith(LocalSnapshot.storageKey)
      ? Future.value(false)
      : super.setValue(valueType, key, value);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  Drama drama(int episodes) => Drama(
    id: 'hongguo:follow',
    source: 'hongguo',
    title: '合成追剧',
    episodes: episodes,
  );
  WatchEntry watch({int episode = 2, double position = 12}) => WatchEntry(
    drama: drama(10),
    episode: episode,
    position: position,
    duration: 60,
    updatedAt: DateTime.utc(2026, 9, 21, 12),
  );
  const seasonOne = Drama(
    id: 'hongguo:season-one',
    source: 'hongguo',
    title: '合成系列 第一季',
    episodes: 12,
  );
  const seasonTwo = Drama(
    id: 'hongguo:season-two',
    source: 'hongguo',
    title: '合成系列 第二季',
    episodes: 8,
  );
  const plainSeasonOne = Drama(
    id: 'hongguo:plain-one',
    source: 'hongguo',
    title: '无后缀系列',
    episodes: 10,
  );
  const plainSeasonTwo = Drama(
    id: 'hongguo:plain-two',
    source: 'hongguo',
    title: '无后缀系列 第2季',
    episodes: 10,
  );
  Future<LocalStore> create([Map<String, Object> values = const {}]) async {
    SharedPreferences.setMockInitialValues(values);
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    return store;
  }

  test(
    'manual watched remains separate from progress and history deletion',
    () async {
      final store = await create();
      await store.saveWatch(watch());
      await store.setFollowStatus(drama(10), FollowStatus.watched);
      expect(store.watched('hongguo:follow')!.toJson(), watch().toJson());
      await store.saveWatch(watch(episode: 3, position: 24));
      expect(store.following('hongguo:follow')!.manuallyWatched, isTrue);
      expect(store.watched('hongguo:follow')!.position, 24);
      await store.removeHistory('hongguo:follow');
      expect(store.history, isEmpty);
      expect(store.following('hongguo:follow')!.status, FollowStatus.watched);
      final restarted = LocalStore(store.preferences);
      addTearDown(restarted.dispose);
      expect(restarted.following('hongguo:follow')!.manuallyWatched, isTrue);
      expect(restarted.history, isEmpty);
    },
  );

  test(
    'episode notices keep their read baseline across refresh and restart',
    () async {
      final store = await create();
      await store.toggleFavorite(drama(0));
      expect(store.following('hongguo:follow')!.readEpisodes, isNull);
      await store.refreshDrama(drama(10));
      expect(store.following('hongguo:follow')!.newEpisodes, 0);
      await store.refreshDrama(drama(12));
      expect(store.following('hongguo:follow')!.newEpisodes, 2);
      await store.refreshDrama(drama(11));
      await store.refreshDrama(drama(0));
      expect(store.following('hongguo:follow')!.newEpisodes, 2);
      await store.markUpdatesRead('hongguo:follow');
      expect(store.following('hongguo:follow')!.newEpisodes, 0);
      await store.refreshDrama(drama(14));
      final restarted = LocalStore(store.preferences);
      addTearDown(restarted.dispose);
      expect(restarted.following('hongguo:follow')!.readEpisodes, 12);
      expect(restarted.following('hongguo:follow')!.newEpisodes, 2);
      expect(restarted.history, isEmpty);
    },
  );

  test(
    'hongguo series season notices are local, persistent and explicitly read',
    () async {
      final store = await create();
      await store.toggleFavorite(seasonOne);
      await store.refreshDramas([seasonOne, seasonTwo]);
      final state = store.following(seasonOne.id)!;
      expect(state.newEpisodes, 0);
      expect(state.newSeasons, 1);
      expect(state.updateLabel, '新季 1 部');
      expect(state.seriesSeasons[seasonTwo.id]!.title, seasonTwo.title);
      expect(store.seriesDramasFor(seasonOne).map((drama) => drama.id), [
        seasonOne.id,
        seasonTwo.id,
      ]);
      final restarted = LocalStore(store.preferences);
      addTearDown(restarted.dispose);
      expect(restarted.following(seasonOne.id)!.newSeasons, 1);
      await restarted.markSeriesSeasonRead(seasonOne.id, seasonTwo.id);
      expect(restarted.following(seasonOne.id)!.newSeasons, 0);
      await restarted.refreshDramas([seasonTwo]);
      expect(restarted.following(seasonOne.id)!.newSeasons, 0);
      await restarted.refreshDramas([
        const Drama(
          id: 'huangdou:season-three',
          source: 'huangdou',
          title: '合成系列 第三季',
        ),
      ]);
      expect(restarted.following(seasonOne.id)!.seriesSeasons.keys, [
        seasonTwo.id,
      ]);
    },
  );

  test(
    'hongguo plain first season can discover an explicit later season',
    () async {
      final store = await create();
      await store.toggleFavorite(plainSeasonOne);
      await store.refreshDramas([plainSeasonTwo]);
      expect(store.following(plainSeasonOne.id)!.newSeasons, 1);
      expect(store.seriesDramasFor(plainSeasonOne).map((drama) => drama.id), [
        plainSeasonOne.id,
        plainSeasonTwo.id,
      ]);
    },
  );

  test('hongguo series candidates include refreshed catalog dramas', () async {
    final store = await create();
    await store.refreshDramas([plainSeasonOne, plainSeasonTwo]);
    expect(store.seriesDramasFor(plainSeasonOne).map((drama) => drama.id), [
      plainSeasonOne.id,
      plainSeasonTwo.id,
    ]);
    final restarted = LocalStore(store.preferences);
    addTearDown(restarted.dispose);
    expect(restarted.seriesDramasFor(plainSeasonOne).map((drama) => drama.id), [
      plainSeasonOne.id,
      plainSeasonTwo.id,
    ]);
  });

  test(
    'actual playback advances automatic status and new episodes reopen it',
    () async {
      final store = await create();
      await store.toggleFavorite(drama(10));
      await store.saveWatch(watch(episode: 1, position: 0));
      expect(store.following('hongguo:follow')!.status, FollowStatus.planned);
      await store.saveWatch(watch());
      expect(store.following('hongguo:follow')!.status, FollowStatus.watching);
      await store.saveWatch(watch(episode: 10, position: 60));
      expect(store.following('hongguo:follow')!.status, FollowStatus.watched);
      expect(store.following('hongguo:follow')!.manuallyWatched, isFalse);
      final updatedAt = store.history.single.updatedAt;
      await store.refreshDrama(drama(11));
      expect(store.following('hongguo:follow')!.status, FollowStatus.watching);
      expect(store.history.single.updatedAt, updatedAt);
      await store.setFollowStatus(drama(11), FollowStatus.watched);
      await store.refreshDrama(drama(12));
      expect(store.following('hongguo:follow')!.status, FollowStatus.watched);
      expect(store.following('hongguo:follow')!.newEpisodes, 1);
    },
  );

  test(
    'older favorites are readable without rewriting the original snapshot',
    () async {
      final favorites = jsonEncode([drama(10).toJson()]);
      final store = await create({
        'favorites': favorites,
        'history': jsonEncode([watch().toJson()]),
      });
      expect(store.following('hongguo:follow')!.status, FollowStatus.watching);
      expect(store.following('hongguo:follow')!.newEpisodes, 0);
      await store.refreshDrama(drama(10));
      expect(store.preferences.getString('favorites'), favorites);
      expect(store.preferences.containsKey(LocalSnapshot.storageKey), isFalse);
    },
  );

  test(
    'backup restores read baselines and accepts older backups without states',
    () async {
      final store = await create({'plugin_private': 'preserve'});
      await store.saveWatch(watch());
      await store.setFollowStatus(drama(10), FollowStatus.watched);
      await store.refreshDrama(drama(13));
      final backup = await store.exportBackup();
      await store.toggleFavorite(drama(10));
      await store.clearHistory();
      await store.importBackup(backup);
      expect(store.following('hongguo:follow')!.manuallyWatched, isTrue);
      expect(store.following('hongguo:follow')!.newEpisodes, 3);
      expect(store.history.single.position, 12);
      expect(store.preferences.getString('plugin_private'), 'preserve');
      final older = jsonDecode(backup) as Map<String, dynamic>;
      ((older['libraries'] as Map)['default'] as Map).remove('followStates');
      await store.importBackup(jsonEncode(older));
      expect(store.following('hongguo:follow')!.manuallyWatched, isFalse);
      expect(store.following('hongguo:follow')!.status, FollowStatus.watching);
      expect(store.history.single.position, 12);
    },
  );

  test('malformed state backup cannot change existing records', () async {
    final store = await create();
    await store.toggleFavorite(drama(10));
    final before = await store.exportBackup();
    final invalid = jsonDecode(before) as Map<String, dynamic>;
    final states =
        ((invalid['libraries'] as Map)['default'] as Map)['followStates']
            as Map;
    (states['hongguo:follow'] as Map)['readEpisodes'] = 100;
    await expectLater(
      store.importBackup(jsonEncode(invalid)),
      throwsFormatException,
    );
    expect(await store.exportBackup(), before);
  });

  test('follow states and removal are isolated between local users', () async {
    final store = await create();
    await store.setFollowStatus(drama(10), FollowStatus.watched);
    await store.saveWatch(watch());
    await store.saveProfile(
      id: 'default',
      name: '管理员',
      sources: [],
      download: true,
      pin: 'abcdef12',
    );
    await store.saveProfile(name: '访客', sources: ['hongguo'], download: false);
    final guest = store.profiles.firstWhere((profile) => !profile.admin);
    await store.switchProfile(guest.id);
    expect(store.following('hongguo:follow'), isNull);
    await store.setFollowStatus(drama(5), FollowStatus.planned);
    await store.refreshDrama(drama(7));
    await store.removeHistory('hongguo:follow');
    expect(store.following('hongguo:follow')!.newEpisodes, 2);
    await store.switchProfile('default', pin: 'abcdef12');
    expect(store.following('hongguo:follow')!.manuallyWatched, isTrue);
    expect(store.following('hongguo:follow')!.newEpisodes, 0);
    expect(store.history.single.position, 12);
  });

  test('failed writes preserve both follow state and real progress', () async {
    SharedPreferences.resetStatic();
    final platform = _RejectingStore();
    SharedPreferencesStorePlatform.instance = platform;
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.toggleFavorite(drama(10));
    await store.saveWatch(watch());
    final before = await store.exportBackup();
    platform.reject = true;
    for (final change in <Future<void> Function()>[
      () => store.setFollowStatus(drama(10), FollowStatus.watched),
      () => store.refreshDrama(drama(12)),
      () => store.removeHistory('hongguo:follow'),
    ]) {
      await expectLater(change(), throwsStateError);
      expect(await store.exportBackup(), before);
    }
    await store.preferences.reload();
    final restarted = LocalStore(store.preferences);
    addTearDown(restarted.dispose);
    expect(await restarted.exportBackup(), before);
  });

  test(
    'continue respects actual episode numbers and never skips unfinished progress',
    () {
      final episodes = [
        for (final number in [1, 3, 5])
          Episode({'currentEpisode': number}, number),
      ];
      expect(resumeEpisodeIndex(episodes, watch(episode: 3, position: 12)), 1);
      expect(resumeEpisodeIndex(episodes, watch(episode: 3, position: 60)), 2);
      expect(resumeEpisodeIndex(episodes, watch(episode: 2)), 1);
      expect(resumeEpisodeIndex(episodes, watch(episode: 5, position: 60)), 2);
    },
  );
}
