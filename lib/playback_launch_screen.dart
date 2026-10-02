import 'dart:async';

import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'follow_state.dart';
import 'local_store.dart';
import 'models.dart';
import 'player_screen.dart';
import 'widgets.dart';

Future<void> openPlaybackDirectly(
  BuildContext context, {
  required Drama drama,
  required AppRepository repository,
  required LocalStore store,
}) async {
  final profileEpoch = store.profileEpoch;
  try {
    final detail = await repository.detail(drama);
    if (!context.mounted || profileEpoch != store.profileEpoch) return;
    final mergedDrama = repository.catalogUpdates
        .current(drama)
        .merge(detail.drama);
    final merged = DramaDetail(
      mergedDrama,
      detail.episodes,
      warning: detail.warning,
    );
    repository.catalogUpdates.publish(mergedDrama, retryCover: true);
    await saveUserChange(context, () => store.refreshDrama(mergedDrama));
    if (!context.mounted || profileEpoch != store.profileEpoch) return;
    if (merged.episodes.isEmpty) {
      throw AppFailure('暂时没有可播放的集数');
    }
    final watched = store.watched(mergedDrama.id);
    final index = resumeEpisodeIndex(merged.episodes, watched);
    final episode = merged.episodes[index];
    if (episode.vip && mergedDrama.source != SourceSite.dsd.id) {
      final accepted = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          title: const Text('这是一集 VIP 内容'),
          content: const Text('站源可能只提供试看或限制播放。'),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('取消'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('尝试播放'),
            ),
          ],
        ),
      );
      if (accepted != true || !context.mounted) return;
    }
    final position =
        watched?.episode == episode.number && watched?.finished == false
        ? watched!.position
        : 0.0;
    if (!context.mounted || profileEpoch != store.profileEpoch) return;
    await Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => PlayerScreen(
          detail: merged,
          initialIndex: index,
          initialPosition: position,
          repository: repository,
          store: store,
        ),
      ),
    );
  } catch (error) {
    if (!context.mounted || profileEpoch != store.profileEpoch) return;
    repository.catalogUpdates.publish(
      repository.catalogUpdates.current(drama),
      retryCover: true,
    );
    final message = error is AppFailure ? error.message : '暂时无法播放，请重试';
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(message)));
  }
}

class PlaybackLaunchScreen extends StatefulWidget {
  const PlaybackLaunchScreen({
    super.key,
    required this.drama,
    required this.repository,
    required this.store,
  });

  final Drama drama;
  final AppRepository repository;
  final LocalStore store;

  @override
  State<PlaybackLaunchScreen> createState() => _PlaybackLaunchScreenState();
}

class _PlaybackLaunchScreenState extends State<PlaybackLaunchScreen> {
  late final int _profileEpoch;
  int _generation = 0;
  String? _error;

  @override
  void initState() {
    super.initState();
    _profileEpoch = widget.store.profileEpoch;
    unawaited(_load());
  }

  @override
  void dispose() {
    _generation++;
    super.dispose();
  }

  Future<void> _load() async {
    final generation = ++_generation;
    setState(() => _error = null);
    try {
      final detail = await widget.repository.detail(widget.drama);
      if (!mounted ||
          generation != _generation ||
          _profileEpoch != widget.store.profileEpoch) {
        return;
      }
      final drama = widget.repository.catalogUpdates
          .current(widget.drama)
          .merge(detail.drama);
      final merged = DramaDetail(
        drama,
        detail.episodes,
        warning: detail.warning,
      );
      widget.repository.catalogUpdates.publish(drama, retryCover: true);
      await saveUserChange(context, () => widget.store.refreshDrama(drama));
      if (!mounted ||
          generation != _generation ||
          _profileEpoch != widget.store.profileEpoch) {
        return;
      }
      if (merged.episodes.isEmpty) {
        setState(() => _error = '暂时没有可播放的集数');
        return;
      }
      final watched = widget.store.watched(drama.id);
      final index = resumeEpisodeIndex(merged.episodes, watched);
      final episode = merged.episodes[index];
      if (episode.vip && drama.source != SourceSite.dsd.id) {
        if (!mounted) return;
        final accepted = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: const Text('这是一集 VIP 内容'),
            content: const Text('站源可能只提供试看或限制播放。'),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('取消'),
              ),
              FilledButton(
                onPressed: () => Navigator.pop(context, true),
                child: const Text('尝试播放'),
              ),
            ],
          ),
        );
        if (accepted != true) {
          if (mounted) Navigator.of(context).maybePop();
          return;
        }
        if (!mounted) {
          return;
        }
      }
      final position =
          watched?.episode == episode.number && watched?.finished == false
          ? watched!.position
          : 0.0;
      if (!mounted ||
          generation != _generation ||
          _profileEpoch != widget.store.profileEpoch) {
        return;
      }
      Navigator.of(context).pushReplacement(
        MaterialPageRoute<void>(
          builder: (_) => PlayerScreen(
            detail: merged,
            initialIndex: index,
            initialPosition: position,
            repository: widget.repository,
            store: widget.store,
          ),
        ),
      );
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() => _error = error.toString());
      widget.repository.catalogUpdates.publish(
        widget.repository.catalogUpdates.current(widget.drama),
        retryCover: true,
      );
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: Text(widget.drama.title)),
    body: SafeArea(
      top: false,
      child: _error == null
          ? const Center(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  CircularProgressIndicator(),
                  SizedBox(height: 16),
                  Text('正在进入播放'),
                ],
              ),
            )
          : StatusPanel(
              title: '暂时无法播放',
              message: _error!,
              onRetry: _load,
              action: '重试',
              icon: Icons.play_disabled_rounded,
            ),
    ),
  );
}
