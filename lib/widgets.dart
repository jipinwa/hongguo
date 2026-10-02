import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'core_bridge.dart';
import 'app_layout.dart';
import 'models.dart';
import 'remote_widgets.dart';

Future<void> saveUserChange(
  BuildContext context,
  Future<void> Function() action,
) async {
  try {
    await action();
  } catch (_) {
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('未能保存，请检查存储空间和权限后重试。')));
    }
  }
}

class RefreshAction extends StatefulWidget {
  const RefreshAction({
    super.key,
    required this.loading,
    required this.tooltip,
    required this.onPressed,
  });
  final bool loading;
  final String tooltip;
  final VoidCallback? onPressed;

  @override
  State<RefreshAction> createState() => _RefreshActionState();
}

class _RefreshActionState extends State<RefreshAction>
    with SingleTickerProviderStateMixin {
  late final AnimationController _rotation;

  @override
  void initState() {
    super.initState();
    _rotation = AnimationController(
      vsync: this,
      duration: const Duration(seconds: 1),
    );
    if (widget.loading) _rotation.repeat();
  }

  @override
  void didUpdateWidget(covariant RefreshAction oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.loading == oldWidget.loading) return;
    if (widget.loading) {
      _rotation.repeat();
    } else {
      _rotation.reset();
    }
  }

  @override
  void dispose() {
    _rotation.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Semantics(
    liveRegion: true,
    value: widget.loading ? '正在更新' : null,
    child: IconButton(
      tooltip: widget.loading ? '正在更新' : widget.tooltip,
      onPressed: widget.loading ? null : widget.onPressed,
      disabledColor: widget.loading
          ? Theme.of(context).colorScheme.primary
          : null,
      icon: RotationTransition(
        turns: _rotation,
        child: const Icon(Icons.refresh_rounded),
      ),
    ),
  );
}

class DramaCover extends StatelessWidget {
  const DramaCover({
    super.key,
    required this.drama,
    required this.repository,
    this.radius = 14,
  });
  final Drama drama;
  final AppRepository repository;
  final double radius;
  static const imagesDisabled = bool.fromEnvironment('DISABLE_REMOTE_IMAGES');

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final placeholder = Container(
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [colors.surfaceContainerHighest, colors.surfaceContainer],
        ),
      ),
      child: Center(
        child: Icon(
          Icons.movie_creation_outlined,
          size: 40,
          color: colors.onSurfaceVariant,
        ),
      ),
    );
    return ClipRRect(
      borderRadius: BorderRadius.circular(radius),
      child: Stack(
        fit: StackFit.expand,
        children: [
          placeholder,
          if (!imagesDisabled && drama.id.isNotEmpty)
            CachedCoverImage(
              key: ValueKey('${drama.id}\u0000${drama.cover}'),
              drama: drama,
              repository: repository,
              placeholder: placeholder,
            ),
          Positioned.fill(
            child: IgnorePointer(
              child: DecoratedBox(
                decoration: BoxDecoration(
                  gradient: LinearGradient(
                    begin: Alignment.center,
                    end: Alignment.bottomCenter,
                    colors: [
                      Colors.transparent,
                      Colors.black.withValues(alpha: .78),
                    ],
                  ),
                ),
              ),
            ),
          ),
          if (drama.episodes > 0)
            Positioned(
              left: 9,
              bottom: 9,
              child: Text(
                '共 ${drama.episodes} 集',
                style: const TextStyle(color: Colors.white, fontSize: 12),
              ),
            ),
          if (drama.vip)
            Positioned(
              left: 8,
              top: 8,
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
                decoration: BoxDecoration(
                  color: const Color(0xFFF6C86B),
                  borderRadius: BorderRadius.circular(6),
                ),
                child: const Text(
                  'VIP',
                  style: TextStyle(
                    color: Color(0xFF40300D),
                    fontSize: 11,
                    fontWeight: FontWeight.w800,
                  ),
                ),
              ),
            ),
        ],
      ),
    );
  }
}

class CachedCoverImage extends StatefulWidget {
  const CachedCoverImage({
    super.key,
    required this.drama,
    required this.repository,
    required this.placeholder,
  });
  final Drama drama;
  final AppRepository repository;
  final Widget placeholder;
  @override
  State<CachedCoverImage> createState() => _CachedCoverImageState();
}

class _CachedCoverImageState extends State<CachedCoverImage> {
  late Future<String> _file;
  int _coverRevision = 0;
  bool _coverFailed = false;
  bool _retryOnFailure = false;
  bool _retryQueued = false;
  String? _failedPath;

  @override
  void initState() {
    super.initState();
    _coverRevision = widget.repository.catalogUpdates.coverRevision(
      widget.drama.id,
    );
    widget.repository.catalogUpdates.addListener(_metadataChanged);
    _file = widget.repository.cover(widget.drama);
  }

  @override
  void didUpdateWidget(covariant CachedCoverImage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.repository != widget.repository) {
      oldWidget.repository.catalogUpdates.removeListener(_metadataChanged);
      widget.repository.catalogUpdates.addListener(_metadataChanged);
    }
    if (oldWidget.repository != widget.repository ||
        oldWidget.drama.id != widget.drama.id ||
        oldWidget.drama.cover != widget.drama.cover ||
        oldWidget.drama.source != widget.drama.source) {
      _coverFailed = _retryOnFailure = false;
      _failedPath = null;
      _coverRevision = widget.repository.catalogUpdates.coverRevision(
        widget.drama.id,
      );
      _file = widget.repository.cover(widget.drama);
    }
  }

  void _metadataChanged() {
    final revision = widget.repository.catalogUpdates.coverRevision(
      widget.drama.id,
    );
    if (revision == _coverRevision) return;
    _coverRevision = revision;
    _retryOnFailure = true;
    if (_coverFailed) _queueRetry();
  }

  void _queueRetry() {
    if (_retryQueued || !_retryOnFailure) return;
    _retryQueued = true;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      _retryQueued = false;
      if (!mounted || !_retryOnFailure) return;
      _retryOnFailure = false;
      _retry(_failedPath);
    });
    WidgetsBinding.instance.ensureVisualUpdate();
  }

  @override
  void dispose() {
    widget.repository.catalogUpdates.removeListener(_metadataChanged);
    super.dispose();
  }

  Future<void> _retry(String? path) async {
    if (path != null) {
      await ResizeImage.resizeIfNeeded(
        440,
        null,
        FileImage(File(path)),
      ).evict();
    }
    if (mounted) {
      setState(() {
        _coverFailed = _retryOnFailure = false;
        _failedPath = null;
        _file = widget.repository.cover(
          widget.repository.catalogUpdates.current(widget.drama),
          force: true,
        );
      });
    }
  }

  Widget _failed(String? path) {
    _coverFailed = true;
    _failedPath = path;
    if (_retryOnFailure) _queueRetry();
    return Center(
      child: IconButton(
        tooltip: '重试海报',
        onPressed: () => _retry(path),
        icon: Icon(
          Icons.refresh_rounded,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) => FutureBuilder<String>(
    future: _file,
    builder: (context, snapshot) {
      if (snapshot.connectionState != ConnectionState.done) {
        return widget.placeholder;
      }
      if (snapshot.hasError || !snapshot.hasData) {
        return _failed(null);
      }
      return Image.file(
        File(snapshot.data!),
        fit: BoxFit.cover,
        cacheWidth: 440,
        excludeFromSemantics: true,
        errorBuilder: (_, error, stack) => _failed(snapshot.data),
      );
    },
  );
}

class DramaTile extends StatelessWidget {
  const DramaTile({
    super.key,
    required this.drama,
    required this.repository,
    required this.onTap,
    this.subtitle,
    this.focusNode,
    this.onFocus,
    this.actions,
    this.badge,
    this.selected,
    this.onMore,
    this.onLongPress,
  });
  final Drama drama;
  final AppRepository repository;
  final VoidCallback onTap;
  final String? subtitle;
  final FocusNode? focusNode;
  final VoidCallback? onFocus;
  final Widget? actions;
  final String? badge;
  final bool? selected;
  final VoidCallback? onMore;
  final VoidCallback? onLongPress;

  static double titleHeight(BuildContext context) =>
      MediaQuery.textScalerOf(
        context,
      ).scale(AppLayout.isTelevision(context) ? 17 : 14) *
      2.6;

  static double subtitleHeight(BuildContext context) =>
      MediaQuery.textScalerOf(
        context,
      ).scale(AppLayout.isTelevision(context) ? 14 : 12) *
      1.3;

  static double extentFor(BuildContext context, double width) =>
      (width * 1.5 + 13 + titleHeight(context) + subtitleHeight(context))
          .ceilToDouble();

  @override
  Widget build(BuildContext context) {
    final television = AppLayout.isTelevision(context);
    final content = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        AspectRatio(
          aspectRatio: 2 / 3,
          child: Stack(
            fit: StackFit.expand,
            children: [
              DramaCover(drama: drama, repository: repository),
              if (badge != null && badge!.isNotEmpty)
                Positioned(
                  left: 6,
                  right: 6,
                  bottom: 6,
                  child: DecoratedBox(
                    decoration: BoxDecoration(
                      color: Colors.black.withValues(alpha: .72),
                      borderRadius: BorderRadius.circular(6),
                    ),
                    child: Padding(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 6,
                        vertical: 4,
                      ),
                      child: Text(
                        badge!,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 12,
                        ),
                      ),
                    ),
                  ),
                ),
              if (actions != null && selected == null)
                Positioned(top: 2, right: 2, child: actions!),
              if (selected != null)
                Positioned(
                  top: 6,
                  right: 6,
                  child: CircleAvatar(
                    radius: 16,
                    backgroundColor: selected!
                        ? Theme.of(context).colorScheme.primary
                        : Colors.black.withValues(alpha: .64),
                    child: Icon(
                      selected! ? Icons.check_rounded : Icons.circle_outlined,
                      size: 22,
                      color: selected!
                          ? Theme.of(context).colorScheme.onPrimary
                          : Colors.white,
                    ),
                  ),
                ),
            ],
          ),
        ),
        const SizedBox(height: 9),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 2),
          child: SizedBox(
            height: titleHeight(context),
            child: Text(
              drama.title,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontWeight: FontWeight.w600,
                height: 1.3,
                fontSize: television ? 17 : 14,
              ),
            ),
          ),
        ),
        const SizedBox(height: 4),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 2),
          child: SizedBox(
            height: subtitleHeight(context),
            child: Text(
              subtitle ?? drama.category,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                fontSize: television ? 14 : 12,
                height: 1.3,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ),
        ),
      ],
    );
    if (television) {
      return CallbackShortcuts(
        bindings: {
          const SingleActivator(LogicalKeyboardKey.contextMenu): ?onMore,
        },
        child: RemoteTarget(
          focusNode: focusNode,
          onFocus: onFocus,
          onPressed: onTap,
          selected: selected ?? false,
          label:
              '${drama.title}，${drama.episodes}集${badge == null ? '' : '，$badge'}',
          child: content,
        ),
      );
    }
    return Semantics(
      button: true,
      selected: selected,
      label: '${drama.title}，${drama.episodes}集',
      child: InkWell(
        onTap: onTap,
        onLongPress: onLongPress ?? onMore,
        onSecondaryTap: onMore,
        borderRadius: BorderRadius.circular(14),
        child: content,
      ),
    );
  }
}

class StatusPanel extends StatelessWidget {
  const StatusPanel({
    super.key,
    required this.title,
    this.message = '',
    this.onRetry,
    this.icon = Icons.movie_filter_outlined,
    this.action = '重试',
    this.secondaryAction,
  });
  final String title;
  final String message;
  final VoidCallback? onRetry;
  final IconData icon;
  final String action;
  final Widget? secondaryAction;
  @override
  Widget build(BuildContext context) => Center(
    child: SingleChildScrollView(
      padding: const EdgeInsets.all(28),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 56, color: Theme.of(context).colorScheme.primary),
          const SizedBox(height: 20),
          Text(
            title,
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.titleLarge,
          ),
          if (message.isNotEmpty) ...[
            const SizedBox(height: 12),
            Text(
              message,
              textAlign: TextAlign.center,
              style: TextStyle(
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
            ),
          ],
          if (onRetry != null) ...[
            const SizedBox(height: 24),
            FilledButton.icon(
              autofocus: AppLayout.isTelevision(context),
              onPressed: onRetry,
              icon: const Icon(Icons.refresh_rounded),
              label: Text(action),
            ),
          ],
          if (secondaryAction != null) ...[
            const SizedBox(height: 8),
            secondaryAction!,
          ],
        ],
      ),
    ),
  );
}

SliverGridDelegate dramaGridDelegate(BuildContext context, double width) {
  final columns = width < 600 ? 3 : (width / 180).floor().clamp(4, 9);
  final spacing = width < 600 ? 10.0 : 18.0;
  final tileWidth = (width - (columns - 1) * spacing) / columns;
  return SliverGridDelegateWithFixedCrossAxisCount(
    crossAxisCount: columns,
    mainAxisSpacing: 22,
    crossAxisSpacing: spacing,
    mainAxisExtent: DramaTile.extentFor(context, tileWidth),
  );
}

String formatPosition(double seconds) {
  final value = seconds.isFinite ? seconds.toInt().clamp(0, 999999) : 0;
  return '${value ~/ 60}:${(value % 60).toString().padLeft(2, '0')}';
}
