import 'package:flutter/material.dart';

import 'app_layout.dart';
import 'catalog_filters.dart';
import 'core_bridge.dart';
import 'detail_screen.dart';
import 'local_store.dart';
import 'models.dart';
import 'ranking_models.dart';
import 'remote_widgets.dart';
import 'widgets.dart';

class RankingsScreen extends StatefulWidget {
  const RankingsScreen({
    super.key,
    required this.repository,
    required this.store,
    required this.initialGroup,
  });
  final AppRepository repository;
  final LocalStore store;
  final String initialGroup;

  @override
  State<RankingsScreen> createState() => _RankingsScreenState();
}

class _RankingsScreenState extends State<RankingsScreen> {
  final _scroll = ScrollController();
  final _boardsKey = GlobalKey<RemoteRowState>();
  final _listKey = GlobalKey<RemoteListState>();
  List<RankingBoard> _boards = [];
  RankingBoard? _board;
  List<RankingItem> _items = [];
  bool _loading = true;
  bool _more = false;
  bool _hasMore = false;
  bool _failedMore = false;
  int _page = 1;
  int _generation = 0;
  String? _error;
  String _updated = '';

  @override
  void initState() {
    super.initState();
    _scroll.addListener(_onScroll);
    widget.repository.catalogUpdates.addListener(_metadataChanged);
    _initialize();
  }

  @override
  void dispose() {
    widget.repository.catalogUpdates.removeListener(_metadataChanged);
    _generation++;
    _scroll.removeListener(_onScroll);
    _scroll.dispose();
    super.dispose();
  }

  void _onScroll() {
    if (!mounted || !_hasMore || _loading || _more || !_scroll.hasClients) {
      return;
    }
    final position = _scroll.position;
    final threshold = (position.viewportDimension * 1.2).clamp(260.0, 720.0);
    if (position.extentAfter <= threshold) {
      _load(more: true);
    }
  }

  void _metadataChanged() {
    if (mounted) setState(() {});
  }

  Future<void> _initialize() async {
    final generation = ++_generation;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final boards = (await widget.repository.rankingBoards())
          .where((board) => widget.store.allowsSource(board.source))
          .toList();
      if (!mounted || generation != _generation) return;
      setState(() {
        _boards = boards;
        _board =
            boards
                .where((board) => board.groupId == widget.initialGroup)
                .firstOrNull ??
            boards.firstOrNull;
        _loading = false;
      });
      if (_board != null) await _load();
    } catch (error) {
      if (mounted && generation == _generation) {
        setState(() {
          _loading = false;
          _error = error.toString();
        });
      }
    }
  }

  Future<void> _load({bool more = false, bool force = false}) async {
    final board = _board;
    if (board == null || more && (_loading || _more || !_hasMore)) return;
    final generation = ++_generation;
    setState(() {
      _loading = !more;
      _more = more;
      _error = null;
      _failedMore = false;
    });
    try {
      final result = await widget.repository.rankings(
        board.id,
        page: more ? _page + 1 : 1,
        force: force,
      );
      if (!mounted || generation != _generation) return;
      setState(() {
        final rows = <String, RankingItem>{
          if (more)
            for (final item in _items) item.drama.id: item,
          for (final item in result.items) item.drama.id: item,
        };
        _items = rows.values.toList()..sort((a, b) => a.rank.compareTo(b.rank));
        _page = result.page;
        _hasMore = result.hasMore;
        _updated = result.updatedText;
        _error = result.warning.isEmpty ? null : result.warning;
        _loading = false;
        _more = false;
      });
    } catch (error) {
      if (mounted && generation == _generation) {
        setState(() {
          _error = error.toString();
          _failedMore = more;
          _loading = false;
          _more = false;
        });
      }
    }
  }

  void _select(RankingBoard board) {
    if (_board?.id == board.id) return;
    setState(() {
      _board = board;
      _items = [];
      _updated = '';
      _page = 1;
      _hasMore = false;
    });
    if (_scroll.hasClients) _scroll.jumpTo(0);
    _load();
  }

  @override
  Widget build(BuildContext context) {
    final television = AppLayout.isTelevision(context);
    final groups = SourceGroup.fromSources(
      _boards.map((board) => SourceSite.byId(board.source)).toSet(),
    );
    final boards = _boards
        .where((board) => board.groupId == _board?.groupId)
        .toList();
    final items = _items
        .map(
          (item) => RankingItem(
            item.rank,
            widget.repository.catalogUpdates.current(item.drama),
            metric: item.metric,
          ),
        )
        .where(
          (item) =>
              widget.store.allowsSource(item.drama.source) &&
              !(item.drama.source == 'huangdou' &&
                  widget.store.hideVip &&
                  item.drama.vip),
        )
        .toList();
    return Scaffold(
      appBar: AppBar(
        title: PopupMenuButton<SourceGroup>(
          tooltip: '切换榜单站源',
          enabled: groups.length > 1,
          onSelected: (group) =>
              _select(_boards.firstWhere((board) => board.groupId == group.id)),
          itemBuilder: (_) => [
            for (final group in groups)
              PopupMenuItem(value: group, child: Text(group.name)),
          ],
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(
                _board == null
                    ? '榜单'
                    : '${SourceSite.byId(_board!.source).groupName}榜单',
              ),
              if (groups.length > 1) const Icon(Icons.expand_more_rounded),
            ],
          ),
        ),
        actions: [
          RefreshAction(
            loading: _loading || _more,
            tooltip: '更新榜单',
            onPressed: _board == null ? _initialize : () => _load(force: true),
          ),
        ],
      ),
      body: SafeArea(
        top: false,
        child: Column(
          children: [
            if (boards.isNotEmpty)
              CatalogFilters(
                key: ValueKey('ranking-boards-${_board!.groupId}'),
                categories: [
                  for (final board in boards)
                    CatalogCategory(board.id, board.name),
                ],
                category: _board!.id,
                remoteKey: _boardsKey,
                remoteAutofocus: television,
                onExitDown: television
                    ? () => _listKey.currentState?.focusCurrent()
                    : null,
                onCategory: (id) =>
                    _select(boards.firstWhere((board) => board.id == id)),
                onRetry: () => _load(force: true),
              ),
            if (_board != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
                child: Align(
                  alignment: Alignment.centerLeft,
                  child: Text(
                    [
                      _board!.description,
                      if (_updated.isNotEmpty) _updated,
                    ].join(' '),
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ),
              ),
            if (_loading && items.isNotEmpty)
              const LinearProgressIndicator(minHeight: 2),
            if (_error != null && items.isNotEmpty)
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
                          : () => _load(more: _failedMore, force: true),
                      child: const Text('重试'),
                    ),
                  ],
                ),
              ),
            Expanded(
              child: _loading && items.isEmpty
                  ? Center(
                      child: Column(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          const CircularProgressIndicator(),
                          if (_board?.source == 'hongguo') ...[
                            const SizedBox(height: 16),
                            const Text('正在获取榜单，数据未完整返回时会自动重试'),
                          ],
                        ],
                      ),
                    )
                  : items.isEmpty
                  ? StatusPanel(
                      title: _error == null ? '暂无榜单内容' : '榜单暂时无法加载',
                      message: _error ?? '当前站源暂无可显示的榜单。',
                      onRetry: _board == null
                          ? _initialize
                          : () => _load(force: true),
                    )
                  : television
                  ? RemoteList(
                      key: _listKey,
                      itemKeys: [
                        for (final item in items)
                          '${item.rank}-${item.drama.id}',
                      ],
                      itemExtent: 140,
                      spacing: 10,
                      controller: _scroll,
                      onExitUp: () => _boardsKey.currentState?.focusCurrent(),
                      itemBuilder: (_, index, node, onFocus) => _rankTile(
                        items[index],
                        focusNode: node,
                        onFocus: onFocus,
                      ),
                    )
                  : RefreshIndicator(
                      onRefresh: () => _load(force: true),
                      child: ListView.builder(
                        key: ValueKey('ranking-${_board!.id}'),
                        controller: _scroll,
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(12, 0, 12, 24),
                        itemCount: items.length + 1,
                        itemBuilder: (context, index) {
                          if (index == items.length) {
                            return Center(
                              child: _more
                                  ? const Padding(
                                      padding: EdgeInsets.all(16),
                                      child: CircularProgressIndicator(),
                                    )
                                  : _hasMore
                                  ? OutlinedButton(
                                      onPressed: () => _load(more: true),
                                      child: const Text('加载更多'),
                                    )
                                  : const Padding(
                                      padding: EdgeInsets.all(16),
                                      child: Text('已显示全部榜单'),
                                    ),
                            );
                          }
                          return _rankTile(items[index]);
                        },
                      ),
                    ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _rankTile(
    RankingItem item, {
    FocusNode? focusNode,
    VoidCallback? onFocus,
  }) {
    final theme = Theme.of(context);
    void open() => Navigator.push<void>(
      context,
      MaterialPageRoute(
        builder: (_) => DetailScreen(
          drama: item.drama,
          repository: widget.repository,
          store: widget.store,
        ),
      ),
    );
    final body = Padding(
      padding: const EdgeInsets.all(12),
      child: Row(
        children: [
          SizedBox(
            width: 36,
            child: Text(
              '${item.rank}',
              style: TextStyle(
                fontSize: 24,
                fontWeight: FontWeight.w800,
                color: item.rank <= 3
                    ? theme.colorScheme.primary
                    : theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
          SizedBox(
            width: 72,
            height: 108,
            child: DramaCover(
              drama: item.drama,
              repository: widget.repository,
              radius: 8,
            ),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  item.drama.title,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.titleMedium,
                ),
                if (item.drama.category.isNotEmpty)
                  Text(
                    item.drama.category,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                if (item.metric.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: 8),
                    child: Text(
                      item.metric,
                      style: TextStyle(color: theme.colorScheme.primary),
                    ),
                  ),
              ],
            ),
          ),
          const Icon(Icons.chevron_right_rounded),
        ],
      ),
    );
    if (focusNode == null && onFocus == null) {
      return Builder(
        builder: (itemContext) => Card(
          child: InkWell(
            key: ValueKey('rank-${item.rank}-${item.drama.id}'),
            borderRadius: BorderRadius.circular(12),
            onFocusChange: (focused) {
              if (focused) {
                Scrollable.ensureVisible(itemContext, alignment: .4);
              }
            },
            onTap: open,
            child: body,
          ),
        ),
      );
    }
    return RemoteTarget(
      key: ValueKey('rank-${item.rank}-${item.drama.id}'),
      focusNode: focusNode,
      onFocus: onFocus,
      outlined: true,
      borderWidth: 2,
      radius: 12,
      padding: EdgeInsets.zero,
      onPressed: open,
      label:
          '第 ${item.rank} 名，${item.drama.title}${item.metric.isEmpty ? '' : '，${item.metric}'}',
      child: Card(margin: EdgeInsets.zero, child: body),
    );
  }
}
