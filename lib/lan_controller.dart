import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:flutter/widgets.dart';
import 'package:nsd/nsd.dart' as nsd;

import 'core_bridge.dart';
import 'lan_models.dart';
import 'lan_sync_models.dart';
import 'local_store.dart';
import 'models.dart';

export 'lan_models.dart';
export 'lan_sync_models.dart';

part 'lan_sync.dart';
part 'lan_playback.dart';

class LanController extends ChangeNotifier with WidgetsBindingObserver {
  LanController(this.repository, this.store, {required this.kind}) {
    store.addListener(_storeChanged);
    WidgetsBinding.instance.addObserver(this);
    _timer = Timer.periodic(const Duration(seconds: 5), (_) => _tick());
    _storeChanged();
  }

  static LanController? current;
  static const serviceType = '_zgj-link._tcp';
  final AppRepository repository;
  final LocalStore store;
  final String kind;
  bool _disposed = false;
  bool _foreground = true;
  int _run = 0;
  String _configuration = '';
  String _generation = '';
  String _selected = '';
  int _revision = -1;
  int _urgentRevision = -1;
  int _sessionEpoch = -1;
  int _syncTicket = 0;
  int _pushSequence = 0;
  bool _connecting = false;
  Object? _pairing;
  bool _pinging = false;
  bool _manual = false;
  bool _syncing = false;
  bool _pushing = false;
  DateTime _scanStarted = DateTime.now();
  DateTime _retryAt = DateTime(2000);
  DateTime _lastAutomatic = DateTime(2000);
  Future<void> _lifecycle = Future.value();
  Future<void>? _syncWork;
  Future<void> _inboundWrites = Future.value();
  Timer? _timer;
  Timer? _gather;
  Timer? _urgent;
  nsd.Discovery? _discovery;
  nsd.Registration? _registration;
  final Map<String, LanPeer> _manualPeers = {};
  List<LanPeer> _peers = [];
  final Set<String> _syncRequests = {};
  final Set<String> _playRequests = {};
  _LanIncomingSync? _incomingSync;
  _LanPreparedPlayback? _preparedPlayback;
  final Map<String, Map<String, dynamic>> _playReceipts = {};
  LanPeer? local;
  LanConnection? connection;
  LanPlaybackHost? playbackHost;
  Future<void> Function(LanIncomingPlayback)? openPlayback;
  String? error;
  String discoveryMessage = '';
  String syncMessage = '尚未同步';
  String pushMessage = '';
  DateTime? lastSync;
  LanChangeCount? lastLocalCount;
  LanChangeCount? lastRemoteCount;
  int skipped = 0;

  Map<String, dynamic> get _checkedSettings {
    final value = store.lanSettings;
    for (final key in ['enabled', 'autoSync']) {
      if (value.containsKey(key) && value[key] is! bool)
        throw const FormatException('设备互联开关无效');
    }
    if (value.containsKey('name')) lanText(value['name'], 60);
    final pins = lanMap(value['pins'] ?? {});
    if (pins.length > 64 ||
        pins.entries.any(
          (entry) =>
              !RegExp(r'^[a-f0-9]{32}$').hasMatch(entry.key) ||
              entry.value is! String ||
              !RegExp(r'^[a-f0-9]{64}$').hasMatch(entry.value as String),
        )) {
      throw const FormatException('已配对设备记录无效');
    }
    if (value['preferred'] != null) {
      final peer = LanPeer.fromJson(lanMap(value['preferred']));
      if (pins[peer.id] != peer.pin ||
          value['remoteAccount'] is! String ||
          !RegExp(
            r'^[a-f0-9]{32}$',
          ).hasMatch(value['remoteAccount'] as String)) {
        throw const FormatException('上次连接的设备记录无效');
      }
    }
    return value;
  }

  Map<String, dynamic> get settings {
    try {
      return _checkedSettings;
    } catch (_) {
      error = '设备互联设置损坏，已停止互联；请从原备份恢复配置';
      return {};
    }
  }

  bool get enabled => !store.locked && settings['enabled'] == true;
  bool get autoSync => !store.locked && settings['autoSync'] != false;
  bool get receiving =>
      _generation.isNotEmpty &&
      _foreground &&
      !store.locked &&
      store.profileEpoch == _sessionEpoch;
  bool get connecting => _connecting;
  bool get syncing => _syncing || _incomingSync != null;
  bool get pushing => _pushing;
  bool get manual => _manual;
  String get deviceName {
    final name = settings['name'];
    if (name is String && name.trim().isNotEmpty) return name;
    return switch (kind) {
      'tv' => '电视',
      'computer' => '电脑',
      _ => Platform.isIOS ? 'iPhone' : 'Android 手机',
    };
  }

  int get conflictCount => store.locked
      ? 0
      : store.lanDocument.records.values
            .where((record) => store.allowsSource(record.drama.source))
            .fold(0, (count, record) => count + record.conflicts);
  List<LanPeer> get peers => List.unmodifiable(_peers);
  String get connectionLabel => connection != null
      ? '已连接 · ' + connection!.peer.name
      : _connecting
      ? '正在连接'
      : receiving
      ? _peers.length > 1
            ? '请选择设备'
            : '正在发现设备'
      : enabled
      ? '回到前台后自动连接'
      : '设备互联已关闭';

  LanPeer? get remembered {
    final value = settings['preferred'];
    if (value == null) return null;
    return LanPeer.fromJson(lanMap(value));
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  void _checkUser(int epoch) {
    if (_disposed || store.locked || store.profileEpoch != epoch) {
      throw StateError('当前用户已变更，请重新操作');
    }
  }

  Future<Map<String, dynamic>> _native(
    String command, [
    Map<String, dynamic> data = const {},
  ]) => repository.lan(command, {'generation': _generation, ...data});

  Map<String, dynamic> _config() => {
    'name': deviceName,
    'kind': kind,
    'account': store.lanDocument.replica,
    'user': store.profile.name,
    'sources': store.sources.map((source) => source.id).toList(),
    'autoSync': autoSync,
  };

  Map<String, dynamic> _peerConfig() => {
    ..._config(),
    'sources': store.sources
        .map((source) => source.id)
        .where(lanLegacySources.contains)
        .toList(),
    'sourceOptions': store.sources.map((source) => source.id).toList(),
  };

  void _storeChanged() {
    if (_disposed) return;
    try {
      final configuration = lanJSON([
        store.profileEpoch,
        store.locked,
        enabled,
        store.locked ? '' : deviceName,
        store.sources.map((source) => source.id).toList(),
        _foreground,
      ]);
      if (configuration != _configuration) {
        final expired = _generation;
        if (expired.isNotEmpty) {
          unawaited(
            repository
                .lan('stop', {'generation': expired})
                .catchError((Object _) => <String, dynamic>{}),
          );
        }
        _configuration = configuration;
        _run++;
        _syncTicket++;
        _lifecycle = _lifecycle
            .catchError((Object _) {})
            .then((_) => _reconcile());
        unawaited(
          _lifecycle.catchError((Object failure) {
            error = failure.toString();
            _notify();
          }),
        );
        _notify();
        return;
      }
      if (_revision != store.lanRevision) {
        _revision = store.lanRevision;
        if (_urgentRevision != store.lanUrgentRevision) {
          _urgentRevision = store.lanUrgentRevision;
          _urgent?.cancel();
          _urgent = Timer(const Duration(milliseconds: 700), () => flush());
        }
      }
    } catch (failure) {
      error = failure.toString();
      _notify();
    }
  }

  Future<void> setEnabled(bool value) async {
    final values = _checkedSettings;
    await store.saveLanSettings({...values, 'enabled': value});
    await _lifecycle;
  }

  Future<void> setAutoSync(bool value) async {
    final epoch = store.profileEpoch;
    await cancelSync();
    _checkUser(epoch);
    await store.saveLanSettings({..._checkedSettings, 'autoSync': value});
    _checkUser(epoch);
    if (receiving) await _native('configure', {'config': _config()});
    _notify();
    if (value) flush();
  }

  Future<void> rename(String name) async {
    name = name.trim();
    if (name.isEmpty || name.length > 60) throw StateError('设备名称需为 1–60 个字');
    await store.saveLanSettings({..._checkedSettings, 'name': name});
    await _lifecycle;
  }

  Future<void> ensureEnabled() async {
    final epoch = store.profileEpoch;
    _checkUser(epoch);
    if (!enabled) await setEnabled(true);
    await _lifecycle;
    _checkUser(epoch);
    if (!receiving) {
      await _reconcile();
    }
    if (!receiving) throw StateError(error ?? '请保持应用在前台后重试');
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    final foreground = state == AppLifecycleState.resumed;
    if (foreground == _foreground) return;
    _foreground = foreground;
    _storeChanged();
  }

  Future<void> _stop() async {
    _pushSequence++;
    _pushing = false;
    _pairing = null;
    if (store.locked || store.profileEpoch != _sessionEpoch) {
      lastSync = null;
      lastLocalCount = null;
      lastRemoteCount = null;
      skipped = 0;
      syncMessage = '尚未同步';
      pushMessage = '';
      playbackHost = null;
    }
    final oldGeneration = _generation;
    _generation = '';
    connection = null;
    local = null;
    _connecting = false;
    _selected = '';
    _incomingSync = null;
    _manual = false;
    _syncTicket++;
    _peers = [];
    _manualPeers.clear();
    _gather?.cancel();
    _urgent?.cancel();
    if (oldGeneration.isNotEmpty) {
      await repository
          .lan('stop', {'generation': oldGeneration})
          .catchError((Object _) => <String, dynamic>{});
    }
    await _cancelIncomingPlayback();
    final discovery = _discovery;
    _discovery = null;
    final registration = _registration;
    _registration = null;
    if (discovery != null) {
      discovery.removeListener(_discovered);
      await nsd.stopDiscovery(discovery).catchError((Object _) {});
    }
    if (registration != null)
      await nsd.unregister(registration).catchError((Object _) {});
    _notify();
  }

  Future<void> _reconcile() async {
    final run = _run;
    await _stop();
    if (_disposed || run != _run || !_foreground || !enabled) return;
    await store.ensureLanRecords();
    if (_disposed || run != _run) return;
    _sessionEpoch = store.profileEpoch;
    final info = await repository.lan('start', {'config': _config()});
    if (_disposed || run != _run) {
      await repository.lan('stop', {'generation': info['generation']});
      return;
    }
    _generation = lanText(info['generation'], 32);
    local = LanPeer.fromJson(info);
    _scanStarted = DateTime.now();
    error = null;
    discoveryMessage = '';
    unawaited(_poll(run, _generation));
    try {
      final registration = await nsd.register(
        nsd.Service(
          name: 'Zgj-' + local!.id.substring(0, 12),
          type: serviceType,
          port: local!.port,
          txt: {
            for (final entry in {
              'id': local!.id,
              'name': deviceName,
              'pin': local!.pin,
              'kind': kind,
              'v': '1',
              'caps': 'sync,play',
            }.entries)
              entry.key: Uint8List.fromList(utf8.encode(entry.value)),
          },
        ),
      );
      if (_disposed || run != _run) {
        await nsd.unregister(registration);
        return;
      }
      _registration = registration;
    } catch (_) {
      discoveryMessage = '自动发现暂不可用，可用下方本机地址手动连接';
    }
    if (_disposed || run != _run) return;
    await rescan();
    _notify();
  }

  Future<void> rescan() async {
    if (!receiving) return;
    final run = _run;
    final previous = _discovery;
    _discovery = null;
    if (previous != null) {
      previous.removeListener(_discovered);
      await nsd.stopDiscovery(previous).catchError((Object _) {});
    }
    _scanStarted = DateTime.now();
    _retryAt = DateTime(2000);
    try {
      final discovery = await nsd.startDiscovery(
        serviceType,
        ipLookupType: nsd.IpLookupType.any,
      );
      if (_disposed || run != _run) {
        await nsd.stopDiscovery(discovery);
        return;
      }
      _discovery = discovery;
      discovery.addListener(_discovered);
      _discovered();
    } catch (_) {
      discoveryMessage = '未能自动发现设备。请检查局域网权限，或输入对方的本机地址';
      _notify();
    }
    _gather?.cancel();
    _gather = Timer(const Duration(seconds: 3), () => _autoConnect());
  }

  void _discovered() {
    final map = <String, LanPeer>{..._manualPeers};
    final conflicted = <String>{};
    for (final service in _discovery?.services.take(128) ?? <nsd.Service>[]) {
      try {
        String text(String key) =>
            utf8.decode(service.txt?[key] ?? [], allowMalformed: false);
        final addresses = <String>[];
        for (final address in service.addresses ?? <InternetAddress>[]) {
          final host = address.address;
          addresses.add(
            host.contains(':')
                ? '[$host]:' + service.port.toString()
                : '$host:' + service.port.toString(),
          );
        }
        if (addresses.isEmpty || text('caps') != 'sync,play') continue;
        final peer = LanPeer.fromJson({
          'deviceId': text('id'),
          'name': text('name'),
          'pin': text('pin'),
          'kind': text('kind'),
          'port': service.port,
          'protocol': int.tryParse(text('v')),
          'addresses': addresses,
        });
        if (peer.id == local?.id) continue;
        final previous = map[peer.id];
        if (previous != null && previous.pin != peer.pin) {
          conflicted.add(peer.id);
          continue;
        }
        map[peer.id] = peer.withAddresses([
          ...?previous?.addresses,
          ...peer.addresses,
        ]);
      } catch (_) {}
    }
    for (final id in conflicted) {
      map.remove(id);
    }
    _peers = map.values.toList()..sort((a, b) => a.name.compareTo(b.name));
    _notify();
    if (DateTime.now().difference(_scanStarted) >= const Duration(seconds: 3)) {
      _gather?.cancel();
      _gather = Timer(const Duration(milliseconds: 500), () => _autoConnect());
    }
  }

  Future<LanPeer> probe(String address) async {
    await ensureEnabled();
    final response = await _native('probe', {
      'address': address.trim(),
      'requestId': lanID(),
    });
    final peer = LanPeer.fromJson(response);
    if (peer.id == local?.id) throw StateError('这是本机地址，请输入另一台设备的地址');
    _checkPin(peer);
    _manualPeers[peer.id] = peer;
    _discovered();
    return peer;
  }

  void _checkPin(LanPeer peer) {
    final pins = lanMap(settings['pins'] ?? {});
    final pin = pins[peer.id];
    if (pin != null && pin != peer.pin) {
      throw StateError('此设备的身份已改变，请先核对设备，再从设备菜单忘记旧配对');
    }
  }

  Future<void> _remember(LanPeer peer, String account) async {
    final values = _checkedSettings;
    final pins = lanMap(values['pins'] ?? {});
    _checkPin(peer);
    if (!pins.containsKey(peer.id) && pins.length >= 64) {
      throw StateError('已记住 64 台设备，请先忘记不再使用的设备');
    }
    pins[peer.id] = peer.pin;
    await store.saveLanSettings({
      ...values,
      'pins': pins,
      'preferred': peer.toJson(),
      'remoteAccount': account,
    });
  }

  Future<void> forget(LanPeer peer) async {
    final epoch = store.profileEpoch;
    if (connection?.peer.id == peer.id || _selected == peer.id)
      await disconnect();
    _checkUser(epoch);
    final values = {..._checkedSettings};
    final pins = lanMap(values['pins'] ?? {})..remove(peer.id);
    values['pins'] = pins;
    if (remembered?.id == peer.id) {
      values.remove('preferred');
      values.remove('remoteAccount');
    }
    await store.saveLanSettings(values);
    _retryAt = DateTime.now().add(const Duration(seconds: 5));
    _notify();
  }

  Future<void> disconnect() async {
    _pushSequence++;
    _pushing = false;
    _syncTicket++;
    _selected = '';
    connection = null;
    _incomingSync = null;
    _retryAt = DateTime.now().add(const Duration(minutes: 5));
    await _cancelIncomingPlayback();
    if (receiving) await _native('disconnect');
    _notify();
  }

  Future<void> connect(LanPeer peer, {bool automatic = false}) async {
    final epoch = store.profileEpoch;
    await ensureEnabled();
    _checkUser(epoch);
    if (connection?.peer.id == peer.id && connection?.peer.pin == peer.pin)
      return;
    if (_connecting || _pairing != null) throw StateError('正在连接设备，请稍候');
    _checkPin(peer);
    if (connection != null) await disconnect();
    _checkUser(epoch);
    final run = _run;
    _selected = peer.id;
    _connecting = true;
    error = null;
    _notify();
    try {
      final grant = await _native('allow', {
        'deviceId': peer.id,
        'pin': peer.pin,
      });
      if (_disposed || run != _run || _selected != peer.id || !receiving)
        throw StateError('连接已取消');
      Object? lastError;
      for (final address in peer.addresses) {
        try {
          final data = await _native('request', {
            'deviceId': peer.id,
            'pin': peer.pin,
            'address': address,
            'path': 'pair',
            'requestId': lanID(),
            'payload': {
              ...local!.toJson(),
              ..._peerConfig(),
              'returnToken': grant['token'],
              'expectedAccount': automatic && remembered?.id == peer.id
                  ? settings['remoteAccount']
                  : null,
            },
          });
          if (_disposed || run != _run || _selected != peer.id)
            throw StateError('连接已取消');
          final remote = _connection(peer, address, data);
          await _remember(peer, remote.account);
          if (_disposed || run != _run || _selected != peer.id)
            throw StateError('连接已取消');
          connection = remote;
          syncMessage = autoSync && remote.autoSync
              ? '已连接，等待同步'
              : '已连接，自动同步已暂停';
          _retryAt = DateTime(2000);
          _lastAutomatic = DateTime(2000);
          _notify();
          flush();
          return;
        } catch (failure) {
          lastError = failure;
          if (_disposed || run != _run) rethrow;
        }
      }
      throw lastError ?? StateError('设备尚未解析出可用地址');
    } catch (failure) {
      if (run == _run) {
        error = failure.toString();
        if (connection?.peer.id != peer.id) {
          _selected = '';
          await _native(
            'disconnect',
          ).catchError((Object _) => <String, dynamic>{});
        }
        _retryAt = DateTime.now().add(const Duration(seconds: 15));
      }
      rethrow;
    } finally {
      if (run == _run) _connecting = false;
      _notify();
    }
  }

  LanConnection _connection(
    LanPeer peer,
    String address,
    Map<String, dynamic> data,
  ) {
    final token = lanText(data['token'], 64);
    final account = lanText(data['account'], 32);
    if (!RegExp(r'^[a-f0-9]{64}$').hasMatch(token) ||
        !RegExp(r'^[a-f0-9]{32}$').hasMatch(account)) {
      throw const FormatException('连接会话无效');
    }
    return LanConnection(
      peer: peer,
      address: address,
      token: token,
      account: account,
      user: lanText(data['user'], 80),
      sources: lanSources(
        data['sourceOptions'] ?? data['sources'],
        advertised: true,
      ),
      autoSync: data['autoSync'] == true,
    );
  }

  Future<void> _autoConnect() async {
    if (_disposed ||
        !receiving ||
        connection != null ||
        _connecting ||
        DateTime.now().isBefore(_retryAt))
      return;
    try {
      final previous = remembered;
      final peer = previous == null
          ? (_peers.length == 1 ? _peers.single : null)
          : _peers.where((peer) => peer.id == previous.id).firstOrNull ??
                previous;
      if (peer == null) return;
      if (previous == null &&
          local!.id.compareTo(peer.id) > 0 &&
          DateTime.now().difference(_scanStarted) < const Duration(seconds: 7))
        return;
      await connect(peer, automatic: true);
    } catch (failure) {
      error = failure.toString();
      _retryAt = DateTime.now().add(const Duration(seconds: 15));
      _notify();
    }
  }

  Future<Map<String, dynamic>> _pair(Map<String, dynamic> event) async {
    if (_pairing != null) throw StateError('接收设备正在建立连接，请稍后重试');
    final ticket = Object();
    _pairing = ticket;
    try {
      return await _acceptPair(event);
    } finally {
      if (identical(_pairing, ticket)) _pairing = null;
    }
  }

  Future<Map<String, dynamic>> _acceptPair(Map<String, dynamic> event) async {
    final body = lanMap(event['payload']);
    final peer = LanPeer.fromJson(body);
    if (peer.id != event['peerId'] || peer.pin != event['pin'])
      throw StateError('设备身份不一致');
    _checkPin(peer);
    if (body['expectedAccount'] != null &&
        body['expectedAccount'] != store.lanDocument.replica) {
      throw StateError('对方切换了用户，请重新选择设备');
    }
    if (connection != null && connection!.peer.id != peer.id ||
        _connecting && _selected.isNotEmpty && _selected != peer.id) {
      throw StateError('接收设备已有其他连接，当前连接不会被抢占');
    }
    final port = peer.port;
    final host = lanText(event['host'], 128);
    final address = host.contains(':') ? '[$host]:$port' : '$host:$port';
    final resolved = peer.withAddresses([address, ...peer.addresses]);
    final reverse = _connection(resolved, address, {
      ...body,
      'token': body['returnToken'],
    });
    if (connection != null && connection!.account != reverse.account) {
      throw StateError('此设备已切换用户，请先断开旧连接');
    }
    final run = _run;
    final grant = await _native('allow', {
      'deviceId': peer.id,
      'pin': peer.pin,
    });
    if (run != _run || !receiving) throw StateError('当前用户已变更，请重新连接');
    await _remember(resolved, reverse.account);
    if (run != _run || !receiving) throw StateError('当前用户已变更，请重新连接');
    connection = reverse;
    _selected = peer.id;
    _lastAutomatic = DateTime(2000);
    syncMessage = autoSync && reverse.autoSync ? '已连接，等待同步' : '已连接，自动同步已暂停';
    _notify();
    _urgent?.cancel();
    _urgent = Timer(const Duration(seconds: 1), flush);
    return {..._peerConfig(), 'token': grant['token']};
  }

  Future<void> _poll(int run, String generation) async {
    while (!_disposed && run == _run && _generation == generation) {
      try {
        final data = await repository.lan('poll', {'generation': generation});
        if (run != _run || _disposed) return;
        if (data['event'] != null) {
          unawaited(_event(lanMap(data['event']), run, generation));
        }
      } catch (failure) {
        if (_disposed || run != _run) return;
        error = failure.toString();
        _notify();
        await Future<void>.delayed(const Duration(seconds: 1));
      }
    }
  }

  Future<void> _event(
    Map<String, dynamic> event,
    int run,
    String generation,
  ) async {
    Object? result;
    String failure = '';
    try {
      if (run != _run ||
          store.locked ||
          store.profileEpoch != _sessionEpoch ||
          !_foreground) {
        throw StateError('接收已关闭或当前用户已变更');
      }
      final path = event['path'];
      if (path == 'pair') {
        result = await _pair(event);
      } else {
        final remote = connection;
        if (remote == null ||
            remote.peer.id != event['peerId'] ||
            remote.peer.pin != event['pin']) {
          throw StateError('请先连接此设备');
        }
        final body = lanMap(event['payload']);
        if (path == 'status') {
          if (body['flush'] == true) _queueAutomatic();
          final scope = _scope(remote);
          final hash = store.lanDocument.hashFor(scope);
          if (body['synced'] is String && body['hash'] == hash && !syncing) {
            lastSync = DateTime.now();
            final conflicts = conflictCount;
            syncMessage = conflicts == 0
                ? '双方记录已同步'
                : '记录已同步 · $conflicts 项冲突待处理';
            _notify();
          }
          result = {
            ..._peerConfig(),
            'manual': _manual,
            'syncing': syncing,
            'playing': playbackHost?.title ?? '',
            'hash': hash,
          };
        } else if (path is String && path.startsWith('sync/')) {
          final completer = Completer<Object?>();
          _inboundWrites = _inboundWrites.catchError((Object _) {}).then((
            _,
          ) async {
            try {
              if (run != _run || connection != remote)
                throw StateError('设备连接已变更');
              completer.complete(await _receiveSync(path, body, remote));
            } catch (error, stack) {
              completer.completeError(error, stack);
            }
          });
          result = await completer.future;
        } else if (path is String && path.startsWith('play/')) {
          result = await _receivePlayback(path, body, remote);
        } else {
          throw StateError('不支持此设备操作');
        }
      }
    } catch (error) {
      failure = error.toString();
    }
    await repository
        .lan('respond', {
          'generation': generation,
          'eventId': event['id'],
          'payload': result ?? {},
          'error': failure,
        })
        .catchError((Object _) => <String, dynamic>{});
  }

  Future<Map<String, dynamic>> _request(
    String path,
    Map<String, dynamic> payload, {
    String owner = '',
  }) async {
    final remote = connection;
    if (remote == null || !receiving) throw StateError('请先连接设备');
    final run = _run;
    final request = lanID();
    final requests = owner == 'sync'
        ? _syncRequests
        : owner == 'play'
        ? _playRequests
        : null;
    requests?.add(request);
    try {
      final result = await _native('request', {
        'deviceId': remote.peer.id,
        'pin': remote.peer.pin,
        'address': remote.address,
        'token': remote.token,
        'path': path,
        'payload': payload,
        'requestId': request,
      });
      if (_disposed ||
          run != _run ||
          connection != remote ||
          store.profileEpoch != _sessionEpoch) {
        throw StateError('设备或用户已变更，请重新操作');
      }
      return result;
    } finally {
      requests?.remove(request);
    }
  }

  void flush() {
    if (_disposed || !receiving || connection == null || !autoSync) return;
    if (_leader) {
      _queueAutomatic();
    } else {
      unawaited(
        _request('status', {
          'flush': true,
        }).catchError((Object _) => <String, dynamic>{}),
      );
    }
  }

  bool get _leader =>
      local != null &&
      connection != null &&
      local!.id.compareTo(connection!.peer.id) < 0;

  void _queueAutomatic() {
    if (!_leader ||
        !autoSync ||
        connection?.autoSync != true ||
        _manual ||
        syncing ||
        _disposed)
      return;
    _lastAutomatic = DateTime.now();
    final run = _run;
    unawaited(
      synchronize(automatic: true).catchError((Object failure) {
        if (run != _run || _disposed) return;
        syncMessage = '等待重试 · ' + failure.toString();
        _notify();
      }),
    );
  }

  Future<void> _tick() async {
    if (_disposed || !receiving) return;
    if (_incomingSync != null &&
        DateTime.now().difference(_incomingSync!.updated) >
            const Duration(minutes: 2)) {
      _incomingSync = null;
      syncMessage = '上次同步未提交，原记录已保留';
      _notify();
    }
    await _expireIncomingPlayback();
    if (connection == null) {
      await _autoConnect();
      return;
    }
    if (_pinging) return;
    _pinging = true;
    final remote = connection;
    try {
      final data = await _request('status', {});
      if (data['account'] != remote!.account)
        throw StateError('对方已切换用户，请重新选择设备');
      remote.autoSync = data['autoSync'] == true;
      if (data['manual'] != true &&
          DateTime.now().difference(_lastAutomatic) >=
              const Duration(seconds: 10)) {
        if (data['hash'] != store.lanDocument.hashFor(_scope(remote))) {
          _queueAutomatic();
        } else if (autoSync && remote.autoSync && !_manual && !syncing) {
          _lastAutomatic = DateTime.now();
          final conflicts = conflictCount;
          syncMessage = conflicts == 0
              ? '双方记录已同步'
              : '记录已同步 · $conflicts 项冲突待处理';
          lastSync ??= DateTime.now();
          _notify();
        }
      }
    } catch (failure) {
      if (connection == remote) {
        connection = null;
        _syncTicket++;
        error = failure.toString();
        syncMessage = '连接中断，重连后补齐';
        _retryAt = DateTime.now().add(const Duration(seconds: 5));
        _notify();
      }
    } finally {
      _pinging = false;
    }
  }

  @override
  void dispose() {
    _disposed = true;
    _run++;
    _syncTicket++;
    store.removeListener(_storeChanged);
    WidgetsBinding.instance.removeObserver(this);
    _timer?.cancel();
    _gather?.cancel();
    _urgent?.cancel();
    unawaited(
      _lifecycle
          .catchError((Object _) {})
          .then((_) => _stop())
          .catchError((Object _) {}),
    );
    super.dispose();
  }
}
