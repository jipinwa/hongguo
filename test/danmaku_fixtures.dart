import 'dart:async';
import 'dart:math' as math;

import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/danmaku_models.dart';
import 'package:duanju_app/models.dart';

import 'player_fixtures.dart';

class DanmakuRequest {
  DanmakuRequest(this.plan, this.startMs, this.durationMs);
  final PlaybackPlan plan;
  final int startMs;
  final int durationMs;
  final result = Completer<DanmakuPage>();
}

class DanmakuFixtureRepository extends RouteRepository {
  final danmakuCalls = <DanmakuRequest>[];
  bool holdDanmaku = false;
  bool failDanmaku = false;
  bool local = false;
  int danmakuCancellations = 0;
  DanmakuPage Function(DanmakuRequest)? response;

  @override
  PlaybackPlan plan(bool alternate) {
    final base = super.plan(alternate);
    return PlaybackPlan(
      url: base.url,
      session: base.session,
      local: local,
      danmakuId: '${1000 + requestedEpisodes.last}',
      quality: base.quality,
      qualities: base.qualities,
      routeIndex: base.routeIndex,
      routeCount: base.routeCount,
    );
  }

  static DanmakuPage page(DanmakuRequest request) => DanmakuPage(
    episodeId: request.plan.danmakuId,
    startMs: request.startMs,
    nextMs: math.min(request.startMs + danmakuWindowMs, request.durationMs),
    items: [
      DanmakuItem(
        id: '${request.plan.danmakuId}-${request.startMs}',
        text: '合成弹幕 ${request.plan.danmakuId}',
        timeMs: request.startMs,
      ),
    ],
  );

  @override
  Future<DanmakuPage> danmaku(
    PlaybackPlan plan, {
    required int startMs,
    required int durationMs,
  }) async {
    final request = DanmakuRequest(plan, startMs, durationMs);
    danmakuCalls.add(request);
    if (holdDanmaku) return request.result.future;
    if (failDanmaku) throw AppFailure('合成弹幕失败');
    return response?.call(request) ?? page(request);
  }

  @override
  Future<void> cancelDanmaku() async {
    danmakuCancellations++;
  }
}
