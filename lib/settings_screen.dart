import 'dart:convert';
import 'dart:io';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';

import 'core_bridge.dart';
import 'app_theme.dart';
import 'app_layout.dart';
import 'background_downloads.dart';
import 'local_store.dart';
import 'profiles_screen.dart';
import 'remote_widgets.dart';
import 'sources_screen.dart';
import 'widgets.dart';
import 'resource_settings_screen.dart';
import 'lan_screen.dart';

String storageSize(int bytes) {
  if (bytes < 0) return '暂不可用';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
  if (bytes < 1024 * 1024 * 1024) {
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
  return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(2)} GB';
}

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;
  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final _listKey = GlobalKey<RemoteListState>();
  bool _busy = false;
  String? _message;

  Future<void> _chooseTheme() async {
    if (AppLayout.isTelevision(context)) {
      final selected = await showDialog<String>(
        context: context,
        builder: (_) => TelevisionActionDialog(
          title: '外观主题',
          options: [
            for (final mode in ['light', 'dark', 'system'])
              TelevisionAction(
                value: mode,
                label: AppTheme.label(mode),
                description: mode == 'system' ? '随设备的深色模式自动切换' : null,
                icon: Icons.brightness_6_rounded,
              ),
          ],
        ),
      );
      if (selected != null && mounted) {
        await saveUserChange(
          context,
          () => widget.store.setThemeMode(selected),
        );
      }
      return;
    }
    final selected = await showDialog<String>(
      context: context,
      builder: (context) => SimpleDialog(
        title: const Text('外观主题'),
        children: [
          RadioGroup<String>(
            groupValue: widget.store.themeMode,
            onChanged: (value) => Navigator.pop(context, value),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final mode in ['light', 'dark', 'system'])
                  RadioListTile<String>(
                    value: mode,
                    title: Text(AppTheme.label(mode)),
                    subtitle: mode == 'system'
                        ? const Text('随设备的深色模式自动切换')
                        : null,
                  ),
              ],
            ),
          ),
        ],
      ),
    );
    if (selected != null && mounted) {
      await saveUserChange(context, () => widget.store.setThemeMode(selected));
    }
  }

  Future<void> _backup(bool restore) async {
    setState(() {
      _busy = true;
      _message = null;
    });
    try {
      if (!restore) {
        final content = await widget.store.exportBackup();
        final saved = await FilePicker.saveFile(
          fileName:
              '$appSlug-backup-${DateTime.now().toIso8601String().substring(0, 10)}.json',
          bytes: Uint8List.fromList(utf8.encode(content)),
          mimeType: 'application/json',
        );
        if (saved != null && mounted) setState(() => _message = '备份已保存');
      } else {
        final file = await FilePicker.pickFile(
          type: FileType.custom,
          allowedExtensions: ['json'],
        );
        if (file == null || !mounted) return;
        final size = await file.length();
        if (size == null || size > 8 * 1024 * 1024) {
          throw const FormatException('备份文件过大或无法读取');
        }
        final content = utf8.decode(await file.readAsBytes());
        final data = widget.store.validateBackup(content);
        if (!mounted) return;
        final summary =
            '包含 ${(data['profiles'] as List).length} 个用户。将替换本机的用户、追剧、观看记录和偏好设置；已下载视频保留。恢复后使用备份内的管理员密码登录。';
        final bool accepted;
        if (AppLayout.isTelevision(context)) {
          accepted =
              await showDialog<String>(
                context: context,
                builder: (_) => TelevisionActionDialog(
                  title: '恢复备份？',
                  options: [
                    TelevisionAction(
                      value: 'restore',
                      label: '恢复这套备份',
                      description: summary,
                      icon: Icons.restore_rounded,
                    ),
                  ],
                ),
              ) ==
              'restore';
        } else {
          accepted =
              await showDialog<bool>(
                context: context,
                builder: (context) => AlertDialog(
                  title: const Text('恢复备份？'),
                  content: Text(summary),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: const Text('取消'),
                    ),
                    FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('恢复'),
                    ),
                  ],
                ),
              ) ??
              false;
        }
        if (!accepted || !mounted) return;
        await widget.store.importBackup(content);
        if (mounted) Navigator.of(context).popUntil((route) => route.isFirst);
      }
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _toggleAutoExport() async {
    try {
      if (!widget.store.autoExport) {
        await BackgroundDownloads.ensureStarted();
      }
      await widget.store.setAutoExport(!widget.store.autoExport);
    } catch (error) {
      if (mounted) setState(() => _message = error.toString());
    }
  }

  Future<void> _toggleExportPosters() => saveUserChange(
    context,
    () => widget.store.setExportPosters(!widget.store.exportPosters),
  );

  List<({String id, IconData icon, String title, String subtitle, VoidCallback? onPressed})>
  _televisionEntries() => [
    (
      id: 'lan',
      icon: Icons.devices_rounded,
      title: '设备互联',
      subtitle: '局域网自动同步追剧与推送播放',
      onPressed: () => openLanSync(context),
    ),
    if (widget.repository.supportsSourceManagement)
      (
        id: 'sources',
        icon: Icons.dns_outlined,
        title: '站源管理',
        subtitle: '独立更新、连接与播放检测',
        onPressed: () => Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (_) => SourcesScreen(
              repository: widget.repository,
              store: widget.store,
            ),
          ),
        ),
      ),
    (
      id: 'theme',
      icon: Icons.palette_outlined,
      title: '外观主题',
      subtitle: AppTheme.label(widget.store.themeMode),
      onPressed: _chooseTheme,
    ),
    (
      id: 'profiles',
      icon: Icons.people_outline,
      title: '用户管理',
      subtitle: '当前：${widget.store.profile.name}',
      onPressed: () => Navigator.push(
        context,
        MaterialPageRoute<void>(
          builder: (_) => ProfilesScreen(store: widget.store),
        ),
      ),
    ),
    if (widget.store.canDownload)
      (
        id: 'download-preferences',
        icon: Icons.download_outlined,
        title: '下载偏好',
        subtitle: widget.store.downloadPreferences.qualityLabel,
        onPressed: () => Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (_) => DownloadPreferencesScreen(store: widget.store),
          ),
        ),
      ),
    if (widget.store.canDownload)
      (
        id: 'storage',
        icon: Icons.folder_outlined,
        title: '下载目录与空间',
        subtitle: '查看存储用量、迁移已下载文件',
        onPressed: () => Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (_) => StorageScreen(
              repository: widget.repository,
              store: widget.store,
            ),
          ),
        ),
      ),
    if (widget.store.profile.admin) ...[
      (
        id: 'resources',
        icon: Icons.settings_ethernet_rounded,
        title: '网络与资源',
        subtitle: '代理、目录请求间隔、下载并发与站源目录',
        onPressed: () => Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (_) => ResourceSettingsScreen(
              repository: widget.repository,
              store: widget.store,
            ),
          ),
        ),
      ),
      (
        id: 'auto-export',
        icon: Icons.movie_filter_outlined,
        title: '自动导出 Emby',
        subtitle: widget.store.autoExport
            ? '已开启 · 在 exports 目录生成视频与海报元数据'
            : '已关闭 · 按确认开启',
        onPressed: _busy ? null : _toggleAutoExport,
      ),
      (
        id: 'export-posters',
        icon: Icons.image_outlined,
        title: '导出海报文件',
        subtitle: widget.store.exportPosters
            ? '已开启 · 同时写入海报文件'
            : '已关闭 · 只写海报 URL',
        onPressed: _busy ? null : _toggleExportPosters,
      ),
      (
        id: 'backup',
        icon: Icons.backup_outlined,
        title: '导出配置备份',
        subtitle: '包含本地用户、追剧、历史和设置，不含视频文件',
        onPressed: _busy ? null : () => _backup(false),
      ),
      (
        id: 'restore',
        icon: Icons.restore,
        title: '恢复配置备份',
        subtitle: '从备份文件恢复用户与设置',
        onPressed: _busy ? null : () => _backup(true),
      ),
    ],
  ];

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (_, _) => Scaffold(
      appBar: AppBar(title: const Text('设置与备份')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: AppLayout.isTelevision(context)
              ? Builder(
                  builder: (_) {
                    final entries = _televisionEntries();
                    return RemoteList(
                      key: _listKey,
                      itemKeys: [for (final entry in entries) entry.id],
                      itemExtent: RemoteListTile.extent,
                      padding: const EdgeInsets.fromLTRB(12, 8, 12, 24),
                      autofocus: true,
                      itemBuilder: (_, index, node, onFocus) => RemoteListTile(
                        title: entries[index].title,
                        subtitle: entries[index].subtitle,
                        leading: Icon(entries[index].icon, size: 26),
                        focusNode: node,
                        onFocus: onFocus,
                        onPressed: entries[index].onPressed,
                      ),
                    );
                  },
                )
              : ListView(
            padding: const EdgeInsets.all(16),
            children: [
              ListTile(
                key: const ValueKey('lan-settings'),
                leading: const Icon(Icons.devices_rounded),
                title: const Text('设备互联'),
                subtitle: const Text('局域网自动同步追剧与推送播放'),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: () => openLanSync(context),
              ),
              if (widget.repository.supportsSourceManagement)
                ListTile(
                  leading: const Icon(Icons.dns_outlined),
                  title: const Text('站源管理'),
                  subtitle: const Text('独立更新、连接与播放检测'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => SourcesScreen(
                        repository: widget.repository,
                        store: widget.store,
                      ),
                    ),
                  ),
                ),
              ListTile(
                key: const ValueKey('theme-setting'),
                leading: const Icon(Icons.palette_outlined),
                title: const Text('外观主题'),
                subtitle: Text(AppTheme.label(widget.store.themeMode)),
                trailing: const Icon(Icons.chevron_right_rounded),
                onTap: _chooseTheme,
              ),
              ListTile(
                leading: const Icon(Icons.people_outline),
                title: const Text('用户管理'),
                subtitle: Text('当前：${widget.store.profile.name}'),
                onTap: () => Navigator.push(
                  context,
                  MaterialPageRoute<void>(
                    builder: (_) => ProfilesScreen(store: widget.store),
                  ),
                ),
              ),
              if (widget.store.canDownload)
                ListTile(
                  leading: const Icon(Icons.download_outlined),
                  title: const Text('下载偏好'),
                  subtitle: Text(widget.store.downloadPreferences.qualityLabel),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) =>
                          DownloadPreferencesScreen(store: widget.store),
                    ),
                  ),
                ),
              if (widget.store.canDownload)
                ListTile(
                  leading: const Icon(Icons.folder_outlined),
                  title: const Text('下载目录与空间'),
                  subtitle: const Text('查看存储用量、迁移已下载文件'),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => StorageScreen(
                        repository: widget.repository,
                        store: widget.store,
                      ),
                    ),
                  ),
                ),
              if (widget.store.profile.admin) ...[
                ListTile(
                  leading: const Icon(Icons.settings_ethernet_rounded),
                  title: const Text('网络与资源'),
                  subtitle: const Text('代理、目录请求间隔、下载并发与站源目录'),
                  trailing: const Icon(Icons.chevron_right_rounded),
                  onTap: () => Navigator.push(
                    context,
                    MaterialPageRoute<void>(
                      builder: (_) => ResourceSettingsScreen(
                        repository: widget.repository,
                        store: widget.store,
                      ),
                    ),
                  ),
                ),
                SwitchListTile(
                  value: widget.store.autoExport,
                  title: const Text('下载完成后自动导出 Emby'),
                  subtitle: const Text(
                    '在下载目录的 exports 中生成视频和海报 URL 元数据，可将该目录加入 Emby 媒体库。',
                  ),
                  onChanged: _busy
                      ? null
                      : (value) async {
                          try {
                            if (value) {
                              await BackgroundDownloads.ensureStarted();
                            }
                            await widget.store.setAutoExport(value);
                          } catch (error) {
                            if (mounted) {
                              setState(() => _message = error.toString());
                            }
                          }
                        },
                ),
                SwitchListTile(
                  value: widget.store.exportPosters,
                  title: const Text('同时导出海报文件'),
                  subtitle: const Text('默认只写海报 URL。源站海报需要解密或外部读取失败时可开启。'),
                  onChanged: _busy
                      ? null
                      : (value) => saveUserChange(
                          context,
                          () => widget.store.setExportPosters(value),
                        ),
                ),
                ListTile(
                  leading: const Icon(Icons.backup_outlined),
                  title: const Text('导出配置备份'),
                  subtitle: const Text('包含本地用户、追剧、历史和设置，不含视频文件'),
                  onTap: _busy ? null : () => _backup(false),
                ),
                ListTile(
                  leading: const Icon(Icons.restore),
                  title: const Text('恢复配置备份'),
                  onTap: _busy ? null : () => _backup(true),
                ),
              ],
              if (Platform.isIOS)
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: Text('iOS 下载和媒体处理需要保持应用在前台；切到后台会暂停，回到前台后可继续。'),
                ),
              if (_busy) const LinearProgressIndicator(),
              if (_message != null)
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: SelectableText(_message!),
                ),
            ],
          ),
        ),
      ),
    ),
  );
}

class StorageScreen extends StatefulWidget {
  const StorageScreen({
    super.key,
    required this.repository,
    required this.store,
  });
  final AppRepository repository;
  final LocalStore store;
  @override
  State<StorageScreen> createState() => _StorageScreenState();
}

class _StorageScreenState extends State<StorageScreen> {
  Map<String, dynamic>? _info;
  String? _error;
  bool _busy = false;
  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final info = await widget.repository.storage();
      if (mounted) {
        setState(() {
          _info = info;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  Future<void> _move() async {
    String? parent;
    if (Platform.isAndroid || Platform.isIOS) {
      final support = await getApplicationSupportDirectory();
      final directories = <String, String>{support.path: '应用内部存储'};
      if (Platform.isAndroid) {
        final external = await getExternalStorageDirectories() ?? [];
        for (var i = 0; i < external.length; i++) {
          directories[external[i].path] = i == 0
              ? '设备共享存储（应用目录）'
              : 'SD 卡 ${i + 1}（应用目录）';
        }
      } else {
        final documents = await getApplicationDocumentsDirectory();
        directories[documents.path] = '文件 App 可见目录';
      }
      if (!mounted) return;
      parent = await showDialog<String>(
        context: context,
        builder: (context) => SimpleDialog(
          title: const Text('选择下载位置'),
          children: [
            for (final entry in directories.entries)
              SimpleDialogOption(
                onPressed: () => Navigator.pop(context, entry.key),
                child: Text(entry.value),
              ),
          ],
        ),
      );
    } else {
      parent = await FilePicker.getDirectoryPath(dialogTitle: '选择下载保存位置');
    }
    if (parent == null || !mounted) return;
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('迁移已下载内容？'),
        content: Text(
          '将视频、合并成品及导出内容迁移到：\n$parent\n\n下载会先暂停，复制成功后清理旧目录。请保证目标有足够空间，并在完成后继续下载。',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('迁移'),
          ),
        ],
      ),
    );
    if (yes != true || !mounted) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.repository.moveDownloads(parent);
      await _refresh();
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => PopScope(
    canPop: !_busy,
    child: Scaffold(
      appBar: AppBar(title: const Text('下载目录与空间')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              if (_info != null) ...[
                Text(
                  '已使用 ${storageSize((_info!['bytes'] as num?)?.toInt() ?? 0)}',
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
                const SizedBox(height: 8),
                Text(
                  '剩余 ${storageSize((_info!['free'] as num?)?.toInt() ?? -1)} · ${_info!['files'] ?? 0} 个文件',
                ),
                const SizedBox(height: 24),
                const Text('当前下载目录'),
                const SizedBox(height: 8),
                SelectableText(_info!['directory'] as String? ?? ''),
                TextButton.icon(
                  onPressed: () => Clipboard.setData(
                    ClipboardData(text: _info!['directory'] as String? ?? ''),
                  ),
                  icon: const Icon(Icons.copy),
                  label: const Text('复制路径'),
                ),
                const SizedBox(height: 20),
                const Text('在下载页删除不需要的分集，在本地媒体页删除合并成品或 Emby 导出，可释放空间。'),
              ],
              if (_busy || (_info == null && _error == null))
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 20),
                  child: LinearProgressIndicator(),
                ),
              if (_busy) const Text('正在迁移，请保持应用运行…'),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 16),
                  child: Text(_error!),
                ),
              if (widget.store.profile.admin)
                FilledButton.icon(
                  onPressed: _busy ? null : _move,
                  icon: const Icon(Icons.drive_file_move_outline),
                  label: const Text('更改并迁移目录'),
                ),
              TextButton(
                onPressed: _busy ? null : _refresh,
                child: const Text('刷新用量'),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}
