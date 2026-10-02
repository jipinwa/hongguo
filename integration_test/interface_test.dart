import 'dart:async';

import 'package:duanju_app/home_screen.dart';
import 'package:duanju_app/app_build.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/main.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../test/interface_fixtures.dart';

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets(
    'catalog feedback, poster alignment and download layout on device',
    (tester) async {
      expect(const bool.fromEnvironment('DISABLE_REMOTE_IMAGES'), isTrue);
      await SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
      await SystemChrome.setPreferredOrientations([
        DeviceOrientation.portraitUp,
      ]);
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = InterfaceRepository();

      Future<void> capture(String name) async {
        await tester.pump(const Duration(milliseconds: 400));
        debugPrint('APPEARANCE_CAPTURE $name');
        await tester.runAsync(
          () => Future<void>.delayed(const Duration(seconds: 2)),
        );
        await binding.takeScreenshot(name);
      }

      Future<void> source(String name) async {
        await tester.tap(find.byKey(const ValueKey('source-switch')));
        await tester.pumpAndSettle();
        await tester.tap(find.text(name).last);
        await tester.pumpAndSettle();
      }

      await tester.pumpWidget(DuanjuApp(repository: repository, store: store));
      await tester.pumpAndSettle();
      expect(store.themeMode, 'system');
      expect(
        Theme.of(tester.element(find.byType(HomeScreen))).brightness,
        WidgetsBinding.instance.platformDispatcher.platformBrightness,
      );
      final covers = find.byType(DramaCover);
      final firstCover = tester.getRect(covers.first);
      for (var index = 1; index < 3; index++) {
        expect(
          tester.getRect(covers.at(index)).bottom,
          closeTo(firstCover.bottom, .01),
        );
      }
      await binding.convertFlutterSurfaceToImage();
      await capture('interface-system-catalog');

      final pending = Completer<CatalogPage>();
      repository.pendingCatalog = pending;
      final refresh = find.byKey(const ValueKey('catalog-refresh'));
      await tester.tap(refresh);
      await tester.pump();
      final rotation = find.descendant(
        of: refresh,
        matching: find.byType(RotationTransition),
      );
      final angle = tester.widget<RotationTransition>(rotation).turns.value;
      await tester.pump(const Duration(milliseconds: 200));
      expect(
        tester.widget<RotationTransition>(rotation).turns.value,
        isNot(angle),
      );
      await capture('interface-refreshing');
      repository.pendingCatalog = null;
      pending.complete(CatalogPage(repository.dramas('hongguo')));
      await tester.pumpAndSettle();
      expect(find.byTooltip('更新当前站源'), findsOneWidget);

      if (allSourcesEnabled) {
        await source('黄豆');
        expect(find.byTooltip('VIP：隐藏'), findsOneWidget);
        expect(find.text('会员合成剧'), findsNothing);
        await capture('interface-huangdou-vip');
        for (final name in ['黄果', '红果']) {
          await source(name);
          expect(find.textContaining('VIP：'), findsNothing);
          expect(find.text('会员合成剧'), findsWidgets);
        }
      } else {
        expect(find.text('黄豆'), findsNothing);
        expect(find.text('红果'), findsOneWidget);
      }
      await store.setThemeMode('dark');
      await tester.pumpAndSettle();
      await capture('interface-dark-catalog');
      await tester.tap(find.byKey(const ValueKey('bottom-nav-3')));
      await tester.pumpAndSettle();
      await capture('interface-dark-downloads');
      await store.setThemeMode('light');
      await tester.pumpAndSettle();
      final menu = find.byKey(const ValueKey('download-queue-actions'));
      final filters = find.byKey(const ValueKey('download-filters'));
      expect(
        tester.getRect(menu).bottom,
        lessThan(tester.getRect(filters).top),
      );
      await capture('interface-light-downloads');
      await tester.tap(menu);
      await tester.pumpAndSettle();
      await capture('interface-download-queue-menu');
      await tester.tap(find.text('全部暂停'));
      await tester.pumpAndSettle();
      expect(repository.jobs.where((job) => job.active), isEmpty);
      await tester.tap(find.byKey(const ValueKey('download-filter-completed')));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('download-task-task-0')), findsNothing);
      await capture('interface-completed-downloads');
      expect(tester.takeException(), isNull);
      binding.reportData ??= {};
      binding.reportData!['interface'] = {
        'allSources': allSourcesEnabled,
        'systemThemeByDefault': true,
        'refreshAnimation': true,
        'alignedPosters': true,
        'vipOnlyOnHuangdou': true,
        'downloadMenuAboveFilters': true,
        'bulkPause': true,
        'downloadFilter': true,
        'remoteImagesDisabled': true,
      };
      await tester.pumpWidget(const SizedBox.shrink());
      await SystemChrome.setPreferredOrientations(DeviceOrientation.values);
      store.dispose();
    },
  );
}
