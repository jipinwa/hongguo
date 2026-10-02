import 'package:flutter/material.dart';

class AppBottomNavigation extends StatelessWidget {
  const AppBottomNavigation({
    super.key,
    required this.selectedIndex,
    required this.onDestinationSelected,
    required this.destinations,
  });

  final int selectedIndex;
  final ValueChanged<int> onDestinationSelected;
  final List<NavigationDestination> destinations;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    return Material(
      color: theme.scaffoldBackgroundColor,
      child: SafeArea(
        top: false,
        child: DecoratedBox(
          decoration: BoxDecoration(
            border: Border(
              top: BorderSide(color: colors.outlineVariant, width: .5),
            ),
          ),
          child: Row(
            children: [
              for (final (index, destination) in destinations.indexed)
                Expanded(
                  child: Semantics(
                    container: true,
                    button: true,
                    selected: index == selectedIndex,
                    child: Tooltip(
                      message: destination.label,
                      excludeFromSemantics: true,
                      child: InkWell(
                        key: ValueKey('bottom-nav-$index'),
                        onTap: () => onDestinationSelected(index),
                        child: ConstrainedBox(
                          constraints: const BoxConstraints(
                            minHeight: 68,
                            minWidth: 48,
                          ),
                          child: Padding(
                            padding: const EdgeInsets.symmetric(
                              horizontal: 4,
                              vertical: 9,
                            ),
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              children: [
                                AnimatedContainer(
                                  duration: const Duration(milliseconds: 160),
                                  curve: Curves.easeOut,
                                  width: 18,
                                  height: 3,
                                  decoration: BoxDecoration(
                                    color: index == selectedIndex
                                        ? colors.primary
                                        : Colors.transparent,
                                    borderRadius: BorderRadius.circular(1.5),
                                  ),
                                ),
                                const SizedBox(height: 6),
                                IconTheme(
                                  data: IconThemeData(
                                    size: 24,
                                    color: index == selectedIndex
                                        ? colors.primary
                                        : colors.onSurfaceVariant,
                                  ),
                                  child: index == selectedIndex
                                      ? destination.selectedIcon ??
                                            destination.icon
                                      : destination.icon,
                                ),
                                const SizedBox(height: 4),
                                Text(
                                  destination.label,
                                  textAlign: TextAlign.center,
                                  maxLines: 2,
                                  overflow: TextOverflow.ellipsis,
                                  style: TextStyle(
                                    fontSize: 12,
                                    height: 1.2,
                                    fontWeight: index == selectedIndex
                                        ? FontWeight.w700
                                        : FontWeight.w500,
                                    color: index == selectedIndex
                                        ? colors.primary
                                        : colors.onSurfaceVariant,
                                  ),
                                ),
                              ],
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
