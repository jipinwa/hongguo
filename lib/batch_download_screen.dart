import 'dart:async';

import 'package:flutter/material.dart';

import 'batch_downloads.dart';
import 'core_bridge.dart';
import 'downloads_screen.dart';
import 'local_store.dart';
import 'models.dart';
import 'remote_widgets.dart';
import 'widgets.dart';

class BatchDownloadScreen extends StatefulWidget {
  const BatchDownloadScreen({
    super.key,
    required this.repository,
    required this.store,
    required this.dramas,
  });
  final AppRepository repository;
  final LocalStore store;
  final List<Drama> dramas;

  @override
  State<BatchDownloadScreen> createState() => _BatchDownloadScreenState();
}

class _BatchDownloadScreenState extends State<BatchDownloadScreen> {
  late final BatchDownloads _batch;

  @override
  void initState() {
    super.initState();
    ensureTelevisionFocus(context);
    _batch = BatchDownloads(widget.repository, widget.store, widget.dramas);
    unawaited(_batch.prepare());
  }

  @override
  void dispose() {
    _batch.dispose();
    super.dispose();
  }

  String _subtitle(BatchDownloadItem item) {
    if (item.loading) return '正在读取分集';
    if (item.error != null) return item.error!;
    if (item.detail == null) return '等待读取分集';
    final count = item.episodes(_batch.includeVip).length;
    final vip = item.detail!.episodes.where((episode) => episode.vip).length;
    if (count == 0) return '没有非 VIP 分集，可手动选择包含 VIP';
    final pieces = <String>[
      '已选 $count 集',
      if (!_batch.includeVip && vip > 0) '排除 $vip 集 VIP',
      if (item.added > 0) '新增 ${item.added} 集',
      if (item.existing > 0) '已有 ${item.existing} 集',
      if (_batch.settingsLocked && item.pending(_batch.includeVip).isEmpty)
        '已加入队列',
      if (item.detail!.warning.isNotEmpty) item.detail!.warning,
    ];
    return pieces.join(' · ');
  }

  void _openDownloads() => Navigator.push<void>(
    context,
    MaterialPageRoute(
      builder: (_) =>
          DownloadsScreen(repository: widget.repository, store: widget.store),
    ),
  );

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: Listenable.merge([_batch, widget.store]),
    builder: (context, _) => Scaffold(
      appBar: AppBar(title: const Text('批量下载')),
      body: SafeArea(
        top: false,
        child: !_batch.hasAccess
            ? const StatusPanel(title: '下载权限已变更', message: '请返回后重新选择短剧。')
            : Center(
                child: ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 1000),
                  child: Column(
                    children: [
                      if (_batch.busy)
                        const LinearProgressIndicator(minHeight: 2),
                      Expanded(
                        child: ListView(
                          padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
                          children: [
                            Text(
                              '${_batch.items.length} 部短剧',
                              style: Theme.of(context).textTheme.titleLarge,
                            ),
                            const SizedBox(height: 8),
                            const Text('先读取分集，再加入下载。默认选择非 VIP 集，已有任务自动跳过。'),
                            const SizedBox(height: 12),
                            DropdownButtonFormField<int>(
                              key: const ValueKey('batch-download-quality'),
                              value: _batch.quality,
                              isExpanded: true,
                              decoration: const InputDecoration(
                                labelText: '下载画质',
                              ),
                              onChanged: _batch.busy || _batch.settingsLocked
                                  ? null
                                  : (value) => _batch.setQuality(value ?? 0),
                              items: [
                                const DropdownMenuItem(
                                  value: 0,
                                  child: Text('自动 · 优先高清'),
                                ),
                                for (final quality in [1080, 720, 480])
                                  DropdownMenuItem(
                                    value: quality,
                                    child: Text('${quality}P'),
                                  ),
                              ],
                            ),
                            CheckboxListTile(
                              contentPadding: EdgeInsets.zero,
                              title: const Text('包含 VIP 分集'),
                              subtitle: const Text('VIP 集可能只能下载试看内容'),
                              value: _batch.includeVip,
                              onChanged: _batch.busy || _batch.settingsLocked
                                  ? null
                                  : (value) =>
                                        _batch.setIncludeVip(value ?? false),
                            ),
                            if (_batch.warning.isNotEmpty)
                              Padding(
                                padding: const EdgeInsets.only(bottom: 12),
                                child: Text(
                                  _batch.warning,
                                  style: TextStyle(
                                    color: Theme.of(context).colorScheme.error,
                                  ),
                                ),
                              ),
                            if (_batch.current != null)
                              Padding(
                                padding: const EdgeInsets.only(bottom: 12),
                                child: Text(
                                  '${_batch.submitting ? '正在添加' : '正在读取'}：${_batch.current!.drama.title}',
                                ),
                              ),
                            for (final item in _batch.items)
                              Card(
                                child: CheckboxListTile(
                                  key: ValueKey('batch-drama-${item.drama.id}'),
                                  value: item.selected,
                                  onChanged:
                                      _batch.busy || _batch.settingsLocked
                                      ? null
                                      : (value) =>
                                            _batch.select(item, value ?? false),
                                  title: Text(
                                    item.detail?.drama.title ??
                                        item.drama.title,
                                  ),
                                  subtitle: Text(
                                    _subtitle(item),
                                    style: item.error == null
                                        ? null
                                        : TextStyle(
                                            color: Theme.of(
                                              context,
                                            ).colorScheme.error,
                                          ),
                                  ),
                                ),
                              ),
                          ],
                        ),
                      ),
                      Padding(
                        padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            Text(
                              [
                                '新增 ${_batch.added} 集',
                                '已有 ${_batch.existing} 集',
                                if (_batch.failures > 0)
                                  '${_batch.failures} 部需重试',
                                if (_batch.stopped) '已停止后续添加，已加入任务继续下载',
                              ].join(' · '),
                            ),
                            const SizedBox(height: 8),
                            Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              alignment: WrapAlignment.end,
                              children: [
                                if (_batch.busy)
                                  OutlinedButton(
                                    onPressed: _batch.stopping
                                        ? null
                                        : _batch.stop,
                                    child: Text(
                                      _batch.stopping
                                          ? '正在停止'
                                          : _batch.submitting
                                          ? '停止添加'
                                          : '停止读取',
                                    ),
                                  )
                                else ...[
                                  if (_batch.unread > 0)
                                    OutlinedButton(
                                      onPressed: _batch.prepare,
                                      child: const Text('读取剩余 / 重试'),
                                    ),
                                  if (_batch.added + _batch.existing > 0 &&
                                      widget.store.canDownload)
                                    TextButton(
                                      onPressed: _openDownloads,
                                      child: const Text('查看下载'),
                                    ),
                                  FilledButton.icon(
                                    key: const ValueKey(
                                      'submit-batch-downloads',
                                    ),
                                    onPressed:
                                        !widget.store.canDownload ||
                                            _batch.pendingEpisodes == 0
                                        ? null
                                        : _batch.submit,
                                    icon: const Icon(Icons.download_rounded),
                                    label: Text(
                                      '${_batch.settingsLocked ? '继续添加' : '加入下载'} · ${_batch.pendingEpisodes} 集',
                                    ),
                                  ),
                                ],
                              ],
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ),
      ),
    ),
  );
}
