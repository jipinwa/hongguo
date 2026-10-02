import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/models.dart';

class FixtureRepository extends AppRepository {
  int detailCalls = 0;
  bool fail = false;
  final requests = <String>[];
  final pages = <int>[];
  final forced = <bool>[];
  final cachedPages = <String, CatalogPage>{};
  static const free = Drama(
    id: 'hongguo:100',
    source: 'hongguo',
    title: '测试短剧',
    episodes: 2,
    category: '合成数据',
  );
  static const vip = Drama(
    id: 'huangdou:200',
    source: 'huangdou',
    title: '会员测试剧',
    episodes: 4,
    vip: true,
  );
  @override
  Future<void> initialize() async {}
  @override
  Future<CatalogPage> cached(String source, {String category = ''}) async =>
      cachedPages[source] ?? CatalogPage([]);
  @override
  Future<String> cover(Drama drama, {bool force = false}) async =>
      throw AppFailure('合成剧集没有远程海报');
  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async {
    requests.add(source);
    pages.add(page);
    forced.add(force);
    if (fail) {
      throw AppFailure('合成网络错误');
    }
    return CatalogPage(
      [
        free,
        vip,
        Drama(
          id: '$source:auto-$page',
          source: source,
          title: '第$page页短剧',
          episodes: 1,
          category: '合成数据',
        ),
      ],
      page: page,
      hasMore: page < 5,
    );
  }

  @override
  Future<DramaDetail> detail(Drama drama) async {
    detailCalls++;
    return DramaDetail(drama, [
      Episode({'id': '1', 'title': '第1集', 'currentEpisode': 1}, 1),
      Episode({'id': '2', 'title': '第2集', 'currentEpisode': 2}, 2),
    ]);
  }

  @override
  Future<PlaybackPlan> resolve(
    Drama drama,
    Episode episode, {
    int quality = 0,
  }) async => const PlaybackPlan(url: 'https://example.test/synthetic.mp4');
  @override
  Future<PlaybackPlan> fallback(PlaybackPlan current) async =>
      throw AppFailure('没有备用线路');
  @override
  Future<void> cancelPlayback() async {}
  @override
  Future<void> release(String session) async {}
}
