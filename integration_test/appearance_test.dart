import 'package:duanju_app/app_bottom_navigation.dart';
import 'package:duanju_app/home_screen.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/main.dart';
import 'package:duanju_app/settings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:media_kit_video/media_kit_video.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'playback_test.dart' show DeviceFixtureRepository, fixtureBase;

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('theme, navigation, keyboard and fullscreen system bars', (
    tester,
  ) async {
    expect(const bool.fromEnvironment('DISABLE_REMOTE_IMAGES'), isTrue);
    expect(fixtureBase, startsWith('http://127.0.0.1:'));
    MediaKit.ensureInitialized();
    await SystemChrome.setEnabledSystemUIMode(SystemUiMode.edgeToEdge);
    await SystemChrome.setPreferredOrientations([DeviceOrientation.portraitUp]);
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    final repository = DeviceFixtureRepository();
    await repository.initialize();
    await store.toggleFavorite(DeviceFixtureRepository.drama);

    Future<void> until(bool Function() ready, String step) async {
      final timer = Stopwatch()..start();
      while (!ready()) {
        if (timer.elapsed > const Duration(seconds: 30)) {
          fail('Timed out: $step');
        }
        await tester.pump(const Duration(milliseconds: 100));
      }
    }

    Future<void> capture(String name) async {
      await tester.pump(const Duration(milliseconds: 400));
      debugPrint('APPEARANCE_CAPTURE $name');
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(seconds: 2)),
      );
      await binding.takeScreenshot(name);
    }

    Player player() =>
        tester.widget<Video>(find.byType(Video)).controller.player;

    await tester.pumpWidget(DuanjuApp(repository: repository, store: store));
    await tester.pumpAndSettle();
    await binding.convertFlutterSurfaceToImage();
    await capture('appearance-system-home');
    await tester.tap(find.byKey(const ValueKey('bottom-nav-1')));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('更多'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('设置与备份'));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('theme-setting')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('浅色'));
    await tester.pumpAndSettle();
    expect(
      Theme.of(tester.element(find.byType(SettingsScreen))).brightness,
      Brightness.light,
    );
    await capture('appearance-light-settings');
    await tester.tap(find.byType(BackButton).last);
    await tester.pumpAndSettle();
    expect(
      tester
          .widget<AppBottomNavigation>(find.byType(AppBottomNavigation))
          .selectedIndex,
      1,
    );
    await capture('appearance-light-favorites');
    await tester.tap(find.byKey(const ValueKey('bottom-nav-0')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const ValueKey('toggle-search')));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(TextField).first);
    await tester.pump(const Duration(milliseconds: 700));
    FocusManager.instance.primaryFocus?.unfocus();
    await SystemChannels.textInput.invokeMethod<void>('TextInput.hide');
    await tester.pumpAndSettle();
    await capture('appearance-light-keyboard-return');
    await tester.tap(find.text('设备播放验证'));
    await until(
      () => find.byKey(const ValueKey('episode-1')).evaluate().isNotEmpty,
      'detail',
    );
    await tester.pumpAndSettle();
    await capture('appearance-light-detail');
    await tester.tap(find.byKey(const ValueKey('episode-1')));
    await until(() => find.byType(Video).evaluate().isNotEmpty, 'player');
    await player().setVolume(0);
    await until(
      () =>
          player().state.position > Duration.zero &&
          player().state.width != null,
      'decoded',
    );
    expect(
      Theme.of(tester.element(find.byType(Video))).brightness,
      Brightness.dark,
    );
    await capture('appearance-dark-player');
    await tester.tap(find.byTooltip('旋转与全屏').first);
    await until(
      () =>
          MediaQuery.orientationOf(tester.element(find.byType(Video))) ==
          Orientation.landscape,
      'fullscreen',
    );
    await tester.pump(const Duration(milliseconds: 400));
    await tester.tap(find.byTooltip('退出全屏').first);
    await until(
      () =>
          MediaQuery.orientationOf(tester.element(find.byType(Video))) ==
          Orientation.portrait,
      'portrait',
    );
    await tester.pump(const Duration(milliseconds: 400));
    await tester.tap(find.byType(BackButton).last);
    await tester.pumpAndSettle();
    await tester.tap(find.byType(BackButton).last);
    await tester.pumpAndSettle();
    expect(
      Theme.of(tester.element(find.byType(HomeScreen))).brightness,
      Brightness.light,
    );
    await capture('appearance-light-player-return');
    expect(tester.takeException(), isNull);
    binding.reportData ??= {};
    binding.reportData!['appearance'] = {
      'theme': store.themeMode,
      'tabPreserved': true,
      'keyboardReturn': true,
      'fullscreenReturn': true,
      'remoteImagesDisabled': true,
    };
    await tester.pumpWidget(const SizedBox.shrink());
    await SystemChrome.setPreferredOrientations(DeviceOrientation.values);
    store.dispose();
  });
}
