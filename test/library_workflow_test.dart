import 'dart:async';

import 'package:duanju_app/batch_downloads.dart';
import 'package:duanju_app/catalog_browser.dart';
import 'package:duanju_app/library_updater.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/source_status.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'library_feature_fixtures.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const first = LibraryFeatureRepository.first;
  const second = LibraryFeatureRepository.second;

  Future<LocalStore> create() async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    return store;
  }

  test(
    'unified updates share source tasks and deliver each finished cache once',
    () async {
      final store = await create();
      final repository = LibraryFeatureRepository()
        ..pendingStart = Completer<SourceStatus>();
      final changed = <String>[];
      final updater = LibraryUpdater(
        repository,
        store,
        onCatalogChanged: changed.add,
      );
      addTearDown(updater.dispose);
      final sources = [SourceSite.byId('hongguo')];
      final pending = updater.update(sources);
      await updater.update(sources);
      expect(repository.starts, ['hongguo:update']);
      expect(updater.busy('hongguo'), isTrue);
      repository.pendingStart!.complete(repository.running('hongguo'));
      await pending;
      await updater.update(sources);
      expect(repository.starts, hasLength(1));
      repository.statuses['hongguo'] = repository.finished('hongguo');
      await updater.refresh();
      await updater.refresh();
      expect(changed, ['hongguo']);
      expect(updater.busy('hongguo'), isFalse);
      expect(repository.requests, isEmpty);
    },
  );

  test('stopping during submission prevents queued source starts', () async {
    final store = await create();
    final repository = LibraryFeatureRepository()
      ..pendingStart = Completer<SourceStatus>();
    final updater = LibraryUpdater(repository, store);
    addTearDown(updater.dispose);
    final pending = updater.update(store.sources);
    await updater.stop(store.sources);
    repository.pendingStart!.complete(repository.running('hongguo'));
    await pending;
    expect(repository.starts, ['hongguo:update']);
    expect(repository.stops, ['hongguo']);
    expect(store.sources.any((source) => updater.busy(source.id)), isFalse);
  });

  test(
    'update scope respects compiled sources and keeps other starts after a failure',
    () async {
      final store = await create();
      final repository = LibraryFeatureRepository()
        ..startFailures.add('hongguo');
      final updater = LibraryUpdater(repository, store);
      addTearDown(updater.dispose);
      await updater.update(SourceSite.knownValues);
      expect(
        repository.starts,
        store.sources.map((source) => '${source.id}:update').toList(),
      );
      expect(updater.error('hongguo'), contains('合成站源启动失败'));
      for (final source in store.sources.where(
        (source) => source.id != 'hongguo',
      )) {
        expect(updater.busy(source.id), isTrue);
      }
    },
  );

  test(
    'a late update response cannot attach to a changed user session',
    () async {
      final store = await create();
      final repository = LibraryFeatureRepository()
        ..pendingStart = Completer<SourceStatus>();
      final changed = <String>[];
      final updater = LibraryUpdater(
        repository,
        store,
        onCatalogChanged: changed.add,
      );
      addTearDown(updater.dispose);
      final pending = updater.update(store.sources);
      await store.switchProfile('default');
      repository.pendingStart!.complete(repository.finished('hongguo'));
      await pending;
      expect(changed, isEmpty);
      expect(updater.status('hongguo'), isNull);
      expect(repository.starts, hasLength(1));
    },
  );

  test(
    'cache refresh uses saved pagination even when stale and never requests a head page',
    () async {
      final repository = LibraryFeatureRepository();
      final browser = CatalogBrowser(repository);
      final group = SourceGroup('hongguo', '红果', [SourceSite.byId('hongguo')]);
      repository.cachedPages['hongguo'] = CatalogPage(
        [first],
        page: 7,
        hasMore: true,
        fresh: false,
      );
      final page = await browser.load(group, cacheOnly: true);
      expect(page.page, 7);
      expect(page.items.single.id, first.id);
      expect(repository.requests, isEmpty);
      await browser.load(group, more: true);
      expect(repository.pages, [8]);
    },
  );

  test(
    'batch preview excludes VIP, skips duplicates and can recover failed details',
    () async {
      final store = await create();
      final repository = LibraryFeatureRepository()
        ..details[first.id] = LibraryFeatureRepository.makeDetail(
          first,
          504,
          vipFrom: 504,
        )
        ..detailFailures.add(second.id)
        ..queued.add('${first.id}:1');
      final batch = BatchDownloads(repository, store, [first, first, second]);
      addTearDown(batch.dispose);
      await batch.prepare();
      expect(batch.items, hasLength(2));
      expect(batch.pendingEpisodes, 503);
      expect(batch.failures, 1);
      expect(repository.enqueues, isEmpty);
      batch.setQuality(720);
      await batch.submit();
      expect(batch.added, 502);
      expect(batch.existing, 1);
      expect(repository.enqueues.map((call) => call.$2.length), [500, 3]);
      expect(repository.enqueues.every((call) => call.$3 == 720), isTrue);
      expect(repository.queued, isNot(contains('${first.id}:504')));
      repository.detailFailures.clear();
      await batch.prepare();
      await batch.submit();
      expect(batch.added, 504);
      expect(batch.pendingEpisodes, 0);
      expect(repository.detailRequests, [first.id, second.id, second.id]);
      expect(repository.enqueues.map((call) => call.$2.length), [500, 3, 2]);
    },
  );

  test(
    'a failed chunk resumes without resubmitting accepted episodes',
    () async {
      final store = await create();
      final repository = LibraryFeatureRepository()
        ..details[first.id] = LibraryFeatureRepository.makeDetail(first, 503)
        ..failChunkStartingAt = 501;
      final batch = BatchDownloads(repository, store, [first]);
      addTearDown(batch.dispose);
      await batch.prepare();
      await batch.submit();
      expect(batch.added, 500);
      expect(batch.pendingEpisodes, 3);
      expect(batch.failures, 1);
      await batch.submit();
      expect(batch.added, 503);
      expect(batch.failures, 0);
      expect(repository.enqueues.map((call) => call.$2.first), [1, 501, 501]);
    },
  );

  test(
    'stopping batch preparation keeps the current result and leaves later dramas untouched',
    () async {
      final store = await create();
      final repository = LibraryFeatureRepository()
        ..pendingDetail = Completer<DramaDetail>();
      final batch = BatchDownloads(repository, store, [first, second]);
      addTearDown(batch.dispose);
      final pending = batch.prepare();
      batch.stop();
      repository.pendingDetail!.complete(repository.details[first.id]);
      await pending;
      expect(repository.detailRequests, [first.id]);
      expect(batch.unread, 1);
      expect(repository.enqueues, isEmpty);
      repository.pendingDetail = null;
      await batch.prepare();
      expect(repository.detailRequests, [first.id, second.id]);
    },
  );

  test(
    'stopping batch addition leaves accepted jobs and submits no later drama',
    () async {
      final store = await create();
      final repository = LibraryFeatureRepository()
        ..pendingEnqueue = Completer<void>();
      final batch = BatchDownloads(repository, store, [first, second]);
      addTearDown(batch.dispose);
      await batch.prepare();
      final pending = batch.submit();
      batch.stop();
      repository.pendingEnqueue!.complete();
      await pending;
      expect(batch.added, 2);
      expect(batch.pendingEpisodes, 2);
      expect(repository.enqueues, hasLength(1));
      repository.pendingEnqueue = null;
      await batch.submit();
      expect(batch.added, 4);
      expect(repository.enqueues.map((call) => call.$1), [first.id, second.id]);
    },
  );

  test(
    'changing users during preparation prevents all later reads and queue writes',
    () async {
      final store = await create();
      final repository = LibraryFeatureRepository()
        ..pendingDetail = Completer<DramaDetail>();
      final batch = BatchDownloads(repository, store, [first, second]);
      addTearDown(batch.dispose);
      final pending = batch.prepare();
      await store.switchProfile('default');
      repository.pendingDetail!.complete(repository.details[first.id]);
      await pending;
      await batch.submit();
      expect(batch.hasAccess, isFalse);
      expect(repository.detailRequests, [first.id]);
      expect(repository.enqueues, isEmpty);
    },
  );

  test(
    'VIP-only batch requires explicit inclusion before it can be added',
    () async {
      final store = await create();
      final repository = LibraryFeatureRepository()
        ..details[first.id] = LibraryFeatureRepository.makeDetail(
          first,
          2,
          vipFrom: 1,
        );
      final batch = BatchDownloads(repository, store, [first]);
      addTearDown(batch.dispose);
      await batch.prepare();
      await batch.submit();
      expect(repository.enqueues, isEmpty);
      batch.setIncludeVip(true);
      await batch.submit();
      expect(batch.added, 2);
    },
  );
}
