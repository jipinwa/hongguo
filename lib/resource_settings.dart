import 'dart:async';
import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

import 'models.dart';

class ResourceSettings {
  const ResourceSettings({
    this.proxyMode = 'auto',
    this.proxyUrl = '',
    this.catalogConcurrency = 3,
    this.catalogIntervalMs = 250,
    this.downloadConcurrency = 2,
    this.downloadBySource = false,
    this.warning = '',
    this.systemProxyStatus = '',
  });
  final String proxyMode, proxyUrl, warning, systemProxyStatus;
  final int catalogConcurrency, catalogIntervalMs, downloadConcurrency;
  final bool downloadBySource;

  factory ResourceSettings.fromJson(Map<String, dynamic> data) =>
      ResourceSettings(
        proxyMode: data['proxyMode'] as String? ?? 'auto',
        proxyUrl: data['proxyUrl'] as String? ?? '',
        catalogConcurrency: intValue(data['catalogConcurrency']),
        catalogIntervalMs: intValue(data['catalogIntervalMs']),
        downloadConcurrency: intValue(data['downloadConcurrency']),
        downloadBySource: data['downloadBySource'] == true,
        warning: data['warning'] as String? ?? '',
        systemProxyStatus: data['systemProxyStatus'] as String? ?? '',
      );

  Map<String, dynamic> toJson() => {
    'proxyMode': proxyMode,
    'proxyUrl': proxyUrl,
    'catalogConcurrency': catalogConcurrency,
    'catalogIntervalMs': catalogIntervalMs,
    'downloadConcurrency': downloadConcurrency,
    'downloadBySource': downloadBySource,
  };
}

class SystemProxyMonitor with WidgetsBindingObserver {
  SystemProxyMonitor._(this.update);
  final Future<void> Function(Map<String, dynamic>) update;
  static SystemProxyMonitor? _current;
  Timer? _timer;
  bool _reading = false;

  static void start(Future<void> Function(Map<String, dynamic>) update) {
    if (!Platform.isAndroid && !Platform.isIOS) return;
    _current?._timer?.cancel();
    if (_current != null) WidgetsBinding.instance.removeObserver(_current!);
    final monitor = SystemProxyMonitor._(update);
    _current = monitor;
    WidgetsBinding.instance.addObserver(monitor);
    monitor._timer = Timer.periodic(
      const Duration(seconds: 30),
      (_) => monitor.refresh(),
    );
    unawaited(monitor.refresh());
  }

  Future<void> refresh() async {
    if (_reading) return;
    _reading = true;
    try {
      final result = await const MethodChannel(
        'duanju/device',
      ).invokeMapMethod<String, dynamic>('systemProxy');
      if (result != null) await update(result);
    } catch (_) {
    } finally {
      _reading = false;
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) unawaited(refresh());
  }
}
