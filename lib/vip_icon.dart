import 'package:flutter/material.dart';

class VipIcon extends StatelessWidget {
  const VipIcon({super.key, this.hidden = false});
  final bool hidden;

  @override
  Widget build(BuildContext context) {
    final color =
        IconTheme.of(context).color ?? Theme.of(context).colorScheme.onSurface;
    return ExcludeSemantics(
      child: CustomPaint(
        foregroundPainter: hidden ? _VipSlash(color) : null,
        child: SizedBox(
          width: 30,
          height: 26,
          child: Center(
            child: Text(
              'VIP',
              textScaler: TextScaler.noScaling,
              style: TextStyle(
                color: color,
                fontSize: 12,
                fontWeight: FontWeight.w900,
                letterSpacing: -.4,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _VipSlash extends CustomPainter {
  const _VipSlash(this.color);
  final Color color;
  @override
  void paint(Canvas canvas, Size size) {
    canvas.drawLine(
      Offset(3, size.height - 3),
      Offset(size.width - 3, 3),
      Paint()
        ..color = color
        ..strokeWidth = 2.2
        ..strokeCap = StrokeCap.round,
    );
  }

  @override
  bool shouldRepaint(_VipSlash oldDelegate) => oldDelegate.color != color;
}
