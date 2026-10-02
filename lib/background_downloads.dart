import 'dart:async';
import 'dart:io';

import 'package:flutter_foreground_task/flutter_foreground_task.dart';

import 'core_bridge.dart';
import 'app_build.dart';

@pragma('vm:entry-point')
void downloadServiceEntry() {
  FlutterForegroundTask.setTaskHandler(DownloadTaskHandler());
}

class BackgroundDownloads {
  static Future<void> prepare() async {
    if (!Platform.isAndroid) return;
    FlutterForegroundTask.init(
      androidNotificationOptions: AndroidNotificationOptions(
        channelId: 'zhenguojian_downloads',
        channelName: '$appName下载',
        channelDescription: '后台下载进度',
        onlyAlertOnce: true,
      ),
      iosNotificationOptions: const IOSNotificationOptions(
        showNotification: false,
        playSound: false,
      ),
      foregroundTaskOptions: ForegroundTaskOptions(
        eventAction: ForegroundTaskEventAction.repeat(2000),
        allowWakeLock: true,
        allowWifiLock: true,
        allowAutoRestart: false,
        autoRunOnBoot: false,
        autoRunOnMyPackageReplaced: false,
        stopWithTask: false,
      ),
    );
  }

  static Future<void> ensureStarted() async {
    if (!Platform.isAndroid) return;
    await prepare();
    if (await FlutterForegroundTask.isRunningService) return;
    await FlutterForegroundTask.requestNotificationPermission();
    final result = await FlutterForegroundTask.startService(
      serviceId: 2406,
      serviceTypes: [ForegroundServiceTypes.dataSync],
      notificationTitle: appName,
      notificationText: '正在准备后台任务',
      notificationButtons: [
        const NotificationButton(id: 'pause', text: '暂停下载'),
      ],
      callback: downloadServiceEntry,
    );
    if (result is ServiceRequestFailure) {
      throw AppFailure('后台下载服务启动失败，请保持应用在前台后重试。');
    }
  }
}

class DownloadTaskHandler extends TaskHandler {
  final repository = NativeRepository(background: true);
  bool _polling = false;
  int _idle = 0;

  @override
  Future<void> onStart(DateTime timestamp, TaskStarter starter) async {
    await repository.initialize();
    await _update();
  }

  @override
  void onRepeatEvent(DateTime timestamp) {
    unawaited(_update());
  }

  Future<void> _update() async {
    if (_polling) return;
    _polling = true;
    try {
      final jobs = await repository.downloads();
      final active = jobs.where((job) => job.active).toList();
      final work = await repository.workLease('', '');
      if (active.isEmpty && work == 0) {
        _idle++;
        if (_idle >= 5) {
          await FlutterForegroundTask.stopService();
          return;
        }
      } else {
        _idle = 0;
      }
      final bytes = active.fold<int>(0, (total, job) => total + job.bytes);
      await FlutterForegroundTask.updateService(
        notificationTitle: appName,
        notificationText: work > 0
            ? '正在更新站源'
            : active.isEmpty
            ? '下载已完成或暂停'
            : '${active.length} 集下载中 · ${(bytes / 1048576).toStringAsFixed(1)} MB',
      );
    } catch (_) {
      await FlutterForegroundTask.updateService(
        notificationTitle: appName,
        notificationText: '正在等待下载任务',
      );
    } finally {
      _polling = false;
    }
  }

  @override
  void onNotificationButtonPressed(String id) {
    if (id == 'pause') unawaited(repository.controlDownloads('pauseAll'));
  }

  @override
  void onNotificationPressed() {
    FlutterForegroundTask.launchApp();
  }

  @override
  Future<void> onDestroy(DateTime timestamp, bool isTimeout) async {
    if (isTimeout) {
      await repository.controlDownloads('pauseAll');
    }
  }
}
