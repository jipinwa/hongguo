import 'package:flutter/foundation.dart';

import 'models.dart';

class CatalogUpdates extends ChangeNotifier {
  final _items = <String, Drama>{};
  final _coverRetries = <String, int>{};
  Drama? latest;

  Drama current(Drama drama) =>
      _items[drama.id] == null ? drama : drama.merge(_items[drama.id]!);

  int coverRevision(String id) => _coverRetries[id] ?? 0;

  void publish(Drama drama, {bool retryCover = false}) {
    final previous = _items[drama.id];
    latest = previous?.merge(drama) ?? drama;
    _items[drama.id] = latest!;
    if (retryCover) _coverRetries[drama.id] = coverRevision(drama.id) + 1;
    notifyListeners();
  }
}
