import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'danmaku_controller.dart';
import 'danmaku_models.dart';

class DanmakuFlight {
  const DanmakuFlight(this.item, this.text, this.lane, this.width);
  final DanmakuItem item;
  final String text;
  final int lane;
  final double width;
}

List<DanmakuFlight> planDanmaku(
  List<DanmakuItem> items, {
  required double width,
  required int rows,
  required double Function(String) measure,
}) {
  if (!width.isFinite || width <= 0 || rows < 1) return const [];
  final lanes = List<DanmakuFlight?>.filled(rows.clamp(1, 4), null);
  final sorted = items.toList()..sort((a, b) => a.timeMs.compareTo(b.timeMs));
  final seen = <String>{};
  final flights = <DanmakuFlight>[];
  for (final item in sorted) {
    if (item.id.isEmpty ||
        item.text.trim().isEmpty ||
        item.timeMs < 0 ||
        !seen.add(item.id)) {
      continue;
    }
    if (flights.reversed
            .takeWhile(
              (flight) => item.timeMs - flight.item.timeMs < danmakuLifetimeMs,
            )
            .length >=
        24) {
      continue;
    }
    final runes = item.text.runes;
    final text = trimDanmakuText(item.text, 100) + (runes.length > 100 ? '…' : '');
    final size = measure(text).ceilToDouble() + 8;
    final lane = lanes.indexWhere((previous) {
      if (previous == null) return true;
      final gap = item.timeMs - previous.item.timeMs;
      if (gap >= danmakuLifetimeMs) return true;
      return (width + previous.width) * gap / danmakuLifetimeMs -
                  previous.width >=
              24 &&
          width -
                  (width + size) *
                      (danmakuLifetimeMs - gap) /
                      danmakuLifetimeMs >=
              24;
    });
    if (lane < 0) continue;
    final flight = DanmakuFlight(item, text, lane, size);
    lanes[lane] = flight;
    flights.add(flight);
  }
  return flights;
}

class DanmakuOverlay extends StatefulWidget {
  const DanmakuOverlay({
    super.key,
    required this.controller,
    required this.aspectRatio,
  });
  final DanmakuController controller;
  final double aspectRatio;

  @override
  State<DanmakuOverlay> createState() => _DanmakuOverlayState();
}

class _DanmakuOverlayState extends State<DanmakuOverlay> {
  Object? _layout;
  List<DanmakuFlight> _schedule = const [];

  @override
  Widget build(BuildContext context) => IgnorePointer(
    child: ExcludeSemantics(
      child: LayoutBuilder(
        builder: (context, constraints) {
          if (!constraints.hasBoundedWidth ||
              !constraints.hasBoundedHeight ||
              constraints.maxWidth <= 0 ||
              constraints.maxHeight <= 0) {
            return const SizedBox.shrink();
          }
          final aspect = widget.aspectRatio.isFinite && widget.aspectRatio > 0
              ? widget.aspectRatio
              : 16 / 9;
          final width = math.min(
            constraints.maxWidth,
            constraints.maxHeight * aspect,
          );
          final height = math.min(
            constraints.maxHeight,
            constraints.maxWidth / aspect,
          );
          final fontSize = width < 600 ? 16.0 : 20.0;
          final scaler = MediaQuery.textScalerOf(context);
          final rowHeight = scaler.scale(fontSize) * 1.5;
          final rows = ((height * .55 - 8) / rowHeight).floor().clamp(0, 4);
          final style = TextStyle(
            fontSize: fontSize,
            fontWeight: FontWeight.w600,
            color: Colors.white,
            height: 1.2,
            shadows: const [
              Shadow(color: Colors.black, blurRadius: 3, offset: Offset(1, 1)),
            ],
          );
          return Center(
            child: SizedBox(
              width: width,
              height: height,
              child: ClipRect(
                child: AnimatedBuilder(
                  animation: widget.controller,
                  builder: (context, _) {
                    final controller = widget.controller;
                    if (!controller.visible || rows == 0) {
                      return const SizedBox.shrink();
                    }
                    final layout = (
                      controller,
                      controller.dataRevision,
                      width,
                      rows,
                      fontSize,
                      scaler,
                    );
                    if (_layout != layout) {
                      _layout = layout;
                      _schedule = planDanmaku(
                        controller.items,
                        width: width,
                        rows: rows,
                        measure: (text) {
                          final painter = TextPainter(
                            text: TextSpan(text: text, style: style),
                            textDirection: TextDirection.ltr,
                            textScaler: scaler,
                            maxLines: 1,
                          )..layout();
                          final measured = painter.width;
                          painter.dispose();
                          return measured;
                        },
                      );
                    }
                    return Stack(
                      clipBehavior: Clip.none,
                      children: [
                        for (final flight in _schedule)
                          if (controller.positionMs >= flight.item.timeMs &&
                              controller.positionMs <
                                  flight.item.timeMs + danmakuLifetimeMs)
                            Positioned(
                              key: ValueKey('danmaku-item-${flight.item.id}'),
                              left: 0,
                              top: 8 + flight.lane * rowHeight,
                              width: flight.width,
                              height: rowHeight,
                              child: _DanmakuMotion(
                                flight: flight,
                                width: width,
                                positionMs: controller.positionMs,
                                moving: controller.moving,
                                rate: controller.rate,
                                revision: controller.motionRevision,
                                style: style,
                                scaler: scaler,
                              ),
                            ),
                      ],
                    );
                  },
                ),
              ),
            ),
          );
        },
      ),
    ),
  );
}

class _DanmakuMotion extends StatefulWidget {
  const _DanmakuMotion({
    required this.flight,
    required this.width,
    required this.positionMs,
    required this.moving,
    required this.rate,
    required this.revision,
    required this.style,
    required this.scaler,
  });
  final DanmakuFlight flight;
  final double width;
  final int positionMs;
  final bool moving;
  final double rate;
  final int revision;
  final TextStyle style;
  final TextScaler scaler;

  @override
  State<_DanmakuMotion> createState() => _DanmakuMotionState();
}

class _DanmakuMotionState extends State<_DanmakuMotion>
    with SingleTickerProviderStateMixin {
  late final AnimationController _motion = AnimationController(vsync: this);

  @override
  void initState() {
    super.initState();
    _sync();
  }

  @override
  void didUpdateWidget(_DanmakuMotion oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.revision != widget.revision ||
        oldWidget.width != widget.width ||
        oldWidget.flight.width != widget.flight.width ||
        oldWidget.flight.item.timeMs != widget.flight.item.timeMs) {
      _sync();
    }
  }

  void _sync() {
    _motion.stop();
    final elapsed = (widget.positionMs - widget.flight.item.timeMs).clamp(
      0,
      danmakuLifetimeMs,
    );
    _motion.value = elapsed / danmakuLifetimeMs;
    if (widget.moving && elapsed < danmakuLifetimeMs) {
      _motion.animateTo(
        1,
        duration: Duration(
          microseconds: ((danmakuLifetimeMs - elapsed) * 1000 / widget.rate)
              .round()
              .clamp(1, 86400000000),
        ),
        curve: Curves.linear,
      );
    }
  }

  @override
  void dispose() {
    _motion.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
    animation: _motion,
    child: RepaintBoundary(
      child: Text(
        widget.flight.text,
        style: widget.style,
        textScaler: widget.scaler,
        textDirection: TextDirection.ltr,
        maxLines: 1,
        softWrap: false,
      ),
    ),
    builder: (context, child) => Transform.translate(
      key: ValueKey('danmaku-motion-${widget.flight.item.id}'),
      offset: Offset(
        widget.width - (widget.width + widget.flight.width) * _motion.value,
        0,
      ),
      child: child,
    ),
  );
}
