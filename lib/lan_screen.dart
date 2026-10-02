import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'lan_controller.dart';
import 'local_store.dart';
import 'remote_widgets.dart';
import 'widgets.dart';

IconData lanDeviceIcon(String kind) => switch (kind) {
  'tv' => Icons.tv_rounded,
  'computer' => Icons.computer_rounded,
  _ => Icons.smartphone_rounded,
};

Future<T?> _lanPanel<T>(
  BuildContext context,
  Widget child, {
  double maxHeight = 740,
  bool dismissible = true,
}) {
  final size = MediaQuery.sizeOf(context);
  final height = min(size.height * .88, maxHeight);
  if (size.width >= 800) {
    return showDialog<T>(
      context: context,
      barrierDismissible: dismissible,
      builder: (_) => Dialog(
        clipBehavior: Clip.antiAlias,
        child: SizedBox(width: 620, height: height, child: child),
      ),
    );
  }
  return showModalBottomSheet<T>(
    context: context,
    useSafeArea: true,
    isScrollControlled: true,
    isDismissible: dismissible,
    enableDrag: dismissible,
    showDragHandle: true,
    constraints: const BoxConstraints(maxWidth: 720),
    builder: (context) {
      final keyboard = MediaQuery.viewInsetsOf(context).bottom;
      final available = max(
        0.0,
        MediaQuery.sizeOf(context).height -
            keyboard -
            MediaQuery.paddingOf(context).top -
            24,
      );
      return Padding(
        padding: EdgeInsets.only(bottom: keyboard),
        child: SizedBox(height: min(height, available), child: child),
      );
    },
  );
}

Future<LanConnection?> chooseLanDevice(
  BuildContext context,
  LanController controller, {
  bool change = false,
}) async {
  if (!change && controller.connection != null) return controller.connection;
  return _lanPanel<LanConnection>(
    context,
    _LanDevices(controller: controller, change: change),
  );
}

void openLanSync(BuildContext context) {
  final controller = LanController.current;
  if (controller == null) {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(const SnackBar(content: Text('设备互联尚未就绪')));
    return;
  }
  Navigator.push(
    context,
    MaterialPageRoute<void>(
      builder: (_) => LanSyncScreen(controller: controller),
    ),
  );
}

class LanSyncScreen extends StatefulWidget {
  const LanSyncScreen({super.key, required this.controller});
  final LanController controller;
  @override
  State<LanSyncScreen> createState() => _LanSyncScreenState();
}

class _LanSyncScreenState extends State<LanSyncScreen> {
  bool _busy = false;
  bool _cancelling = false;
  String? _error;
  LanController get link => widget.controller;

  @override
  void initState() {
    super.initState();
    ensureTelevisionFocus(context);
  }

  Future<void> _perform(Future<void> Function() action) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _rename() async {
    final epoch = link.store.profileEpoch;
    final name = TextEditingController(text: link.deviceName);
    final value = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('设备名称'),
        content: TextField(
          controller: name,
          maxLength: 60,
          autofocus: true,
          decoration: const InputDecoration(labelText: '在其他设备上显示的名称'),
          onSubmitted: (_) => Navigator.pop(context, name.text),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, name.text),
            child: const Text('保存'),
          ),
        ],
      ),
    );
    name.dispose();
    if (mounted &&
        value != null &&
        epoch == link.store.profileEpoch &&
        !link.store.locked) {
      await _perform(() => link.rename(value));
    }
  }

  Future<void> _cancel() async {
    if (_cancelling) return;
    setState(() => _cancelling = true);
    try {
      await link.cancelSync();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _cancelling = false);
    }
  }

  Future<void> _manual() async {
    final epoch = link.store.profileEpoch;
    await _perform(() async {
      await link.beginManual();
      try {
        if (mounted && epoch == link.store.profileEpoch && !link.store.locked) {
          await _lanPanel<void>(
            context,
            _LanManual(controller: link),
            dismissible: false,
          );
        }
      } finally {
        if (epoch == link.store.profileEpoch) link.endManual();
      }
    });
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: link,
    builder: (context, _) {
      final remote = link.connection;
      int conflicts = 0;
      try {
        conflicts = link.conflictCount;
      } catch (_) {}
      final controlsEnabled =
          !_busy && !_cancelling && !link.syncing && !link.store.locked;
      return Scaffold(
        appBar: AppBar(title: const Text('追剧同步')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
              children: [
                Card(
                  child: Column(
                    children: [
                      SwitchListTile.adaptive(
                        key: const ValueKey('lan-enabled'),
                        title: const Text('设备互联'),
                        subtitle: const Text('同一局域网直连，接收端无需点击确认'),
                        value: link.enabled,
                        onChanged: _busy || link.store.locked
                            ? null
                            : (value) => _perform(() => link.setEnabled(value)),
                      ),
                      ListTile(
                        leading: Icon(lanDeviceIcon(link.kind)),
                        title: Text(link.deviceName),
                        subtitle: Text('本机用户 · ' + link.store.profile.name),
                        trailing: IconButton(
                          tooltip: '修改设备名称',
                          onPressed: _busy ? null : _rename,
                          icon: const Icon(Icons.edit_outlined),
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 12),
                Card(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      ListTile(
                        leading: Icon(
                          remote == null
                              ? Icons.devices_rounded
                              : lanDeviceIcon(remote.peer.kind),
                        ),
                        title: Text(
                          remote?.peer.name ?? link.connectionLabel,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        ),
                        subtitle: Text(
                          remote == null
                              ? '仅发现一台设备时自动连接，多台时由发起端选择'
                              : '对方用户 · ' + remote.user,
                        ),
                        trailing: remote == null
                            ? null
                            : PopupMenuButton<String>(
                                tooltip: '设备操作',
                                onSelected: (value) => _perform(
                                  () => value == 'forget'
                                      ? link.forget(remote.peer)
                                      : link.disconnect(),
                                ),
                                itemBuilder: (_) => const [
                                  PopupMenuItem(
                                    value: 'disconnect',
                                    child: Text('断开连接'),
                                  ),
                                  PopupMenuItem(
                                    value: 'forget',
                                    child: Text('忘记配对'),
                                  ),
                                ],
                              ),
                      ),
                      Padding(
                        padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                        child: OutlinedButton.icon(
                          icon: const Icon(Icons.devices_other_rounded),
                          label: Text(remote == null ? '选择设备' : '切换设备'),
                          onPressed: _busy || link.store.locked
                              ? null
                              : () => chooseLanDevice(
                                  context,
                                  link,
                                  change: remote != null,
                                ),
                        ),
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 12),
                Card(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      SwitchListTile.adaptive(
                        key: const ValueKey('lan-auto-sync'),
                        title: const Text('自动同步'),
                        subtitle: const Text('同步追剧、观看状态和这些剧目的续播进度'),
                        value: link.autoSync,
                        onChanged: controlsEnabled
                            ? (value) => _perform(() => link.setAutoSync(value))
                            : null,
                      ),
                      Padding(
                        padding: const EdgeInsets.symmetric(
                          horizontal: 16,
                          vertical: 8,
                        ),
                        child: Semantics(
                          liveRegion: true,
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              if (link.syncing) const LinearProgressIndicator(),
                              const SizedBox(height: 8),
                              Text(link.syncMessage),
                              if (link.lastSync != null)
                                Text(
                                  '最近同步 · ' +
                                      MaterialLocalizations.of(
                                        context,
                                      ).formatTimeOfDay(
                                        TimeOfDay.fromDateTime(link.lastSync!),
                                      ),
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                              if (link.lastLocalCount != null)
                                Text(
                                  '本机：' + link.lastLocalCount!.label,
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                              if (link.lastRemoteCount != null)
                                Text(
                                  '对方：' + link.lastRemoteCount!.label,
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                              if (link.skipped > 0)
                                Text(
                                  '保留范围外记录：' + link.skipped.toString(),
                                  style: Theme.of(context).textTheme.bodySmall,
                                ),
                            ],
                          ),
                        ),
                      ),
                      Padding(
                        padding: const EdgeInsets.all(16),
                        child: FilledButton.icon(
                          icon: Icon(
                            link.syncing
                                ? Icons.close_rounded
                                : Icons.sync_rounded,
                          ),
                          label: Text(link.syncing ? '取消本次同步' : '立即同步'),
                          onPressed: link.syncing
                              ? _cancelling
                                    ? null
                                    : _cancel
                              : remote == null || _busy || _cancelling
                              ? null
                              : () => _perform(() => link.synchronize()),
                        ),
                      ),
                    ],
                  ),
                ),
                if (conflicts > 0)
                  Card(
                    child: ListTile(
                      leading: const Icon(Icons.compare_arrows_rounded),
                      title: Text('$conflicts 项记录待处理'),
                      subtitle: const Text('两端同时修改的内容已保留，选择要继续使用的记录'),
                      trailing: const Icon(Icons.chevron_right_rounded),
                      onTap: () => Navigator.push(
                        context,
                        MaterialPageRoute<void>(
                          builder: (_) => _LanConflicts(controller: link),
                        ),
                      ),
                    ),
                  ),
                ListTile(
                  leading: const Icon(Icons.sync_alt_rounded),
                  title: const Text('手动同步'),
                  subtitle: const Text('双向合并、覆盖对方或覆盖本机'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  enabled: remote != null && controlsEnabled,
                  onTap: remote != null && controlsEnabled ? _manual : null,
                ),
                if (_error ?? link.error case final message?)
                  Padding(
                    padding: const EdgeInsets.all(16),
                    child: Text(
                      message,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ),
                if (link.discoveryMessage.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.all(16),
                    child: Text(link.discoveryMessage),
                  ),
                if (link.local != null)
                  ExpansionTile(
                    leading: const Icon(Icons.info_outline_rounded),
                    title: const Text('本机地址与连接帮助'),
                    childrenPadding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                    children: [
                      const Text(
                        '两台设备都需开启设备互联并保持前台。发现失败时，可在另一端输入下列地址；Windows 需允许本应用通过专用网络防火墙。',
                      ),
                      for (final address in link.local!.addresses)
                        ListTile(
                          title: SelectableText(address),
                          trailing: IconButton(
                            tooltip: '复制地址',
                            icon: const Icon(Icons.copy_rounded),
                            onPressed: () =>
                                Clipboard.setData(ClipboardData(text: address)),
                          ),
                        ),
                      if (link.local!.addresses.isEmpty)
                        const Text('尚未取得局域网地址，请检查 Wi-Fi 或网线连接。'),
                    ],
                  ),
              ],
            ),
          ),
        ),
      );
    },
  );
}

class _LanDevices extends StatefulWidget {
  const _LanDevices({required this.controller, required this.change});
  final LanController controller;
  final bool change;
  @override
  State<_LanDevices> createState() => _LanDevicesState();
}

class _LanDevicesState extends State<_LanDevices> {
  final _address = TextEditingController();
  bool _busy = false;
  bool _manual = false;
  bool _selected = false;
  LanPeer? _target;
  bool _closing = false;
  String? _error;
  late final int _epoch;
  LanController get link => widget.controller;

  @override
  void initState() {
    super.initState();
    _epoch = link.store.profileEpoch;
    link.addListener(_changed);
    unawaited(_start());
  }

  Future<void> _start() async {
    try {
      await link.ensureEnabled();
      if (widget.change) await link.rescan();
      _changed();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  void _changed() {
    if (!mounted) return;
    if (link.store.profileEpoch != _epoch || link.store.locked) {
      if (!_closing) {
        _closing = true;
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) Navigator.pop(context);
        });
      }
      return;
    }
    final remote = link.connection;
    if (remote != null &&
        (_selected
            ? remote.peer.id == _target?.id && remote.peer.pin == _target?.pin
            : !widget.change) &&
        !_closing) {
      _closing = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) Navigator.pop(context, remote);
      });
    }
    setState(() {});
  }

  Future<void> _connect(LanPeer? peer) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
      _selected = true;
      _target = peer;
    });
    try {
      if (_epoch != link.store.profileEpoch || link.store.locked)
        throw StateError('当前用户已变更');
      final selected = peer ?? await link.probe(_address.text);
      if (!mounted || _epoch != link.store.profileEpoch || link.store.locked)
        return;
      _target = selected;
      await link.connect(selected);
      _changed();
    } catch (error) {
      if (mounted)
        setState(() {
          _error = error.toString();
          _selected = false;
          _target = null;
        });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _forget(LanPeer peer) async {
    if (_busy || _epoch != link.store.profileEpoch || link.store.locked) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await link.forget(peer);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    link.removeListener(_changed);
    _address.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final peers = [...link.peers];
    final remembered = link.remembered;
    if (remembered != null && !peers.any((peer) => peer.id == remembered.id))
      peers.add(remembered);
    return SafeArea(
      top: false,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 8, 12, 8),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    widget.change ? '切换设备' : '连接设备',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
                IconButton(
                  tooltip: '重新搜索',
                  onPressed: _busy ? null : link.rescan,
                  icon: const Icon(Icons.refresh_rounded),
                ),
                IconButton(
                  tooltip: '关闭',
                  onPressed: () => Navigator.pop(context),
                  icon: const Icon(Icons.close_rounded),
                ),
              ],
            ),
          ),
          if (link.connecting || _busy) const LinearProgressIndicator(),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              children: [
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 8),
                  child: Text('只发现一台设备时自动连接；多台时选择接收设备，对方无需确认。'),
                ),
                if (peers.isEmpty)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 28),
                    child: Column(
                      children: [
                        const Icon(Icons.wifi_find_rounded, size: 48),
                        const SizedBox(height: 12),
                        Text(link.receiving ? '正在发现同一网络中的设备' : '正在开启设备互联'),
                        const SizedBox(height: 8),
                        const Text(
                          '请让另一台设备也开启设备互联并保持应用在前台。',
                          textAlign: TextAlign.center,
                        ),
                      ],
                    ),
                  ),
                for (final peer in peers)
                  Card(
                    child: ListTile(
                      autofocus: peers.length == 1,
                      minVerticalPadding: 16,
                      leading: Icon(lanDeviceIcon(peer.kind)),
                      title: Text(
                        peer.name,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                      subtitle: Text(
                        link.connection?.peer.id == peer.id
                            ? '已连接'
                            : link.peers.any((found) => found.id == peer.id)
                            ? '可连接'
                            : '上次连接的设备',
                      ),
                      trailing: PopupMenuButton<String>(
                        tooltip: '设备操作',
                        enabled: !_busy && !link.connecting,
                        onSelected: (_) => _forget(peer),
                        itemBuilder: (_) => const [
                          PopupMenuItem(value: 'forget', child: Text('忘记配对')),
                        ],
                      ),
                      onTap: _busy || link.connecting
                          ? null
                          : () => _connect(peer),
                    ),
                  ),
                if (_error ?? link.error case final message?)
                  Padding(
                    padding: const EdgeInsets.all(12),
                    child: Text(
                      message,
                      style: TextStyle(
                        color: Theme.of(context).colorScheme.error,
                      ),
                    ),
                  ),
                if (link.discoveryMessage.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.all(12),
                    child: Text(link.discoveryMessage),
                  ),
                TextButton.icon(
                  onPressed: () => setState(() => _manual = !_manual),
                  icon: const Icon(Icons.keyboard_rounded),
                  label: const Text('输入地址连接'),
                ),
                if (_manual) ...[
                  TextField(
                    controller: _address,
                    autocorrect: false,
                    enableSuggestions: false,
                    decoration: const InputDecoration(
                      labelText: '对方显示的本机地址',
                      hintText: '192.168.1.8:53120',
                    ),
                    onSubmitted: (_) => _connect(null),
                  ),
                  const SizedBox(height: 12),
                  FilledButton(
                    onPressed: _busy ? null : () => _connect(null),
                    child: const Text('连接'),
                  ),
                ],
                const SizedBox(height: 24),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _LanManual extends StatefulWidget {
  const _LanManual({required this.controller});
  final LanController controller;
  @override
  State<_LanManual> createState() => _LanManualState();
}

class _LanManualState extends State<_LanManual> {
  LanSyncMode _mode = LanSyncMode.merge;
  LanPreview? _preview;
  bool _busy = false;
  bool _cancelling = false;
  late final int _epoch;
  late final LanConnection? _connection;
  String? _error;
  LanController get link => widget.controller;

  @override
  void initState() {
    super.initState();
    _epoch = link.store.profileEpoch;
    _connection = link.connection;
  }

  Future<void> _cancel() async {
    if (_cancelling) return;
    setState(() => _cancelling = true);
    try {
      await link.cancelSync();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _cancelling = false);
    }
  }

  @override
  void dispose() {
    if (_busy && _epoch == link.store.profileEpoch) {
      unawaited(link.cancelSync().catchError((Object _) {}));
    }
    super.dispose();
  }

  Future<void> _submit() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      if (_epoch != link.store.profileEpoch ||
          link.store.locked ||
          link.connection != _connection) {
        throw StateError('设备或用户已变更，请关闭面板后重新操作');
      }
      if (_preview == null) {
        final preview = await link.preview(_mode);
        if (mounted) setState(() => _preview = preview);
      } else {
        await link.applyPreview(_preview!);
        if (mounted) Navigator.pop(context);
      }
    } catch (error) {
      if (mounted)
        setState(() {
          _error = error.toString();
          _preview = null;
        });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final name = link.connection?.peer.name ?? '对方';
    return PopScope<void>(
      canPop: !_busy && !_cancelling,
      child: SafeArea(
        top: false,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 12, 12, 8),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      '手动同步',
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                  ),
                  IconButton(
                    tooltip: '关闭',
                    onPressed: _busy ? null : () => Navigator.pop(context),
                    icon: const Icon(Icons.close_rounded),
                  ),
                ],
              ),
            ),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.all(16),
                children: [
                  for (final mode in LanSyncMode.values)
                    Card(
                      color: _mode == mode
                          ? Theme.of(context).colorScheme.secondaryContainer
                          : null,
                      child: Semantics(
                        checked: _mode == mode,
                        inMutuallyExclusiveGroup: true,
                        child: ListTile(
                          autofocus: mode == LanSyncMode.merge,
                          minVerticalPadding: 16,
                          leading: Icon(
                            _mode == mode
                                ? Icons.radio_button_checked_rounded
                                : Icons.radio_button_unchecked_rounded,
                          ),
                          title: Text(mode.label),
                          subtitle: Text(switch (mode) {
                            LanSyncMode.merge => '本机 ↔ $name · 合并双方记录',
                            LanSyncMode.push => '本机 → $name · 以本机记录替换对方',
                            LanSyncMode.pull => '$name → 本机 · 以对方记录替换本机',
                          }),
                          onTap: _busy
                              ? null
                              : () => setState(() {
                                  _mode = mode;
                                  _preview = null;
                                }),
                        ),
                      ),
                    ),
                  if (_busy)
                    const Padding(
                      padding: EdgeInsets.all(16),
                      child: LinearProgressIndicator(),
                    ),
                  if (_preview case final preview?)
                    Card(
                      child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              '同步预览',
                              style: Theme.of(context).textTheme.titleMedium,
                            ),
                            const SizedBox(height: 12),
                            Text('本机 · ' + link.store.profile.name),
                            Text(preview.localCount.label),
                            const SizedBox(height: 12),
                            Text(name + ' · ' + preview.connection.user),
                            Text(preview.remoteCount.label),
                            if (preview.skipped > 0)
                              Padding(
                                padding: const EdgeInsets.only(top: 12),
                                child: Text(
                                  '双方范围外的 ' +
                                      preview.skipped.toString() +
                                      ' 条记录会保留',
                                ),
                              ),
                            if (_mode != LanSyncMode.merge)
                              Padding(
                                padding: const EdgeInsets.only(top: 16),
                                child: Text(
                                  _mode == LanSyncMode.push
                                      ? '将覆盖 $name 的当前用户记录；移除 ' +
                                            preview.remoteCount.removed
                                                .toString() +
                                            ' 部追剧。'
                                      : '将覆盖本机当前用户记录；移除 ' +
                                            preview.localCount.removed
                                                .toString() +
                                            ' 部追剧。',
                                ),
                              ),
                          ],
                        ),
                      ),
                    ),
                  if (_error != null)
                    Padding(
                      padding: const EdgeInsets.all(12),
                      child: Text(
                        _error!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.all(16),
              child: _busy || _cancelling
                  ? OutlinedButton.icon(
                      onPressed: _cancelling ? null : _cancel,
                      icon: const Icon(Icons.close_rounded),
                      label: Text(_cancelling ? '正在取消' : '取消本次同步'),
                    )
                  : FilledButton(
                      onPressed: _submit,
                      child: Text(
                        _preview == null
                            ? '查看预览'
                            : _mode == LanSyncMode.merge
                            ? '开始同步'
                            : _mode.label,
                      ),
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

class _LanConflicts extends StatelessWidget {
  const _LanConflicts({required this.controller});
  final LanController controller;

  String _value(String field, Object? value) {
    if (field == 'member') return value == true ? '保留追剧' : '取消追剧';
    if (value == null) return '清空续播进度';
    final row = lanMap(value);
    if (field == 'status') {
      return switch (row['status']) {
        'planned' => '想看',
        'watching' => '在看',
        _ => row['manual'] == true ? '已看 · 手动标记' : '已看',
      };
    }
    return '第 ' +
        row['episode'].toString() +
        ' 集 · ' +
        formatPosition((row['position'] as num).toDouble());
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: controller.store,
    builder: (context, _) {
      final records = controller.store.locked
          ? <LanRecord>[]
          : controller.store.lanDocument.records.values
                .where(
                  (record) =>
                      record.conflicts > 0 &&
                      controller.store.allowsSource(record.drama.source),
                )
                .toList();
      return Scaffold(
        appBar: AppBar(title: const Text('处理记录冲突')),
        body: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: records.isEmpty
                ? const Center(child: Text('没有待处理的冲突'))
                : ListView.builder(
                    padding: const EdgeInsets.all(16),
                    itemCount: records.length,
                    itemBuilder: (context, index) {
                      final record = records[index];
                      return Card(
                        child: Padding(
                          padding: const EdgeInsets.all(16),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                record.drama.title,
                                style: Theme.of(context).textTheme.titleMedium,
                              ),
                              for (final field in record.fields.entries.where(
                                (field) => field.value.conflict,
                              )) ...[
                                Padding(
                                  padding: const EdgeInsets.only(
                                    top: 16,
                                    bottom: 4,
                                  ),
                                  child: Text(switch (field.key) {
                                    'member' => '追剧列表',
                                    'status' => '观看状态',
                                    _ => '续播进度',
                                  }),
                                ),
                                for (final candidate in field.value.values)
                                  ListTile(
                                    leading: const Icon(
                                      Icons.radio_button_unchecked_rounded,
                                    ),
                                    title: Text(
                                      _value(field.key, candidate.value),
                                    ),
                                    subtitle: const Text('使用这条记录'),
                                    onTap: () =>
                                        saveUserChange(context, () async {
                                          await controller.store
                                              .resolveLanConflict(
                                                record.id,
                                                field.key,
                                                candidate,
                                                record.hash,
                                              );
                                          controller.flush();
                                        }),
                                  ),
                              ],
                            ],
                          ),
                        ),
                      );
                    },
                  ),
          ),
        ),
      );
    },
  );
}

class LanPushButton extends StatelessWidget {
  const LanPushButton({super.key, required this.onPressed});
  final VoidCallback? onPressed;
  @override
  Widget build(BuildContext context) {
    final link = LanController.current;
    if (link == null) return const SizedBox.shrink();
    return AnimatedBuilder(
      animation: link,
      builder: (context, _) => IconButton(
        tooltip: link.connection == null
            ? '推送'
            : '推送到 ' + link.connection!.peer.name,
        onPressed: onPressed,
        constraints: const BoxConstraints.tightFor(width: 36, height: 36),
        padding: EdgeInsets.zero,
        iconSize: 22,
        icon: link.pushing
            ? const SizedBox(
                width: 22,
                height: 22,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : Icon(
                link.connection == null
                    ? Icons.cast_rounded
                    : Icons.cast_connected_rounded,
              ),
      ),
    );
  }
}

Future<void> showLanPush(
  BuildContext context,
  LanController link, {
  required LanPlaybackIntent Function() snapshot,
  required bool Function() stillCurrent,
  required Future<void> Function() onAccepted,
}) async {
  final remote = await chooseLanDevice(context, link);
  if (remote == null || !context.mounted || !stillCurrent()) return;
  await _lanPanel<void>(
    context,
    _LanPushPanel(
      controller: link,
      snapshot: snapshot,
      stillCurrent: stillCurrent,
      onAccepted: onAccepted,
    ),
    maxHeight: 560,
  );
  if (link.pushing) await link.cancelPush();
}

class _LanPushPanel extends StatefulWidget {
  const _LanPushPanel({
    required this.controller,
    required this.snapshot,
    required this.stillCurrent,
    required this.onAccepted,
  });
  final LanController controller;
  final LanPlaybackIntent Function() snapshot;
  final bool Function() stillCurrent;
  final Future<void> Function() onAccepted;
  @override
  State<_LanPushPanel> createState() => _LanPushPanelState();
}

class _LanPushPanelState extends State<_LanPushPanel> {
  bool _done = false;
  bool _success = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _start());
  }

  Future<void> _start() async {
    if (!mounted) return;
    try {
      await widget.controller.pushPlayback(
        snapshot: widget.snapshot,
        stillCurrent: () => mounted && widget.stillCurrent(),
        onAccepted: widget.onAccepted,
        confirmReplace: (title) async {
          if (!mounted) return false;
          return await showDialog<bool>(
                context: context,
                builder: (context) => AlertDialog(
                  title: const Text('切换对方正在播放的内容？'),
                  content: Text('对方正在播放“$title”，继续后将接续本机当前分集。'),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: const Text('取消'),
                    ),
                    FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('推送播放'),
                    ),
                  ],
                ),
              ) ??
              false;
        },
      );
      _success = true;
    } catch (_) {
    } finally {
      if (mounted) setState(() => _done = true);
    }
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.controller,
    builder: (context, _) => SafeArea(
      top: false,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.all(20),
            child: Text('推送播放', style: Theme.of(context).textTheme.titleLarge),
          ),
          Expanded(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(24),
              child: Column(
                children: [
                  Icon(
                    _success
                        ? Icons.cast_connected_rounded
                        : Icons.cast_rounded,
                    size: 64,
                  ),
                  const SizedBox(height: 24),
                  Text(
                    widget.snapshot().drama.title,
                    textAlign: TextAlign.center,
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                  const SizedBox(height: 12),
                  Text(
                    '接收设备 · ' +
                        (widget.controller.connection?.peer.name ?? '连接已断开'),
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 24),
                  if (!_done) const LinearProgressIndicator(),
                  const SizedBox(height: 16),
                  Semantics(
                    liveRegion: true,
                    child: Text(
                      widget.controller.pushMessage,
                      textAlign: TextAlign.center,
                    ),
                  ),
                  if (!_done)
                    const Padding(
                      padding: EdgeInsets.only(top: 16),
                      child: Text(
                        '对方实际开播后，本机自动暂停。',
                        textAlign: TextAlign.center,
                      ),
                    ),
                ],
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.all(16),
            child: FilledButton(
              onPressed: () async {
                if (!_done) await widget.controller.cancelPush();
                if (context.mounted) Navigator.pop(context);
              },
              child: Text(_done ? '完成' : '取消推送'),
            ),
          ),
        ],
      ),
    ),
  );
}
