import 'package:duanju_app/app_layout.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

void focusRemote(WidgetTester tester, Finder finder) {
  final detector = find
      .descendant(of: finder, matching: find.byType(FocusableActionDetector))
      .first;
  final container = find
      .descendant(of: detector, matching: find.byType(AnimatedContainer))
      .first;
  Focus.of(tester.element(container)).requestFocus();
}

Widget televisionHost({required Widget child}) => MaterialApp(
  theme: televisionTheme(ThemeData.dark()),
  builder: (_, child) => AppLayout(
    television: true,
    child: Shortcuts(
      shortcuts: const {
        SingleActivator(LogicalKeyboardKey.select): ActivateIntent(),
        SingleActivator(LogicalKeyboardKey.goBack): DismissIntent(),
      },
      child: FocusTraversalGroup(child: child!),
    ),
  ),
  home: child,
);
