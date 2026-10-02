import 'dart:async';

import 'package:duanju_app/app_build.dart';
import 'package:duanju_app/catalog_browser.dart';
import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/main.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/ranking_models.dart';
import 'package:duanju_app/widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class CategoryRepository extends FixtureRepository {
  final categoryRequests = <String>[];
  Completer<CatalogPage>? pendingComic;
  bool failLegacy = false;
  bool paginate = false;
  final rankRequests = <String>[];

  @override
  Future<CatalogPage> cached(String source, {String category = ''}) async =>
      category.isEmpty
      ? cachedPages[source] ?? CatalogPage([])
      : CatalogPage([]);

  @override
  Future<List<CatalogCategory>> categories(
    String source, {
    bool force = false,
  }) async => switch (source) {
    'hongguo' => const [
      CatalogCategory.all,
      CatalogCategory('short_play', '真人剧'),
      CatalogCategory('comic_series', '漫剧'),
      CatalogCategory('ai_series', 'AI 剧'),
    ],
    'huangguo-video' => const [CatalogCategory.all, CatalogCategory('2', '短片')],
    'huangguoai' => const [
      CatalogCategory.all,
      CatalogCategory('ai-duanju', 'AI 短剧'),
      CatalogCategory('ai-manju', 'AI 漫剧'),
    ],
    'cloudfront' => const [
      CatalogCategory.all,
      CatalogCategory('old-short', 'AI成人短剧'),
    ],
    _ => const [CatalogCategory.all],
  };

  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async {
    categoryRequests.add('$source|$category|$page');
    if (source == 'cloudfront' && failLegacy) throw AppFailure('合成入口失败');
    if (category == 'ai-manju' && pendingComic != null) {
      return pendingComic!.future;
    }
    return CatalogPage(
      [
        Drama(
          id: '$source:$category:$page',
          source: source,
          title: '$source · ${category.isEmpty ? '全部' : category}',
          category: source == 'hongguo' ? '异能' : '',
        ),
      ],
      page: page,
      hasMore: paginate && source == 'huangguo-video' && page == 1,
    );
  }

  @override
  Future<List<RankingBoard>> rankingBoards() async => const [
    RankingBoard(id: 'hongguo-hot', source: 'hongguo', name: '总热播榜'),
    RankingBoard(id: 'hongguo-real', source: 'hongguo', name: '真人剧榜'),
  ];

  @override
  Future<RankingPage> rankings(
    String board, {
    int page = 1,
    bool force = false,
  }) async {
    rankRequests.add('$board|$page|$force');
    return RankingPage(
      items: [
        RankingItem(
          (page - 1) * 20 + 1,
          Drama(
            id: 'hongguo:rank:$board:$page',
            source: 'hongguo',
            title: '合成榜单第$page页',
          ),
        ),
      ],
      page: page,
      hasMore: page == 1,
    );
  }
}

void main() {
  Future<LocalStore> mount(
    WidgetTester tester,
    CategoryRepository repository, {
    String source = 'hongguo',
    double width = 390,
    double scale = 1,
  }) async {
    tester.view.physicalSize = Size(width, 844);
    tester.view.devicePixelRatio = 1;
    tester.platformDispatcher.textScaleFactorTestValue = scale;
    addTearDown(tester.view.reset);
    addTearDown(tester.platformDispatcher.clearTextScaleFactorTestValue);
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    await store.setSource(source);
    await tester.pumpWidget(DuanjuApp(repository: repository, store: store));
    await tester.pumpAndSettle();
    return store;
  }

  for (final width in [390.0, 320.0]) {
    testWidgets('compact home keeps search collapsed at $width', (
      tester,
    ) async {
      await mount(
        tester,
        CategoryRepository(),
        width: width,
        scale: width == 320 ? 2 : 1,
      );
      expect(find.byType(TextField), findsNothing);
      final coverTop = tester.getTopLeft(find.byType(DramaCover).first).dy;
      expect(coverTop, lessThan(150));
      final switcher = find.byKey(const ValueKey('source-switch'));
      final rankings = find.byKey(const ValueKey('open-rankings'));
      expect(
        tester.getCenter(switcher).dy,
        closeTo(tester.getCenter(rankings).dy, 2),
      );
      await tester.tap(find.byKey(const ValueKey('toggle-search')));
      await tester.pumpAndSettle();
      expect(find.byType(TextField), findsOneWidget);
      await tester.tap(find.byKey(const ValueKey('toggle-search')));
      await tester.pumpAndSettle();
      expect(find.byType(TextField), findsNothing);
      expect(tester.getTopLeft(find.byType(DramaCover).first).dy, coverTop);
      expect(tester.takeException(), isNull);
    });
  }

  testWidgets(
    'one Huangguo source merges catalogs and categories and rejects stale replies',
    (tester) async {
      final repository = CategoryRepository();
      await mount(tester, repository, source: 'huangguoai');
      expect(find.text('黄果'), findsOneWidget);
      expect(find.text('入口'), findsNothing);
      expect(find.text('旧版'), findsNothing);
      expect(
        repository.categoryRequests.toSet(),
        containsAll(['huangguo-video||1', 'huangguoai||1', 'cloudfront||1']),
      );
      expect(find.text('AI成人短剧'), findsNothing);
      expect(find.text('AI 短剧'), findsOneWidget);
      Future<void> choose(String name) async {
        final chip = find.widgetWithText(ChoiceChip, name);
        await tester.ensureVisible(chip);
        await tester.tap(chip);
        await tester.pump();
        await tester.pump(const Duration(milliseconds: 250));
      }

      final pending = Completer<CatalogPage>();
      repository.pendingComic = pending;
      await choose('AI 漫剧');
      await choose('AI 短剧');
      await tester.pumpAndSettle();
      pending.complete(
        CatalogPage(const [
          Drama(id: 'huangguoai:stale', source: 'huangguoai', title: '过期分类结果'),
        ]),
      );
      await tester.pumpAndSettle();
      expect(find.text('过期分类结果'), findsNothing);
      expect(
        repository.categoryRequests,
        containsAll(['huangguoai|ai-duanju|1', 'cloudfront|old-short|1']),
      );
      expect(find.text('huangguoai · ai-duanju'), findsOneWidget);
      expect(find.text('cloudfront · old-short'), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
    skip: !allSourcesEnabled,
  );

  test(
    'remote content types never hide fine categories from cached pages',
    () async {
      final repository = CategoryRepository();
      repository.cachedPages['hongguo'] = CatalogPage(const [
        Drama(
          id: 'hongguo:old1',
          source: 'hongguo',
          title: '缓存剧甲',
          category: '都市',
        ),
        Drama(
          id: 'hongguo:old2',
          source: 'hongguo',
          title: '缓存剧乙',
          category: '成长',
        ),
      ], page: 7);
      final browser = CatalogBrowser(repository);
      const group = SourceGroup('hongguo', '红果', [SourceSite.hongguo]);
      await browser.loadCategories(group);
      await browser.load(group, category: 'category:漫剧');
      expect(
        browser.categories(group).map((category) => category.name),
        containsAll(['真人剧', '漫剧', 'AI 剧', '都市', '成长', '异能']),
      );
      final local = await browser.load(group, category: 'local:都市');
      expect(local.items.map((drama) => drama.id), contains('hongguo:old1'));
    },
  );

  test(
    'group pagination retries failed member without skipping or reloading exhausted members',
    () async {
      final repository = CategoryRepository()
        ..failLegacy = true
        ..paginate = true;
      final browser = CatalogBrowser(repository);
      final group = SourceGroup.fromSources(
        SourceSite.knownValues,
      ).firstWhere((group) => group.id == 'huangguo');
      final first = await browser.load(group);
      expect(first.items.length, 2);
      expect(first.warning, '合成入口失败');
      repository.failLegacy = false;
      final next = await browser.load(group, more: true);
      expect(
        repository.categoryRequests
            .where((request) => request == 'cloudfront||1')
            .length,
        2,
      );
      expect(repository.categoryRequests, contains('huangguo-video||2'));
      expect(repository.categoryRequests, isNot(contains('huangguoai||2')));
      expect(next.items.length, 4);
      expect(next.warning, isEmpty);
    },
  );

  testWidgets(
    'rankings are separate from categories and preserve upstream rank numbers',
    (tester) async {
      final repository = CategoryRepository();
      await mount(tester, repository);
      await tester.tap(find.byKey(const ValueKey('open-rankings')));
      await tester.pumpAndSettle();
      expect(repository.rankRequests, ['hongguo-hot|1|false']);
      expect(find.text('总热播榜'), findsOneWidget);
      await tester.tap(find.text('加载更多'));
      await tester.pumpAndSettle();
      expect(
        find.byKey(const ValueKey('rank-21-hongguo:rank:hongguo-hot:2')),
        findsOneWidget,
      );
      await tester.tap(find.text('真人剧榜'));
      await tester.pumpAndSettle();
      expect(repository.rankRequests.last, 'hongguo-real|1|false');
      expect(
        find.byKey(const ValueKey('rank-21-hongguo:rank:hongguo-hot:2')),
        findsNothing,
      );
      expect(repository.categoryRequests, ['hongguo||1']);
      expect(tester.takeException(), isNull);
    },
  );
}
