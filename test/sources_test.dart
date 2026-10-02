import 'dart:async';
import 'dart:convert';

import 'package:duanju_app/local_profiles.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/source_status.dart';
import 'package:duanju_app/sources_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class SourceFixtureRepository extends FixtureRepository {
  final statuses = <String, SourceStatus>{};
  final operations = <String>[];
  final statusRequests = <String>[];
  Completer<SourceStatus>? pending;

  @override
  bool get supportsSourceManagement => true;

  @override
  Future<SourceStatus> sourceStatus(String source) async {
    statusRequests.add(source);
    return statuses[source] ??
        SourceStatus.fromJson({
          'source': source,
          'count': 3,
          'page': 2,
          'hasMore': true,
        });
  }

  @override
  Future<SourceStatus> startSourceJob(
    String source,
    String operation, {
    Drama? drama,
  }) async {
    operations.add('$source:$operation');
    if (pending != null) return pending!.future;
    return statuses[source] = SourceStatus.fromJson({
      'source': source,
      'count': 3,
      'running': true,
      'operation': operation,
      'stage': '查找新剧',
    });
  }

  @override
  Future<SourceStatus> cancelSourceJob(String source) async {
    operations.add('$source:cancel');
    return statuses[source] = SourceStatus.fromJson({
      'source': source,
      'count': 3,
      'running': false,
      'stage': '已停止',
    });
  }
}

void main() {
  testWidgets('source list keeps bottom content above system navigation', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    tester.view.padding = const FakeViewPadding(bottom: 34);
    addTearDown(tester.view.reset);
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    await tester.pumpWidget(
      MaterialApp(
        home: SourcesScreen(
          repository: SourceFixtureRepository(),
          store: store,
        ),
      ),
    );
    await tester.pumpAndSettle();
    final list = tester.widget<ListView>(find.byType(ListView));
    expect((list.padding! as EdgeInsets).bottom, 50);
    await tester.pumpWidget(const SizedBox.shrink());
    store.dispose();
  });

  testWidgets(
    'source update prevents duplicate submits and can stop without losing cache',
    (tester) async {
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = SourceFixtureRepository()
        ..pending = Completer<SourceStatus>();
      await tester.pumpWidget(
        MaterialApp(
          home: SourcesScreen(repository: repository, store: store),
        ),
      );
      await tester.pumpAndSettle();
      final update = find.byKey(const ValueKey('update-hongguo'));
      await tester.tap(update);
      await tester.pump();
      await tester.tap(update);
      expect(repository.operations, ['hongguo:update']);
      expect(tester.widget<FilledButton>(update).onPressed, isNull);
      repository.pending!.complete(
        SourceStatus.fromJson({
          'source': 'hongguo',
          'count': 3,
          'running': true,
          'stage': '查找新剧',
        }),
      );
      await tester.pump();
      await tester.tap(find.text('停止').first);
      await tester.pumpAndSettle();
      expect(repository.operations, ['hongguo:update', 'hongguo:cancel']);
      expect(find.text('已停止'), findsOneWidget);
      expect(find.text('3 部'), findsWidgets);
      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    },
  );

  testWidgets(
    'source manager requests only the current user permitted compiled sources',
    (tester) async {
      SharedPreferences.setMockInitialValues({
        'profiles': jsonEncode([
          const LocalProfile(id: 'default', name: '管理员', admin: true).toJson(),
          const LocalProfile(
            id: 'viewer',
            name: '只看红果',
            sources: ['hongguo'],
            download: false,
          ).toJson(),
        ]),
        'activeProfile': 'viewer',
      });
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = SourceFixtureRepository();
      await tester.pumpWidget(
        MaterialApp(
          home: SourcesScreen(repository: repository, store: store),
        ),
      );
      await tester.pumpAndSettle();
      expect(repository.statusRequests, ['hongguo']);
      expect(find.byKey(const ValueKey('source-hongguo')), findsOneWidget);
      for (final source in SourceSite.knownValues.skip(1)) {
        expect(find.byKey(ValueKey('source-${source.id}')), findsNothing);
      }
      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    },
  );

  for (final operation in ['check', 'checkCatalog']) {
    testWidgets(
      'source diagnostics remain collapsed through $operation polling and completion',
      (tester) async {
        SharedPreferences.setMockInitialValues({});
        final store = LocalStore(await SharedPreferences.getInstance());
        SourceStatus status(String state, {bool running = false}) =>
            SourceStatus.fromJson({
              'source': 'hongguo',
              'count': 7,
              'operation': operation,
              'running': running,
              'stage': running ? '检测连接' : '检测完成',
              'health': {
                'state': state,
                'checkedAt': '2026-09-21T00:00:00Z',
                'sample': '合成检测剧集',
                'steps': [
                  {
                    'name': '入口与目录',
                    'state': 'ok',
                    'message': '目录可达',
                    'httpStatus': 200,
                  },
                ],
              },
            });
        final repository = SourceFixtureRepository()
          ..statuses['hongguo'] = status('ok')
          ..pending = Completer<SourceStatus>();
        await tester.pumpWidget(
          MaterialApp(
            home: SourcesScreen(repository: repository, store: store),
          ),
        );
        await tester.pumpAndSettle();
        final toggle = find.byKey(const ValueKey('health-toggle-hongguo'));
        final sample = find.text('检测剧集：合成检测剧集');
        expect(toggle, findsOneWidget);
        expect(sample, findsNothing);
        expect(find.byTooltip('复制诊断信息'), findsOneWidget);

        if (operation == 'check') {
          await tester.tap(find.byKey(const ValueKey('check-hongguo')));
        } else {
          await tester.tap(find.byTooltip('红果更多操作'));
          await tester.pumpAndSettle();
          await tester.tap(find.text('仅检测目录'));
        }
        await tester.pump();
        expect(repository.operations, ['hongguo:$operation']);
        expect(sample, findsOneWidget);
        await tester.ensureVisible(toggle);
        await tester.tap(toggle);
        await tester.pump();
        expect(sample, findsNothing);

        final checking = status('checking', running: true);
        repository.statuses['hongguo'] = checking;
        repository.pending!.complete(checking);
        repository.pending = null;
        await tester.pump();
        expect(find.text('检测中'), findsOneWidget);
        expect(sample, findsNothing);

        final requests = repository.statusRequests.length;
        await tester.pump(const Duration(seconds: 2));
        await tester.pump();
        expect(repository.statusRequests.length, greaterThan(requests));
        expect(sample, findsNothing);

        repository.statuses['hongguo'] = status(
          operation == 'check' ? 'ok' : 'catalogOnly',
        );
        await tester.pump(const Duration(seconds: 2));
        await tester.pumpAndSettle();
        expect(
          find.text(operation == 'check' ? '连接检测通过' : '目录正常 · 播放未检测'),
          findsOneWidget,
        );
        expect(sample, findsNothing);

        await tester.tap(toggle);
        await tester.pumpAndSettle();
        expect(sample, findsOneWidget);
        expect(find.text('入口与目录：目录可达'), findsOneWidget);
        await tester.tap(toggle);
        await tester.pumpAndSettle();
        expect(sample, findsNothing);
        await tester.pump(const Duration(seconds: 10));
        await tester.pump();
        expect(sample, findsNothing);
        expect(find.byTooltip('复制诊断信息'), findsOneWidget);
        expect(tester.takeException(), isNull);
        await tester.pumpWidget(const SizedBox.shrink());
        store.dispose();
      },
    );
  }

  testWidgets(
    'source diagnostics distinguish CF playback failure on a narrow screen',
    (tester) async {
      tester.view.physicalSize = const Size(320, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = SourceFixtureRepository();
      repository.statuses['hongguo'] = SourceStatus.fromJson({
        'source': 'hongguo',
        'count': 7,
        'page': 3,
        'hasMore': true,
        'error': '合成 Cloudflare 验证错误',
        'health': {
          'state': 'failed',
          'checkedAt': '2026-09-19T03:00:00Z',
          'sample': '无图合成数据',
          'steps': [
            {
              'name': '入口与目录',
              'state': 'ok',
              'message': '目录可达',
              'httpStatus': 200,
            },
            {
              'name': '播放地址与播放列表',
              'state': 'failed',
              'message': 'Cloudflare 要求浏览器验证',
              'httpStatus': 403,
              'host': 'example.test',
            },
          ],
        },
      });
      await tester.pumpWidget(
        MaterialApp(
          builder: (context, child) => MediaQuery(
            data: MediaQuery.of(
              context,
            ).copyWith(textScaler: const TextScaler.linear(1.5)),
            child: child!,
          ),
          home: SourcesScreen(repository: repository, store: store),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.textContaining('检测未通过'), findsOneWidget);
      expect(find.textContaining('HTTP 403'), findsNothing);
      final toggle = find.byKey(const ValueKey('health-toggle-hongguo'));
      await tester.ensureVisible(toggle);
      await tester.tap(toggle);
      await tester.pumpAndSettle();
      expect(find.textContaining('HTTP 403'), findsOneWidget);
      expect(find.textContaining('连接检测通过'), findsNothing);
      await tester.tap(toggle);
      await tester.pumpAndSettle();
      expect(find.textContaining('HTTP 403'), findsNothing);
      expect(find.textContaining('检测未通过'), findsOneWidget);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    },
  );
}
