import 'package:flutter/material.dart';

import 'search_input.dart';
import 'app_layout.dart';
import 'package:flutter/services.dart';

/// 遥控区域之间的方向越界出口：没有接管者时保持默认焦点遍历行为。
KeyEventResult _remoteExit(VoidCallback? action, {bool handled = false}) {
  if (action == null) {
    return handled ? KeyEventResult.handled : KeyEventResult.ignored;
  }
  action();
  return KeyEventResult.handled;
}

/// 电视模式下为页面补齐初始焦点，避免进入页面后遥控器没有响应。
void ensureTelevisionFocus(BuildContext context) {
  WidgetsBinding.instance.addPostFrameCallback((_) {
    if (!context.mounted) return;
    if (!AppLayout.isTelevision(context)) return;
    final scope = FocusScope.of(context);
    if (scope.hasFocus) return;
    scope.nextFocus();
  });
}

class RemoteTarget extends StatefulWidget {
  const RemoteTarget({
    super.key,
    required this.child,
    required this.onPressed,
    this.focusNode,
    this.onFocus,
    this.autofocus = false,
    this.selected = false,
    this.label,
    this.radius = 14,
    this.padding = const EdgeInsets.all(4),
    this.borderWidth = 3,
    this.outlined = false,
  });
  final Widget child;
  final VoidCallback? onPressed;
  final FocusNode? focusNode;
  final VoidCallback? onFocus;
  final bool autofocus;
  final bool selected;
  final String? label;
  final double radius;
  final EdgeInsetsGeometry padding;
  final double borderWidth;
  final bool outlined;

  @override
  State<RemoteTarget> createState() => _RemoteTargetState();
}

class _RemoteTargetState extends State<RemoteTarget> {
  bool _focused = false;

  @override
  Widget build(BuildContext context) => FocusableActionDetector(
    focusNode: widget.focusNode,
    autofocus: widget.autofocus,
    enabled: widget.onPressed != null,
    actions: {
      ActivateIntent: CallbackAction<ActivateIntent>(
        onInvoke: (_) {
          widget.onPressed?.call();
          return null;
        },
      ),
    },
    onFocusChange: (focused) {
      if (mounted) {
        setState(() => _focused = focused);
      }
      if (focused) {
        widget.onFocus?.call();
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted && _focused) {
            Scrollable.ensureVisible(
              context,
              alignmentPolicy: ScrollPositionAlignmentPolicy.keepVisibleAtEnd,
            );
          }
        });
      }
    },
    child: Semantics(
      button: true,
      enabled: widget.onPressed != null,
      focused: _focused,
      label: widget.label,
      child: MouseRegion(
        cursor: widget.onPressed == null
            ? SystemMouseCursors.basic
            : SystemMouseCursors.click,
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: widget.onPressed,
          child: AnimatedContainer(
            duration: const Duration(milliseconds: 120),
            padding: widget.padding,
            decoration: BoxDecoration(
              color: widget.selected
                  ? Theme.of(context).colorScheme.primaryContainer
                  : _focused && widget.outlined
                  ? Theme.of(context).colorScheme.surfaceContainerHighest
                  : _focused
                  ? Theme.of(context).colorScheme.primaryContainer
                  : Colors.transparent,
              borderRadius: BorderRadius.circular(widget.radius),
              border: Border.all(
                color: _focused
                    ? Theme.of(context).colorScheme.primary
                    : widget.selected
                    ? Theme.of(context).colorScheme.primary
                    : widget.outlined
                    ? Theme.of(context).colorScheme.outlineVariant
                    : Colors.transparent,
                width: widget.borderWidth,
              ),
            ),
            child: ExcludeFocus(child: widget.child),
          ),
        ),
      ),
    ),
  );
}

class RemoteButton extends StatelessWidget {
  const RemoteButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.icon,
    this.selected = false,
    this.autofocus = false,
    this.focusNode,
    this.onFocus,
  });
  final String label;
  final VoidCallback? onPressed;
  final IconData? icon;
  final bool selected;
  final bool autofocus;
  final FocusNode? focusNode;
  final VoidCallback? onFocus;

  @override
  Widget build(BuildContext context) => RemoteTarget(
    onPressed: onPressed,
    selected: selected,
    autofocus: autofocus,
    focusNode: focusNode,
    onFocus: onFocus,
    label: label,
    child: Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[Icon(icon, size: 22), const SizedBox(width: 8)],
          Text(
            label,
            style: TextStyle(
              fontSize: 17,
              color: onPressed == null
                  ? Theme.of(context).disabledColor
                  : Theme.of(context).colorScheme.onSurface,
            ),
          ),
        ],
      ),
    ),
  );
}

class RemoteGrid extends StatefulWidget {
  const RemoteGrid({
    super.key,
    required this.itemKeys,
    required this.columns,
    required this.itemExtent,
    required this.itemBuilder,
    this.controller,
    this.spacing = 14,
    this.padding = const EdgeInsets.all(18),
    this.footer,
    this.initialIndex = 0,
    this.autofocus = false,
    this.onExitUp,
    this.onExitDown,
    this.onExitLeft,
    this.onExitRight,
  });
  final List<String> itemKeys;
  final int columns;
  final double itemExtent;
  final double spacing;
  final EdgeInsets padding;
  final ScrollController? controller;
  final Widget? footer;
  final int initialIndex;
  final bool autofocus;
  final VoidCallback? onExitUp;
  final VoidCallback? onExitDown;
  final VoidCallback? onExitLeft;
  final VoidCallback? onExitRight;
  final Widget Function(BuildContext, int, FocusNode, VoidCallback) itemBuilder;

  @override
  State<RemoteGrid> createState() => RemoteGridState();
}

class RemoteGridState extends State<RemoteGrid> {
  final _ownScroll = ScrollController();
  final _nodes = <String, FocusNode>{};
  String? _focused;
  String? _target;
  int _generation = 0;
  ScrollController get _scroll => widget.controller ?? _ownScroll;

  FocusNode _node(int index) => _nodes.putIfAbsent(
    widget.itemKeys[index],
    () => FocusNode(debugLabel: 'remote-${widget.itemKeys[index]}'),
  );

  int get _currentIndex {
    if (widget.itemKeys.isEmpty) return -1;
    final key = _target ?? _focused;
    final index = key == null ? -1 : widget.itemKeys.indexOf(key);
    return index < 0
        ? widget.initialIndex.clamp(0, widget.itemKeys.length - 1)
        : index;
  }

  void focusCurrent() {
    final index = _currentIndex;
    if (index >= 0) _focusAt(index);
  }

  @override
  void initState() {
    super.initState();
    if (widget.autofocus) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.itemKeys.isNotEmpty) {
          _focusAt(widget.initialIndex.clamp(0, widget.itemKeys.length - 1));
        }
      });
    }
  }

  void _focusAt(int index) {
    final generation = ++_generation;
    _target = widget.itemKeys[index];
    final node = _node(index);
    if (node.context != null) {
      node.requestFocus();
      _target = null;
      return;
    }
    if (_scroll.hasClients) {
      final top =
          widget.padding.top +
          (index ~/ widget.columns) * (widget.itemExtent + widget.spacing);
      final offset = top < _scroll.offset
          ? top
          : top + widget.itemExtent - _scroll.position.viewportDimension;
      _scroll.jumpTo(offset.clamp(0.0, _scroll.position.maxScrollExtent));
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && generation == _generation && node.context != null) {
        node.requestFocus();
        _target = null;
      }
    });
  }

  KeyEventResult _key(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (_focused == null || !(_nodes[_focused]?.hasFocus ?? false)) {
      return KeyEventResult.ignored;
    }
    final index = widget.itemKeys.indexOf(_target ?? _focused!);
    if (index < 0) return KeyEventResult.ignored;
    final key = event.logicalKey;
    final int next;
    if (key == LogicalKeyboardKey.arrowDown) {
      if (index ~/ widget.columns ==
          (widget.itemKeys.length - 1) ~/ widget.columns) {
        return _remoteExit(widget.onExitDown);
      }
      next = (index + widget.columns).clamp(0, widget.itemKeys.length - 1);
    } else if (key == LogicalKeyboardKey.arrowUp) {
      if (index < widget.columns) return _remoteExit(widget.onExitUp);
      next = index - widget.columns;
    } else if (key == LogicalKeyboardKey.arrowRight) {
      if (index % widget.columns == widget.columns - 1 ||
          index == widget.itemKeys.length - 1) {
        return _remoteExit(widget.onExitRight, handled: true);
      }
      next = index + 1;
    } else if (key == LogicalKeyboardKey.arrowLeft) {
      if (index % widget.columns == 0) return _remoteExit(widget.onExitLeft);
      next = index - 1;
    } else {
      return KeyEventResult.ignored;
    }
    if (next < 0 || next >= widget.itemKeys.length) {
      return KeyEventResult.ignored;
    }
    _focusAt(next);
    return KeyEventResult.handled;
  }

  @override
  void didUpdateWidget(covariant RemoteGrid oldWidget) {
    super.didUpdateWidget(oldWidget);
    final removed = _nodes.keys
        .where((key) => !widget.itemKeys.contains(key))
        .toList();
    final restore = removed.any((key) => _nodes[key]!.hasFocus);
    final index = oldWidget.itemKeys.indexOf(_focused ?? '');
    for (final key in removed) {
      _nodes.remove(key)?.dispose();
    }
    if (_focused != null && !widget.itemKeys.contains(_focused)) {
      _focused = null;
      _target = null;
      _generation++;
    }
    if (restore && widget.itemKeys.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.itemKeys.isNotEmpty) {
          _focusAt(index.clamp(0, widget.itemKeys.length - 1));
        }
      });
    }
  }

  @override
  void dispose() {
    _generation++;
    for (final node in _nodes.values) {
      node.dispose();
    }
    _ownScroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Focus(
    canRequestFocus: false,
    skipTraversal: true,
    onKeyEvent: _key,
    child: CustomScrollView(
      controller: _scroll,
      slivers: [
        SliverPadding(
          padding: widget.padding,
          sliver: SliverGrid(
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: widget.columns,
              mainAxisExtent: widget.itemExtent,
              crossAxisSpacing: widget.spacing,
              mainAxisSpacing: widget.spacing,
            ),
            delegate: SliverChildBuilderDelegate(
              (context, index) =>
                  widget.itemBuilder(context, index, _node(index), () {
                    _focused = widget.itemKeys[index];
                  }),
              childCount: widget.itemKeys.length,
            ),
          ),
        ),
        if (widget.footer != null) SliverToBoxAdapter(child: widget.footer),
      ],
    ),
  );
}

/// 单行横向遥控区域，用于分类栏、榜单栏目和工具行。
class RemoteRow extends StatefulWidget {
  const RemoteRow({
    super.key,
    required this.itemKeys,
    required this.itemBuilder,
    this.controller,
    this.spacing = 6,
    this.padding = EdgeInsets.zero,
    this.initialIndex = 0,
    this.autofocus = false,
    this.onExitUp,
    this.onExitDown,
    this.onExitLeft,
    this.onExitRight,
  });
  final List<String> itemKeys;
  final double spacing;
  final EdgeInsets padding;
  final ScrollController? controller;
  final int initialIndex;
  final bool autofocus;
  final VoidCallback? onExitUp;
  final VoidCallback? onExitDown;
  final VoidCallback? onExitLeft;
  final VoidCallback? onExitRight;
  final Widget Function(BuildContext, int, FocusNode, VoidCallback) itemBuilder;

  @override
  State<RemoteRow> createState() => RemoteRowState();
}

class RemoteRowState extends State<RemoteRow> {
  final _ownScroll = ScrollController();
  final _nodes = <String, FocusNode>{};
  String? _focused;
  String? _target;
  ScrollController get _scroll => widget.controller ?? _ownScroll;

  FocusNode _node(int index) => _nodes.putIfAbsent(
    widget.itemKeys[index],
    () => FocusNode(debugLabel: 'remote-row-${widget.itemKeys[index]}'),
  );

  @override
  void initState() {
    super.initState();
    if (widget.autofocus) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.itemKeys.isNotEmpty) {
          _focusAt(widget.initialIndex.clamp(0, widget.itemKeys.length - 1));
        }
      });
    }
  }

  int get _currentIndex {
    if (widget.itemKeys.isEmpty) return -1;
    final key = _target ?? _focused;
    final index = key == null ? -1 : widget.itemKeys.indexOf(key);
    return index < 0
        ? widget.initialIndex.clamp(0, widget.itemKeys.length - 1)
        : index;
  }

  void focusCurrent() {
    final index = _currentIndex;
    if (index >= 0) _focusAt(index);
  }

  void _focusAt(int index) {
    _target = widget.itemKeys[index];
    final node = _node(index);
    if (node.context != null) {
      node.requestFocus();
      _target = null;
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && node.context != null) {
        node.requestFocus();
        _target = null;
      }
    });
  }

  KeyEventResult _key(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (_focused == null || !(_nodes[_focused]?.hasFocus ?? false)) {
      return KeyEventResult.ignored;
    }
    final index = widget.itemKeys.indexOf(_target ?? _focused!);
    if (index < 0) return KeyEventResult.ignored;
    final key = event.logicalKey;
    final int next;
    if (key == LogicalKeyboardKey.arrowRight) {
      if (index == widget.itemKeys.length - 1) {
        return _remoteExit(widget.onExitRight, handled: true);
      }
      next = index + 1;
    } else if (key == LogicalKeyboardKey.arrowLeft) {
      if (index == 0) return _remoteExit(widget.onExitLeft);
      next = index - 1;
    } else if (key == LogicalKeyboardKey.arrowUp) {
      return _remoteExit(widget.onExitUp);
    } else if (key == LogicalKeyboardKey.arrowDown) {
      return _remoteExit(widget.onExitDown);
    } else {
      return KeyEventResult.ignored;
    }
    _focusAt(next);
    return KeyEventResult.handled;
  }

  @override
  void didUpdateWidget(covariant RemoteRow oldWidget) {
    super.didUpdateWidget(oldWidget);
    final removed = _nodes.keys
        .where((key) => !widget.itemKeys.contains(key))
        .toList();
    for (final key in removed) {
      _nodes.remove(key)?.dispose();
    }
    if (_focused != null && !widget.itemKeys.contains(_focused)) {
      _focused = null;
      _target = null;
    }
    if (_target != null && !widget.itemKeys.contains(_target)) _target = null;
  }

  @override
  void dispose() {
    for (final node in _nodes.values) {
      node.dispose();
    }
    _ownScroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Focus(
    canRequestFocus: false,
    skipTraversal: true,
    onKeyEvent: _key,
    child: SingleChildScrollView(
      controller: _scroll,
      scrollDirection: Axis.horizontal,
      padding: widget.padding,
      child: Row(
        children: [
          for (var index = 0; index < widget.itemKeys.length; index++)
            Padding(
              padding: EdgeInsets.only(
                right: index == widget.itemKeys.length - 1
                    ? 0
                    : widget.spacing,
              ),
              child: widget.itemBuilder(
                context,
                index,
                _node(index),
                () => _focused = widget.itemKeys[index],
              ),
            ),
        ],
      ),
    ),
  );
}

/// 等高纵向遥控列表，用于榜单、下载、设置等条目式页面。
class RemoteList extends StatefulWidget {
  const RemoteList({
    super.key,
    required this.itemKeys,
    required this.itemExtent,
    required this.itemBuilder,
    this.controller,
    this.spacing = 8,
    this.padding = const EdgeInsets.fromLTRB(12, 8, 12, 24),
    this.initialIndex = 0,
    this.autofocus = false,
    this.footer,
    this.onExitUp,
    this.onExitDown,
    this.onExitLeft,
    this.onExitRight,
  });
  final List<String> itemKeys;
  final double itemExtent;
  final double spacing;
  final EdgeInsets padding;
  final ScrollController? controller;
  final Widget? footer;
  final int initialIndex;
  final bool autofocus;
  final VoidCallback? onExitUp;
  final VoidCallback? onExitDown;
  final VoidCallback? onExitLeft;
  final VoidCallback? onExitRight;
  final Widget Function(BuildContext, int, FocusNode, VoidCallback) itemBuilder;

  @override
  State<RemoteList> createState() => RemoteListState();
}

class RemoteListState extends State<RemoteList> {
  final _ownScroll = ScrollController();
  final _nodes = <String, FocusNode>{};
  String? _focused;
  String? _target;
  int _generation = 0;
  ScrollController get _scroll => widget.controller ?? _ownScroll;
  double get _step => widget.itemExtent + widget.spacing;

  FocusNode _node(int index) => _nodes.putIfAbsent(
    widget.itemKeys[index],
    () => FocusNode(debugLabel: 'remote-list-${widget.itemKeys[index]}'),
  );

  @override
  void initState() {
    super.initState();
    if (widget.autofocus) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.itemKeys.isNotEmpty) {
          _focusAt(widget.initialIndex.clamp(0, widget.itemKeys.length - 1));
        }
      });
    }
  }

  int get _currentIndex {
    if (widget.itemKeys.isEmpty) return -1;
    final key = _target ?? _focused;
    final index = key == null ? -1 : widget.itemKeys.indexOf(key);
    return index < 0
        ? widget.initialIndex.clamp(0, widget.itemKeys.length - 1)
        : index;
  }

  void focusCurrent() {
    final index = _currentIndex;
    if (index >= 0) _focusAt(index);
  }

  void _focusAt(int index) {
    final generation = ++_generation;
    _target = widget.itemKeys[index];
    final node = _node(index);
    if (node.context != null) {
      node.requestFocus();
      _target = null;
      return;
    }
    if (_scroll.hasClients) {
      final top = widget.padding.top + index * _step;
      final offset = top < _scroll.offset
          ? top
          : top + widget.itemExtent - _scroll.position.viewportDimension;
      _scroll.jumpTo(offset.clamp(0.0, _scroll.position.maxScrollExtent));
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted && generation == _generation && node.context != null) {
        node.requestFocus();
        _target = null;
      }
    });
  }

  KeyEventResult _key(FocusNode node, KeyEvent event) {
    if (event is! KeyDownEvent && event is! KeyRepeatEvent) {
      return KeyEventResult.ignored;
    }
    if (_focused == null || !(_nodes[_focused]?.hasFocus ?? false)) {
      return KeyEventResult.ignored;
    }
    final index = widget.itemKeys.indexOf(_target ?? _focused!);
    if (index < 0) return KeyEventResult.ignored;
    final key = event.logicalKey;
    final int next;
    if (key == LogicalKeyboardKey.arrowDown) {
      if (index == widget.itemKeys.length - 1) {
        return _remoteExit(widget.onExitDown);
      }
      next = index + 1;
    } else if (key == LogicalKeyboardKey.arrowUp) {
      if (index == 0) return _remoteExit(widget.onExitUp);
      next = index - 1;
    } else if (key == LogicalKeyboardKey.arrowLeft) {
      return _remoteExit(widget.onExitLeft);
    } else if (key == LogicalKeyboardKey.arrowRight) {
      return _remoteExit(widget.onExitRight);
    } else {
      return KeyEventResult.ignored;
    }
    _focusAt(next);
    return KeyEventResult.handled;
  }

  @override
  void didUpdateWidget(covariant RemoteList oldWidget) {
    super.didUpdateWidget(oldWidget);
    final removed = _nodes.keys
        .where((key) => !widget.itemKeys.contains(key))
        .toList();
    final restore = removed.any((key) => _nodes[key]!.hasFocus);
    final index = oldWidget.itemKeys.indexOf(_focused ?? '');
    for (final key in removed) {
      _nodes.remove(key)?.dispose();
    }
    if (_focused != null && !widget.itemKeys.contains(_focused)) {
      _focused = null;
      _target = null;
      _generation++;
    }
    if (_target != null && !widget.itemKeys.contains(_target)) _target = null;
    if (restore && widget.itemKeys.isNotEmpty) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && widget.itemKeys.isNotEmpty) {
          _focusAt(index.clamp(0, widget.itemKeys.length - 1));
        }
      });
    }
  }

  @override
  void dispose() {
    _generation++;
    for (final node in _nodes.values) {
      node.dispose();
    }
    _ownScroll.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Focus(
    canRequestFocus: false,
    skipTraversal: true,
    onKeyEvent: _key,
    child: CustomScrollView(
      controller: _scroll,
      slivers: [
        SliverPadding(
          padding: widget.padding,
          sliver: SliverFixedExtentList(
            itemExtent: _step,
            delegate: SliverChildBuilderDelegate(
              (context, index) => Padding(
                padding: EdgeInsets.only(bottom: widget.spacing),
                child: widget.itemBuilder(
                  context,
                  index,
                  _node(index),
                  () => _focused = widget.itemKeys[index],
                ),
              ),
              childCount: widget.itemKeys.length,
            ),
          ),
        ),
        if (widget.footer != null) SliverToBoxAdapter(child: widget.footer),
      ],
    ),
  );
}

/// 电视模式下的统一条目外观：整行是一个焦点单位，状态写在副标题文本里。
class RemoteListTile extends StatelessWidget {
  const RemoteListTile({
    super.key,
    required this.title,
    this.subtitle,
    this.leading,
    this.trailing,
    this.onPressed,
    this.selected = false,
    this.focusNode,
    this.onFocus,
    this.label,
  });
  final String title;
  final String? subtitle;
  final Widget? leading;
  final Widget? trailing;
  final VoidCallback? onPressed;
  final bool selected;
  final FocusNode? focusNode;
  final VoidCallback? onFocus;
  final String? label;

  static const double extent = 80;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return RemoteTarget(
      focusNode: focusNode,
      onFocus: onFocus,
      onPressed: onPressed,
      selected: selected,
      radius: 12,
      borderWidth: 2,
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      label: label ?? title,
      child: SizedBox(
        height: 52,
        child: Row(
          children: [
            if (leading != null) ...[leading!, const SizedBox(width: 14)],
            Expanded(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(
                    title,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: const TextStyle(
                      fontSize: 18,
                      height: 1.3,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  if (subtitle != null && subtitle!.isNotEmpty)
                    Text(
                      subtitle!,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 14,
                        height: 1.3,
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                ],
              ),
            ),
            if (trailing != null) ...[
              const SizedBox(width: 12),
              ExcludeFocus(child: trailing!),
            ],
          ],
        ),
      ),
    );
  }
}

class RemoteEpisodeButton extends StatelessWidget {
  const RemoteEpisodeButton({
    super.key,
    required this.number,
    required this.onPressed,
    this.vip = false,
    this.current = false,
    this.compact = false,
    this.focusNode,
    this.onFocus,
  });
  final int number;
  final bool vip;
  final bool current;
  final bool compact;
  final VoidCallback onPressed;
  final FocusNode? focusNode;
  final VoidCallback? onFocus;

  @override
  Widget build(BuildContext context) => RemoteTarget(
    focusNode: focusNode,
    onFocus: onFocus,
    selected: current,
    radius: compact ? 8 : 14,
    padding: compact ? const EdgeInsets.all(2) : const EdgeInsets.all(4),
    borderWidth: compact ? 1.2 : 2,
    outlined: true,
    onPressed: onPressed,
    label: '第 $number 集${vip ? '，VIP 试看' : ''}',
    child: Center(
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Flexible(
            child: Text(
              '$number',
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: compact ? 14 : 20,
                fontWeight: current ? FontWeight.w700 : FontWeight.w500,
              ),
            ),
          ),
          if (vip) ...[
            SizedBox(width: compact ? 2 : 4),
            Icon(
              Icons.workspace_premium_rounded,
              color: Theme.of(context).colorScheme.tertiary,
              size: compact ? 15 : 18,
            ),
          ],
        ],
      ),
    ),
  );
}

/// 电视模式下的选项列表对话框，全部选项都可用遥控器移动和确认。
class TelevisionActionDialog extends StatelessWidget {
  const TelevisionActionDialog({
    super.key,
    required this.title,
    required this.options,
  });
  final String title;
  final List<TelevisionAction> options;

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(title),
    content: SizedBox(
      width: 520,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * .6,
        ),
        child: RemoteList(
          itemKeys: [for (var index = 0; index <= options.length; index++) '$index'],
          itemExtent: RemoteListTile.extent,
          padding: EdgeInsets.zero,
          autofocus: true,
          itemBuilder: (_, index, node, onFocus) {
            if (index == options.length) {
              return RemoteListTile(
                title: '返回',
                leading: const Icon(Icons.arrow_back_rounded, size: 26),
                focusNode: node,
                onFocus: onFocus,
                onPressed: () => Navigator.pop(context),
              );
            }
            final option = options[index];
            return RemoteListTile(
              title: option.label,
              subtitle: option.description,
              leading: Icon(option.icon, size: 26),
              focusNode: node,
              onFocus: onFocus,
              onPressed: option.enabled
                  ? () => Navigator.pop(context, option.value)
                  : null,
            );
          },
        ),
      ),
    ),
  );
}

class TelevisionAction {
  const TelevisionAction({
    required this.value,
    required this.label,
    this.description,
    this.icon = Icons.chevron_right_rounded,
    this.enabled = true,
  });
  final String value;
  final String label;
  final String? description;
  final IconData icon;
  final bool enabled;
}

class TelevisionSearchDialog extends StatefulWidget {
  const TelevisionSearchDialog({
    super.key,
    required this.initialValue,
    required this.title,
    this.suggestions,
    this.recentSearches = const [],
    this.onCancel,
  });
  final Future<List<String>> Function(String)? suggestions;
  final List<String> recentSearches;
  final VoidCallback? onCancel;
  final String initialValue;
  final String title;

  @override
  State<TelevisionSearchDialog> createState() => _TelevisionSearchDialogState();
}

class _TelevisionSearchDialogState extends State<TelevisionSearchDialog> {
  late final _controller = TextEditingController(text: widget.initialValue);
  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.title),
    content: SizedBox(
      width: 460,
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(context).height * .6,
        ),
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              SearchInput(
                autofocus: true,
                controller: _controller,
                hint: '输入剧名',
                suggestions: widget.suggestions,
                onCancel: widget.onCancel,
                onSearch: (value) => Navigator.pop(context, value),
              ),
              if (widget.recentSearches.isNotEmpty) ...[
                const SizedBox(height: 16),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    for (final query in widget.recentSearches)
                      ActionChip(
                        label: Text(query),
                        avatar: const Icon(Icons.history_rounded, size: 16),
                        onPressed: () => Navigator.pop(context, query),
                      ),
                  ],
                ),
              ],
            ],
          ),
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('取消'),
      ),
      TextButton(
        onPressed: () => Navigator.pop(context, ''),
        child: const Text('清空'),
      ),
      FilledButton(
        onPressed: () => Navigator.pop(context, _controller.text.trim()),
        child: const Text('搜索'),
      ),
    ],
  );
}
