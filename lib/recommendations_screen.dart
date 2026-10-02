import 'dart:async';

import 'package:flutter/material.dart';

import 'catalog_filters.dart';
import 'app_layout.dart';
import 'core_bridge.dart';
import 'local_store.dart';
import 'models.dart';
import 'playback_launch_screen.dart';
import 'remote_widgets.dart';
import 'widgets.dart';

class RecommendationsScreen extends StatefulWidget {
  const RecommendationsScreen({
    super.key,
    required this.repository,
    required this.store,
    this.embedded = false,
    this.gridKey,
    this.onExitLeft,
  });
  final AppRepository repository;
  final LocalStore store;
  final bool embedded;
  final GlobalKey<RemoteGridState>? gridKey;
  final VoidCallback? onExitLeft;

  @override
  State<RecommendationsScreen> createState() => _RecommendationsScreenState();
}

class _RecommendationsScreenState extends State<RecommendationsScreen> {
  static const _genres = [
    CatalogCategory('short_play', '真人剧'),
    CatalogCategory('comic_series', '漫剧'),
    CatalogCategory('ai_series', 'AI 剧'),
  ];
  final _scroll = ScrollController();
  String _genre = 'short_play';
  List<Drama> _items = [];
  bool _loading = false;
  bool _more = false;
  bool _hasMore = true;
  bool _failedMore = false;
  String? _error;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    widget.repository.catalogUpdates.addListener(_metadataChanged);
    _load();
  }

  @override
  void dispose() {
    _generation++;
    widget.repository.catalogUpdates.removeListener(_metadataChanged);
    unawaited(widget.repository.cancelRecommendations());
    _scroll.dispose();
    super.dispose();
  }

  void _metadataChanged() {
    final drama = widget.repository.catalogUpdates.latest;
    if (!mounted || drama == null) return;
    setState(() {
      _items = [
        for (final item in _items)
          item.id == drama.id ? item.merge(drama) : item,
      ];
    });
  }

  Future<void> _load({bool more = false, bool force = false}) async {
    if (more && (_loading || _more || !_hasMore)) return;
    final generation = ++_generation;
    final genre = _genre;
    setState(() {
      _loading = !more;
      _more = more;
      _error = null;
      _failedMore = more;
    });
    try {
      await widget.repository.cancelRecommendations();
      if (!mounted || generation != _generation) return;
      final page = await widget.repository.recommendations(
        genre,
        more: more,
        force: force,
      );
      if (!mounted || generation != _generation) return;
      setState(() {
        _items = page.items;
        _hasMore = page.hasMore;
        _error = page.warning.isEmpty ? null : page.warning;
        _loading = _more = false;
      });
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _loading = _more = false;
        _error = error.toString();
      });
    }
  }

  void _select(String genre) {
    if (_genre == genre) return;
    setState(() {
      _genre = genre;
      _items = [];
      _hasMore = true;
    });
    if (_scroll.hasClients) _scroll.jumpTo(0);
    _load();
  }

  @override
  Widget build(BuildContext context) {
    final refresh = RefreshAction(
      loading: _loading || _more,
      tooltip: '刷新推荐',
      onPressed: () => _load(force: true),
    );
    final content = Column(
      children: [
        CatalogFilters(
          categories: _genres,
          category: _genre,
          onCategory: _select,
          onRetry: () => _load(force: true),
          onExitDown: () => widget.gridKey?.currentState?.focusCurrent(),
          trailing: widget.embedded ? refresh : null,
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
          child: Align(
            alignment: Alignment.centerLeft,
            child: Text(
              '公开推荐 · 已加载 ${_items.length} 部',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ),
        ),
        if (_loading && _items.isNotEmpty)
          const LinearProgressIndicator(minHeight: 2),
        if (_error != null && _items.isNotEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    _error!,
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.error,
                    ),
                  ),
                ),
                TextButton(
                  onPressed: _loading || _more
                      ? null
                      : () => _load(more: _failedMore, force: !_failedMore),
                  child: const Text('重试'),
                ),
              ],
            ),
          ),
        Expanded(
          child: !widget.store.allowsSource('hongguo')
              ? const StatusPanel(title: '当前用户未开放红果', message: '可在用户管理中调整站源权限。')
              : _loading && _items.isEmpty
              ? const Center(child: CircularProgressIndicator())
              : _items.isEmpty
              ? StatusPanel(
                  title: _error == null ? '暂无推荐' : '推荐暂时无法加载',
                  message: _error ?? '稍后刷新可获取新的推荐。',
                  onRetry: () => _load(force: true),
                )
              : LayoutBuilder(
                  builder: (context, constraints) {
                    if (AppLayout.isTelevision(context)) {
                      final columns = ((constraints.maxWidth - 36) / 150)
                          .floor()
                          .clamp(1, 8);
                      final tileWidth =
                          (constraints.maxWidth - 36 - (columns - 1) * 14) /
                          columns;
                      return RemoteGrid(
                        key: widget.gridKey,
                        itemKeys: _items.map((item) => item.id).toList(),
                        columns: columns,
                        itemExtent:
                            DramaTile.extentFor(context, tileWidth - 14) + 14,
                        controller: _scroll,
                        padding: const EdgeInsets.fromLTRB(18, 2, 18, 18),
                        onExitLeft: widget.onExitLeft,
                        footer: Padding(
                          padding: const EdgeInsets.fromLTRB(18, 0, 18, 24),
                          child: Center(
                            child: _more
                                ? const CircularProgressIndicator()
                                : _hasMore
                                ? RemoteButton(
                                    label: '继续推荐',
                                    icon: Icons.auto_awesome_rounded,
                                    onPressed: () => _load(more: true),
                                  )
                                : RemoteButton(
                                    label: '刷新获取新推荐',
                                    icon: Icons.refresh_rounded,
                                    onPressed: () => _load(force: true),
                                  ),
                          ),
                        ),
                        itemBuilder: (_, index, node, onFocus) {
                          final drama = _items[index];
                          return DramaTile(
                            key: ValueKey(drama.id),
                            drama: drama,
                            repository: widget.repository,
                            focusNode: node,
                            onFocus: onFocus,
                            onTap: () => unawaited(
                              openPlaybackDirectly(
                                context,
                                drama: drama,
                                repository: widget.repository,
                                store: widget.store,
                              ),
                            ),
                          );
                        },
                      );
                    }
                    return RefreshIndicator(
                    onRefresh: () => _load(force: true),
                    child: CustomScrollView(
                      controller: _scroll,
                      physics: const AlwaysScrollableScrollPhysics(),
                      slivers: [
                        SliverPadding(
                          padding: const EdgeInsets.all(16),
                          sliver: SliverGrid(
                            gridDelegate: dramaGridDelegate(
                              context,
                              constraints.maxWidth - 32,
                            ),
                            delegate: SliverChildBuilderDelegate((
                              context,
                              index,
                            ) {
                              final drama = _items[index];
                              return DramaTile(
                                key: ValueKey(drama.id),
                                drama: drama,
                                repository: widget.repository,
                                onTap: () => unawaited(
                                  openPlaybackDirectly(
                                    context,
                                    drama: drama,
                                    repository: widget.repository,
                                    store: widget.store,
                                  ),
                                ),
                              );
                            }, childCount: _items.length),
                          ),
                        ),
                        SliverToBoxAdapter(
                          child: Padding(
                            padding: const EdgeInsets.fromLTRB(16, 0, 16, 24),
                            child: Center(
                              child: _more
                                  ? const CircularProgressIndicator()
                                  : _hasMore
                                  ? OutlinedButton.icon(
                                      onPressed: () => _load(more: true),
                                      icon: const Icon(
                                        Icons.auto_awesome_rounded,
                                      ),
                                      label: const Text('继续推荐'),
                                    )
                                  : TextButton(
                                      onPressed: () => _load(force: true),
                                      child: const Text('本轮推荐已看完，刷新获取新推荐'),
                                    ),
                            ),
                          ),
                        ),
                      ],
                    ),
                  );
                  },
                ),
        ),
      ],
    );
    if (widget.embedded) return content;
    return Scaffold(
      appBar: AppBar(title: const Text('红果推荐'), actions: [refresh]),
      body: SafeArea(top: false, child: content),
    );
  }
}
