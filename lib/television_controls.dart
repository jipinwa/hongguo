import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:media_kit/media_kit.dart';

import 'models.dart';
import 'playback_preferences.dart';
import 'video_enhancement.dart';
import 'video_enhancement_preferences.dart';
import 'video_enhancement_settings.dart';
import 'remote_widgets.dart';
import 'widgets.dart';

class TelevisionControls extends StatefulWidget {
  const TelevisionControls({
    super.key,
    required this.player,
    required this.title,
    required this.enabled,
    required this.showOnPlaybackReady,
    required this.onTogglePlayback,
    required this.onSeek,
    required this.onPrevious,
    required this.onNext,
    required this.onEpisodes,
    required this.onSettings,
    required this.onBack,
    this.enhancement,
  });
  final Player player;
  final String title;
  final bool enabled;
  final bool showOnPlaybackReady;
  final VoidCallback onTogglePlayback;
  final ValueChanged<int> onSeek;
  final VoidCallback? onPrevious;
  final VoidCallback? onNext;
  final Future<void> Function() onEpisodes;
  final Future<void> Function() onSettings;
  final VoidCallback onBack;
  final VideoEnhancementController? enhancement;

  @override
  State<TelevisionControls> createState() => _TelevisionControlsState();
}

class _TelevisionControlsState extends State<TelevisionControls> {
  final _surface = FocusNode(debugLabel: 'tv-player-surface');
  final _play = FocusNode(debugLabel: 'tv-player-play');
  final _episodes = FocusNode(debugLabel: 'tv-player-episodes');
  final _settings = FocusNode(debugLabel: 'tv-player-settings');
  final _progress = FocusNode(debugLabel: 'tv-player-progress');
  final _subscriptions = <StreamSubscription<dynamic>>[];
  Timer? _hideTimer;
  Timer? _seekTimer;
  bool _visible = true;
  bool _panelOpen = false;
  bool _seekHint = false;

  @override
  void initState() {
    super.initState();
    _visible = widget.showOnPlaybackReady;
    for (final stream in [
      widget.player.stream.position,
      widget.player.stream.duration,
      widget.player.stream.buffer,
      widget.player.stream.playing,
      widget.player.stream.buffering,
    ]) {
      _subscriptions.add(
        stream.listen((_) {
          if (!mounted) return;
          setState(() {});
        }),
      );
    }
    _subscriptions.add(
      widget.player.stream.playing.listen((playing) {
        if (!mounted || !widget.enabled || _panelOpen) return;
        if (playing) {
          _scheduleHide();
        } else {
          _show();
        }
      }),
    );
    if (widget.enabled) {
      if (widget.showOnPlaybackReady || !widget.player.state.playing) {
        _show();
      } else {
        _visible = false;
        _focus(_surface);
      }
    }
  }

  @override
  void didUpdateWidget(covariant TelevisionControls oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.enabled && !oldWidget.enabled) {
      if (widget.showOnPlaybackReady || !widget.player.state.playing) {
        _show();
      } else {
        setState(() => _visible = false);
        _hideTimer?.cancel();
        _focus(_surface);
      }
    } else if (!widget.enabled) {
      _hideTimer?.cancel();
    }
  }

  void _focus(FocusNode node) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted &&
          widget.enabled &&
          !_panelOpen &&
          node.context != null &&
          (ModalRoute.of(context)?.isCurrent ?? true)) {
        node.requestFocus();
      }
    });
  }

  void _show() {
    final needsFocus =
        !_visible || !_surface.hasFocus || _surface.hasPrimaryFocus;
    if (!_visible) setState(() => _visible = true);
    if (needsFocus) _focus(_play);
    _scheduleHide();
  }

  void _scheduleHide() {
    _hideTimer?.cancel();
    if (!widget.enabled || _panelOpen) return;
    _hideTimer = Timer(const Duration(seconds: 5), () {
      if (mounted &&
          widget.enabled &&
          !_panelOpen &&
          widget.player.state.playing) {
        setState(() => _visible = false);
        if (_surface.hasFocus) _focus(_surface);
      }
    });
  }

  void _seek(int seconds) {
    widget.onSeek(seconds);
    setState(() => _seekHint = true);
    _seekTimer?.cancel();
    _seekTimer = Timer(const Duration(seconds: 2), () {
      if (mounted) setState(() => _seekHint = false);
    });
    _scheduleHide();
  }

  Future<void> _openPanel(
    Future<void> Function() action,
    FocusNode returnFocus,
  ) async {
    if (_panelOpen) return;
    _panelOpen = true;
    _hideTimer?.cancel();
    try {
      await action();
    } finally {
      if (mounted) {
        _panelOpen = false;
        if (widget.enabled) {
          setState(() => _visible = true);
          _focus(returnFocus);
          _scheduleHide();
        }
      }
    }
  }

  KeyEventResult _key(FocusNode node, KeyEvent event) {
    if (!widget.enabled ||
        _panelOpen ||
        (event is! KeyDownEvent && event is! KeyRepeatEvent)) {
      return KeyEventResult.ignored;
    }
    final key = event.logicalKey;
    _scheduleHide();
    if (key == LogicalKeyboardKey.escape || key == LogicalKeyboardKey.goBack) {
      if (event is KeyDownEvent) widget.onBack();
    } else if (key == LogicalKeyboardKey.mediaPlayPause ||
        key == LogicalKeyboardKey.space) {
      if (event is KeyDownEvent) {
        widget.onTogglePlayback();
        _show();
      }
    } else if (key == LogicalKeyboardKey.mediaPlay ||
        key == LogicalKeyboardKey.mediaPause) {
      if (event is KeyDownEvent &&
          widget.player.state.playing !=
              (key == LogicalKeyboardKey.mediaPlay)) {
        widget.onTogglePlayback();
        _show();
      }
    } else if (key == LogicalKeyboardKey.mediaTrackNext) {
      if (event is KeyDownEvent) widget.onNext?.call();
    } else if (key == LogicalKeyboardKey.mediaTrackPrevious) {
      if (event is KeyDownEvent) widget.onPrevious?.call();
    } else if ((!_visible || _progress.hasFocus) &&
        key == LogicalKeyboardKey.arrowLeft) {
      _seek(-10);
    } else if ((!_visible || _progress.hasFocus) &&
        key == LogicalKeyboardKey.arrowRight) {
      _seek(10);
    } else if ((!_visible || _surface.hasPrimaryFocus) &&
        [
          LogicalKeyboardKey.arrowUp,
          LogicalKeyboardKey.arrowDown,
          LogicalKeyboardKey.select,
          LogicalKeyboardKey.enter,
          LogicalKeyboardKey.numpadEnter,
          LogicalKeyboardKey.gameButtonA,
        ].contains(key)) {
      _show();
    } else {
      return KeyEventResult.ignored;
    }
    return KeyEventResult.handled;
  }

  @override
  void dispose() {
    _hideTimer?.cancel();
    _seekTimer?.cancel();
    for (final subscription in _subscriptions) {
      subscription.cancel();
    }
    for (final node in [_surface, _play, _episodes, _settings, _progress]) {
      node.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.player.state;
    final duration = state.duration.inMilliseconds / 1000;
    final position = state.position.inMilliseconds / 1000;
    return ExcludeFocus(
      excluding: !widget.enabled,
      child: Focus(
        focusNode: _surface,
        onKeyEvent: _key,
        child: GestureDetector(
          behavior: HitTestBehavior.opaque,
          onTap: widget.enabled ? _show : null,
          child: Stack(
            fit: StackFit.expand,
            children: [
              if (state.buffering && widget.enabled)
                const Center(child: CircularProgressIndicator()),
              if (_seekHint && !_visible)
                Center(
                  child: DecoratedBox(
                    decoration: BoxDecoration(
                      color: Colors.black87,
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Padding(
                      padding: const EdgeInsets.all(20),
                      child: Text(
                        '${formatPosition(position)} / ${formatPosition(duration)}',
                        style: const TextStyle(fontSize: 26),
                      ),
                    ),
                  ),
                ),
              if (_visible && widget.enabled) ...[
                const DecoratedBox(
                  decoration: BoxDecoration(
                    gradient: LinearGradient(
                      begin: Alignment.topCenter,
                      end: Alignment.bottomCenter,
                      colors: [
                        Color(0xCC000000),
                        Colors.transparent,
                        Color(0xEE000000),
                      ],
                      stops: [0, .4, 1],
                    ),
                  ),
                ),
                Positioned(
                  top: 12,
                  left: 24,
                  right: 24,
                  child: Row(
                    children: [
                      RemoteButton(
                        key: const ValueKey('tv-player-back'),
                        label: '返回',
                        icon: Icons.arrow_back_rounded,
                        onPressed: widget.onBack,
                      ),
                      const SizedBox(width: 16),
                      Expanded(
                        child: Text(
                          widget.title,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontSize: 22,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
                Positioned(
                  left: 28,
                  right: 28,
                  bottom: 18,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      RemoteTarget(
                        key: const ValueKey('tv-progress'),
                        label: '播放进度，左右键快进或后退十秒',
                        focusNode: _progress,
                        onPressed: widget.onTogglePlayback,
                        child: Column(
                          children: [
                            Stack(
                              children: [
                                LinearProgressIndicator(
                                  value: duration > 0
                                      ? (state.buffer.inMilliseconds /
                                                1000 /
                                                duration)
                                            .clamp(0, 1)
                                      : 0,
                                  minHeight: 5,
                                  color: Colors.white38,
                                  backgroundColor: Colors.white24,
                                ),
                                LinearProgressIndicator(
                                  value: duration > 0
                                      ? (position / duration).clamp(0, 1)
                                      : 0,
                                  minHeight: 5,
                                  backgroundColor: Colors.transparent,
                                ),
                              ],
                            ),
                            Row(
                              children: [
                                Text(
                                  '${formatPosition(position)} / ${formatPosition(duration)}',
                                  style: const TextStyle(fontSize: 16),
                                ),
                                const Spacer(),
                                const Text(
                                  '左右快进 · 确认暂停',
                                  style: TextStyle(
                                    fontSize: 14,
                                    color: Colors.white70,
                                  ),
                                ),
                              ],
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 8),
                      SingleChildScrollView(
                        scrollDirection: Axis.horizontal,
                        child: Row(
                          children: [
                            RemoteButton(
                              key: const ValueKey('tv-previous'),
                              label: '上一集',
                              icon: Icons.skip_previous_rounded,
                              onPressed: widget.onPrevious,
                            ),
                            RemoteButton(
                              key: const ValueKey('tv-play-pause'),
                              label: state.playing ? '暂停' : '播放',
                              icon: state.playing
                                  ? Icons.pause_rounded
                                  : Icons.play_arrow_rounded,
                              focusNode: _play,
                              onPressed: () {
                                widget.onTogglePlayback();
                                _scheduleHide();
                              },
                            ),
                            RemoteButton(
                              key: const ValueKey('tv-next'),
                              label: '下一集',
                              icon: Icons.skip_next_rounded,
                              onPressed: widget.onNext,
                            ),
                            RemoteButton(
                              key: const ValueKey('tv-episodes'),
                              label: '选集',
                              icon: Icons.grid_view_rounded,
                              focusNode: _episodes,
                              onPressed: () =>
                                  _openPanel(widget.onEpisodes, _episodes),
                            ),
                            if (widget.enhancement != null)
                              AnimatedBuilder(
                                animation: widget.enhancement!,
                                builder: (_, _) {
                                  final enhancement = widget.enhancement!;
                                  if (!enhancement.canCompare) {
                                    return const SizedBox.shrink();
                                  }
                                  return RemoteButton(
                                    key: const ValueKey(
                                      'tv-enhancement-compare',
                                    ),
                                    label: enhancement.comparing
                                        ? '恢复增强'
                                        : '原画对比',
                                    icon: Icons.compare_rounded,
                                    onPressed: () {
                                      unawaited(enhancement.toggleCompare());
                                      _focus(_play);
                                      _scheduleHide();
                                    },
                                  );
                                },
                              ),
                            RemoteButton(
                              key: const ValueKey('tv-settings'),
                              label: '播放设置',
                              icon: Icons.tune_rounded,
                              focusNode: _settings,
                              onPressed: () =>
                                  _openPanel(widget.onSettings, _settings),
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(height: 6),
                      const Text(
                        '控制条隐藏后：左右快进 10 秒，上下或确认键显示控制条',
                        style: TextStyle(fontSize: 13, color: Colors.white70),
                      ),
                    ],
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class TelevisionEpisodeDialog extends StatelessWidget {
  const TelevisionEpisodeDialog({
    super.key,
    required this.episodes,
    required this.currentIndex,
  });
  final List<Episode> episodes;
  final int currentIndex;

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('选集 · 共 ${episodes.length} 集'),
    content: SizedBox(
      width: 700,
      height: MediaQuery.sizeOf(context).height * .55,
      child: LayoutBuilder(
        builder: (context, constraints) => RemoteGrid(
          itemKeys: episodes.map((episode) => '${episode.number}').toList(),
          columns: ((constraints.maxWidth - 12) / 96).floor().clamp(1, 8),
          itemExtent: 64,
          initialIndex: currentIndex,
          autofocus: true,
          padding: const EdgeInsets.all(6),
          itemBuilder: (_, index, node, onFocus) => RemoteEpisodeButton(
            key: ValueKey('tv-select-episode-${episodes[index].number}'),
            number: episodes[index].number,
            vip: episodes[index].vip,
            current: index == currentIndex,
            focusNode: node,
            onFocus: onFocus,
            onPressed: () => Navigator.pop(context, index),
          ),
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('返回播放'),
      ),
    ],
  );
}

class TelevisionPlaybackSetting {
  const TelevisionPlaybackSetting({
    this.speed,
    this.quality,
    this.autoAdvance,
    this.danmaku,
    this.preload,
    this.enhancement,
  });
  final double? speed;
  final int? quality;
  final bool? autoAdvance;
  final bool? danmaku;
  final bool? preload;
  final VideoEnhancementPreferences? enhancement;
}

class TelevisionSettingsDialog extends StatelessWidget {
  const TelevisionSettingsDialog({
    super.key,
    required this.speed,
    required this.quality,
    required this.qualities,
    required this.favorite,
    required this.onFavorite,
    this.autoAdvance = true,
    this.danmaku = true,
    this.showDanmaku = false,
    this.danmakuStatus = '',
    this.onRetryDanmaku,
    this.preload = true,
    this.preloadStatus = '',
    this.enhancement,
    this.onCompareEnhancement,
  });
  final double speed;
  final int quality;
  final List<int> qualities;
  final bool favorite;
  final VoidCallback onFavorite;
  final bool autoAdvance;
  final bool danmaku;
  final bool showDanmaku;
  final String danmakuStatus;
  final VoidCallback? onRetryDanmaku;
  final bool preload;
  final String preloadStatus;
  final VideoEnhancementController? enhancement;
  final VoidCallback? onCompareEnhancement;

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('播放设置'),
    content: SizedBox(
      width: 640,
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text('倍速', style: TextStyle(fontSize: 18)),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final value in playbackSpeeds)
                  RemoteButton(
                    key: ValueKey('tv-speed-$value'),
                    label: '${value}x',
                    autofocus: value == speed,
                    selected: value == speed,
                    onPressed: () => Navigator.pop(
                      context,
                      TelevisionPlaybackSetting(speed: value),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 20),
            const Text('清晰度', style: TextStyle(fontSize: 18)),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final value in [
                  0,
                  ...qualities.where((value) => value > 0).toSet(),
                ])
                  RemoteButton(
                    key: ValueKey('tv-quality-$value'),
                    label: value == 0 ? '自动（优先高清）' : '${value}P',
                    selected: value == quality,
                    onPressed: () => Navigator.pop(
                      context,
                      TelevisionPlaybackSetting(quality: value),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 20),
            if (enhancement != null)
              VideoEnhancementSettings(
                controller: enhancement!,
                television: true,
                onChanged: (value) => Navigator.pop(
                  context,
                  TelevisionPlaybackSetting(enhancement: value),
                ),
                onCompare: onCompareEnhancement ?? () {},
              ),
            if (showDanmaku) ...[
              RemoteButton(
                key: const ValueKey('tv-danmaku-enabled'),
                label: danmaku ? '弹幕：开' : '弹幕：关',
                onPressed: () => Navigator.pop(
                  context,
                  TelevisionPlaybackSetting(danmaku: !danmaku),
                ),
              ),
              const SizedBox(height: 8),
              Text(danmakuStatus, style: const TextStyle(fontSize: 14)),
              if (onRetryDanmaku != null)
                RemoteButton(
                  key: const ValueKey('tv-danmaku-retry'),
                  label: '重试弹幕',
                  onPressed: onRetryDanmaku,
                ),
              const SizedBox(height: 20),
            ],
            RemoteButton(
              key: const ValueKey('tv-auto-advance'),
              label: autoAdvance ? '自动连播：开' : '自动连播：关',
              onPressed: () => Navigator.pop(
                context,
                TelevisionPlaybackSetting(autoAdvance: !autoAdvance),
              ),
            ),
            const SizedBox(height: 16),
            RemoteButton(
              key: const ValueKey('tv-preload-enabled'),
              label: preload ? '下一集预加载：开' : '下一集预加载：关',
              onPressed: () => Navigator.pop(
                context,
                TelevisionPlaybackSetting(preload: !preload),
              ),
            ),
            const SizedBox(height: 8),
            Text(preloadStatus, style: const TextStyle(fontSize: 14)),
            const SizedBox(height: 20),
            RemoteButton(
              label: favorite ? '取消追剧' : '加入追剧',
              icon: favorite
                  ? Icons.bookmark_rounded
                  : Icons.bookmark_border_rounded,
              onPressed: () {
                onFavorite();
                Navigator.pop(context);
              },
            ),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('返回播放'),
      ),
    ],
  );
}
