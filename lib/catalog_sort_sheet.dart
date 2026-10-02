import 'package:flutter/material.dart';

import 'app_layout.dart';
import 'catalog_sort.dart';

Future<CatalogView?> chooseCatalogView(
  BuildContext context,
  CatalogView current,
) => showModalBottomSheet<CatalogView>(
  context: context,
  showDragHandle: true,
  isScrollControlled: true,
  useSafeArea: true,
  builder: (context) {
    var selected = current;
    return StatefulBuilder(
      builder: (context, update) => SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(
          20,
          0,
          20,
          20 + MediaQuery.viewInsetsOf(context).bottom,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('排序与筛选', style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 16),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final sort in CatalogSort.values)
                  ChoiceChip(
                    autofocus:
                        AppLayout.isTelevision(context) &&
                        sort == CatalogSort.values.first,
                    label: Text(sort.label),
                    selected: selected.sort == sort,
                    onSelected: (_) =>
                        update(() => selected = selected.copyWith(sort: sort)),
                  ),
              ],
            ),
            const SizedBox(height: 24),
            const Text('剧集状态'),
            const SizedBox(height: 8),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                for (final entry in const {
                  '': '全部',
                  'ongoing': '连载中',
                  'finished': '已完结',
                  'unknown': '状态未知',
                }.entries)
                  ChoiceChip(
                    label: Text(entry.value),
                    selected: selected.release == entry.key,
                    onSelected: (_) => update(
                      () => selected = selected.copyWith(release: entry.key),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 16),
            Text(
              '排序和筛选作用于已加载的剧集；缺少排序资料的条目排在最后。',
              style: Theme.of(context).textTheme.bodySmall,
            ),
            const SizedBox(height: 20),
            SizedBox(
              width: double.infinity,
              child: FilledButton(
                onPressed: () => Navigator.pop(context, selected),
                child: const Text('应用'),
              ),
            ),
          ],
        ),
      ),
    );
  },
);
