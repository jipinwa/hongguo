import 'dart:async';

import 'lan_sync_models.dart';
import 'models.dart';

class LanPeer {
  const LanPeer({
    required this.id,
    required this.name,
    required this.pin,
    required this.kind,
    required this.port,
    this.addresses = const [],
  });
  final String id, name, pin, kind;
  final int port;
  final List<String> addresses;

  factory LanPeer.fromJson(Map<String, dynamic> row) {
    final id = lanText(row['deviceId'], 32);
    final pin = lanText(row['pin'], 64);
    final port = row['port'];
    final kind = row['kind'];
    if (!RegExp(r'^[a-f0-9]{32}$').hasMatch(id) ||
        !RegExp(r'^[a-f0-9]{64}$').hasMatch(pin) ||
        port is! int ||
        port < 1 ||
        port > 65535 ||
        !{'phone', 'computer', 'tv'}.contains(kind) ||
        row['protocol'] != 1) {
      throw const FormatException('设备信息或协议版本无效');
    }
    return LanPeer(
      id: id,
      name: lanText(row['name'], 80),
      pin: pin,
      kind: kind as String,
      port: port,
      addresses: [
        for (final address in (row['addresses'] as List? ?? []))
          lanText(address, 160),
        if (row['address'] is String) lanText(row['address'], 160),
      ].take(16).toSet().toList(),
    );
  }

  LanPeer withAddresses(Iterable<String> values) => LanPeer(
    id: id,
    name: name,
    pin: pin,
    kind: kind,
    port: port,
    addresses: values.toSet().take(16).toList(),
  );

  Map<String, dynamic> toJson() => {
    'deviceId': id,
    'name': name,
    'pin': pin,
    'kind': kind,
    'port': port,
    'addresses': addresses,
    'protocol': 1,
  };
}

class LanConnection {
  LanConnection({
    required this.peer,
    required this.address,
    required this.token,
    required this.account,
    required this.user,
    required this.sources,
    required this.autoSync,
  });
  final LanPeer peer;
  final String address, token, account, user;
  final Set<String> sources;
  bool autoSync;
}

const lanLegacySources = {
  'hongguo',
  'huangdou',
  'huangguo-video',
  'huangguoai',
  'cloudfront',
};

Set<String> lanSources(Object? value, {bool advertised = false}) {
  if (value is! List ||
      value.length > (advertised ? 32 : SourceSite.allValues.length)) {
    throw const FormatException('设备站源范围无效');
  }
  final result = value.map((source) => lanText(source, 32)).toSet();
  if (advertised) {
    return result.where(SourceSite.isKnown).toSet();
  }
  if (result.any((source) => !SourceSite.isKnown(source))) {
    throw const FormatException('设备站源范围无效');
  }
  return result;
}

class LanPreview {
  LanPreview({
    required this.operation,
    required this.mode,
    required this.connection,
    required this.epoch,
    required this.sources,
    required this.localBase,
    required this.remoteBase,
    required this.result,
    required this.localChanges,
    required this.remoteChanges,
    required this.localCount,
    required this.remoteCount,
    required this.skipped,
  });
  final String operation, localBase, remoteBase;
  final LanSyncMode mode;
  final LanConnection connection;
  final int epoch, skipped;
  final Set<String> sources;
  final Map<String, LanRecord> result;
  final List<LanRecord> localChanges, remoteChanges;
  final LanChangeCount localCount, remoteCount;
  final DateTime created = DateTime.now();
}

class LanPlaybackIntent {
  const LanPlaybackIntent({
    required this.drama,
    required this.episodeID,
    required this.episode,
    required this.position,
    this.playing = true,
  });
  final Drama drama;
  final String episodeID;
  final int episode;
  final double position;
  final bool playing;

  factory LanPlaybackIntent.fromJson(Object? value) {
    final row = lanMap(value);
    final drama = lanDrama(row['drama']);
    final episodeID = lanText(row['episodeId'], 1024, empty: true);
    final episode = row['episode'];
    final position = row['position'];
    if (episode is! int ||
        episode < 1 ||
        episode > 1000000 ||
        position is! num ||
        !position.isFinite ||
        position < 0 ||
        position > 604800 ||
        row['playing'] is! bool) {
      throw const FormatException('推送播放信息无效');
    }
    return LanPlaybackIntent(
      drama: drama,
      episodeID: episodeID,
      episode: episode,
      position: position.toDouble(),
      playing: row['playing'] as bool,
    );
  }

  Map<String, dynamic> toJson() => {
    'drama': lanCompactDrama(drama).toJson(),
    'episodeId': episodeID,
    'episode': episode,
    'position': position,
    'playing': playing,
  };
  String get identity => lanHash([drama.id, episodeID, episode]);
}

class LanIncomingPlayback {
  LanIncomingPlayback({
    required this.id,
    required this.intent,
    required this.detail,
    required this.index,
    required this.plan,
    required this.profileEpoch,
  });
  final String id;
  final LanPlaybackIntent intent;
  final DramaDetail detail;
  final int index;
  final PlaybackPlan plan;
  final int profileEpoch;
  final Completer<Map<String, dynamic>> started = Completer();
  double position = 0;
  bool cancelled = false;
  bool consumed = false;
  Future<void> Function()? stop;

  void fail(String message) {
    if (!started.isCompleted) {
      started.complete({'state': 'failed', 'message': message});
    }
  }

  void acknowledge(double position) {
    if (!cancelled && !started.isCompleted) {
      started.complete({'state': 'playing', 'position': position});
    }
  }
}

class LanPlaybackHost {
  LanPlaybackHost({
    required this.identity,
    required this.title,
    required this.stop,
  });
  final Object identity;
  final String title;
  final Future<void> Function() stop;
}
