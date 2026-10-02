import 'dart:convert';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';

import 'local_profiles.dart';
import 'local_store.dart';
import 'models.dart';
import 'app_build.dart';
import 'remote_widgets.dart';

class ProfilesScreen extends StatefulWidget {
  const ProfilesScreen({super.key, required this.store, this.locked = false});
  final LocalStore store;
  final bool locked;
  @override
  State<ProfilesScreen> createState() => _ProfilesScreenState();
}

class _ProfilesScreenState extends State<ProfilesScreen> {
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    ensureTelevisionFocus(context);
  }

  Future<void> _recover({bool export = false, bool reload = false}) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      if (reload) {
        await widget.store.reload();
      } else if (export) {
        await FilePicker.saveFile(
          fileName: '$appSlug-recovery.json',
          bytes: Uint8List.fromList(
            utf8.encode(widget.store.exportRecoveryData()),
          ),
          mimeType: 'application/json',
        );
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
        widget.store.validateBackup(content);
        if (!mounted) return;
        final accepted = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('从备份恢复用户与记录？'),
            content: const Text(
              '将使用这份备份替换损坏的用户配置、追剧、观看记录和偏好。建议先导出原始配置；已下载视频保留。',
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('继续恢复'),
              ),
            ],
          ),
        );
        if (accepted != true || !mounted) return;
        final pin = await showDialog<String>(
          context: context,
          builder: (_) => const _PasswordDialog(name: '管理员（原配置或备份中的密码）'),
        );
        if (pin == null || !mounted) return;
        await widget.store.recoverBackup(content, pin: pin);
      }
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _switch(LocalProfile profile) async {
    String pin = '';
    if (profile.protected) {
      final value = await showDialog<String>(
        context: context,
        builder: (_) => _PasswordDialog(name: profile.name),
      );
      if (value == null || !mounted) return;
      pin = value;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.store.switchProfile(profile.id, pin: pin);
      if (mounted) Navigator.of(context).popUntil((route) => route.isFirst);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _edit([LocalProfile? profile]) async {
    await Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (_) => ProfileEditor(store: widget.store, profile: profile),
      ),
    );
    if (mounted) setState(() {});
  }

  Future<void> _setForceLogin(bool value) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.store.setForceLogin(value);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  String _permissionsLabel(LocalProfile profile) {
    if (profile.admin) return '管理员 · 全部权限';
    final sources = profile.sources
        .where(SourceSite.isAvailable)
        .map((id) => SourceSite.byId(id).name)
        .join(' / ');
    return '${sources.isEmpty ? '未开放站源' : sources} · ${profile.download ? '可下载' : '仅在线观看'}';
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: widget.store,
    builder: (_, _) => Scaffold(
      appBar: AppBar(title: Text(widget.locked ? '解锁$appName' : '用户管理')),
      body: Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 720),
          child: ListView(
            padding: const EdgeInsets.all(20),
            children: [
              if (widget.store.configurationError != null) ...[
                Text('需要恢复本地配置', style: Theme.of(context).textTheme.titleLarge),
                const SizedBox(height: 12),
                Text(widget.store.configurationError!),
                const SizedBox(height: 12),
                FilledButton.icon(
                  onPressed: _busy ? null : () => _recover(),
                  icon: const Icon(Icons.restore_rounded),
                  label: const Text('从备份恢复'),
                ),
                OutlinedButton(
                  onPressed: _busy ? null : () => _recover(export: true),
                  child: const Text('导出原始配置'),
                ),
                TextButton(
                  onPressed: _busy ? null : () => _recover(reload: true),
                  child: const Text('重新读取配置'),
                ),
              ] else ...[
                Text(
                  widget.locked
                      ? '选择用户并输入密码'
                      : '当前用户：${widget.store.profile.name}',
                  style: Theme.of(context).textTheme.titleLarge,
                ),
                const SizedBox(height: 8),
                const Text('各用户的追剧和观看记录独立保存，下载文件由本机共享。'),
                if (!widget.store.locked && widget.store.profile.admin) ...[
                  const SizedBox(height: 12),
                  SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('启动时需要登录'),
                    subtitle: Text(
                      widget.store.forceLogin
                          ? '每次打开应用先解锁当前受保护用户'
                          : '保留密码，仅切换用户或手动锁定时验证',
                    ),
                    value: widget.store.forceLogin,
                    onChanged: _busy ? null : _setForceLogin,
                  ),
                ],
              ],
              if (_busy)
                const Padding(
                  padding: EdgeInsets.all(16),
                  child: LinearProgressIndicator(),
                ),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 12),
                  child: Text(
                    _error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
              const SizedBox(height: 16),
              for (final profile
                  in widget.store.configurationError == null
                      ? widget.store.profiles
                      : <LocalProfile>[])
                Card(
                  child: ListTile(
                    leading: Icon(
                      profile.admin
                          ? Icons.admin_panel_settings_outlined
                          : Icons.person_outline,
                    ),
                    title: Text(profile.name),
                    subtitle: Text(_permissionsLabel(profile)),
                    onTap: _busy ? null : () => _switch(profile),
                    trailing: !widget.store.locked && widget.store.profile.admin
                        ? IconButton(
                            tooltip: '编辑用户',
                            onPressed: _busy ? null : () => _edit(profile),
                            icon: const Icon(Icons.edit_outlined),
                          )
                        : Icon(
                            profile.protected
                                ? Icons.lock_outline
                                : Icons.chevron_right,
                          ),
                  ),
                ),
              if (!widget.store.locked && widget.store.profile.admin) ...[
                const SizedBox(height: 16),
                FilledButton.icon(
                  onPressed: _busy ? null : () => _edit(),
                  icon: const Icon(Icons.person_add_outlined),
                  label: const Text('添加用户'),
                ),
              ],
              if (!widget.store.locked && widget.store.profile.protected)
                TextButton.icon(
                  onPressed: () {
                    Navigator.of(context).popUntil((route) => route.isFirst);
                    widget.store.lock();
                  },
                  icon: const Icon(Icons.lock_outline),
                  label: const Text('锁定当前用户'),
                ),
            ],
          ),
        ),
      ),
    ),
  );
}

class _PasswordDialog extends StatefulWidget {
  const _PasswordDialog({required this.name});
  final String name;
  @override
  State<_PasswordDialog> createState() => _PasswordDialogState();
}

class _PasswordDialogState extends State<_PasswordDialog> {
  final _pin = TextEditingController();
  @override
  void dispose() {
    _pin.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('登录 ${widget.name}'),
    content: TextField(
      controller: _pin,
      autofocus: true,
      obscureText: true,
      decoration: const InputDecoration(labelText: '密码'),
      onSubmitted: (value) => Navigator.pop(context, value),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      FilledButton(
        onPressed: () => Navigator.pop(context, _pin.text),
        child: const Text('登录'),
      ),
    ],
  );
}

class ProfileEditor extends StatefulWidget {
  const ProfileEditor({super.key, required this.store, this.profile});
  final LocalStore store;
  final LocalProfile? profile;
  @override
  State<ProfileEditor> createState() => _ProfileEditorState();
}

class _ProfileEditorState extends State<ProfileEditor> {
  late final _name = TextEditingController(text: widget.profile?.name ?? '');
  final _pin = TextEditingController(), _confirm = TextEditingController();
  late final _sources =
      (widget.profile?.sources ?? SourceSite.values.map((s) => s.id).toList())
          .toSet();
  late bool _download = widget.profile?.download ?? true;
  bool _clearPin = false, _busy = false;
  String? _error;
  @override
  void dispose() {
    _name.dispose();
    _pin.dispose();
    _confirm.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    setState(() {
      _error = null;
      _busy = true;
    });
    try {
      if (_pin.text != _confirm.text) throw StateError('两次输入的密码不一致');
      await widget.store.saveProfile(
        id: widget.profile?.id,
        name: _name.text,
        sources: _sources.toList(),
        download: _download,
        pin: _clearPin
            ? ''
            : _pin.text.isEmpty
            ? null
            : _pin.text,
      );
      if (mounted) Navigator.pop(context);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _delete() async {
    final yes = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('删除此用户？'),
        content: const Text('会同时删除该用户的追剧和观看记录。下载文件会保留。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('删除'),
          ),
        ],
      ),
    );
    if (yes != true || !mounted) return;
    try {
      await widget.store.deleteProfile(widget.profile!.id);
      if (mounted) Navigator.pop(context);
    } catch (error) {
      if (mounted) setState(() => _error = error.toString());
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(widget.profile == null ? '添加用户' : '编辑用户')),
    body: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 640),
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            TextField(
              controller: _name,
              maxLength: 40,
              decoration: const InputDecoration(labelText: '用户名'),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _pin,
              obscureText: true,
              decoration: const InputDecoration(
                labelText: '设置密码（至少 6 位）',
                helperText: '留空保留原密码',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _confirm,
              obscureText: true,
              decoration: const InputDecoration(labelText: '再次输入密码'),
            ),
            if (widget.profile?.protected == true &&
                widget.profile?.admin != true)
              CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('取消此用户密码'),
                value: _clearPin,
                onChanged: (v) => setState(() => _clearPin = v!),
              ),
            const SizedBox(height: 20),
            if (widget.profile?.admin != true) ...[
              Text('允许访问的站源', style: Theme.of(context).textTheme.titleMedium),
              for (final source in SourceSite.values)
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  controlAffinity: ListTileControlAffinity.leading,
                  title: Text(source.name),
                  value: _sources.contains(source.id),
                  onChanged: (value) {
                    setState(() {
                      if (value == true) {
                        _sources.add(source.id);
                      } else {
                        _sources.remove(source.id);
                      }
                    });
                  },
                ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('允许下载和本地媒体'),
                subtitle: Text(_download ? '可下载、合并和导出' : '仅在线观看，隐藏下载入口'),
                value: _download,
                onChanged: (v) => setState(() => _download = v),
              ),
            ] else
              const Text('管理员可以访问全部站源和功能。创建其他用户前需要先设置管理员密码。'),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 12),
                child: Text(
                  _error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
            const SizedBox(height: 24),
            FilledButton(
              onPressed: _busy ? null : _save,
              child: Text(_busy ? '正在保存…' : '保存'),
            ),
            if (widget.profile != null && !widget.profile!.admin)
              TextButton(
                onPressed: _busy ? null : _delete,
                child: const Text('删除用户'),
              ),
          ],
        ),
      ),
    ),
  );
}
