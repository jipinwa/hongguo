import 'dart:convert';
import 'dart:io';

import 'package:media_kit/media_kit.dart';

import 'core_bridge.dart';
import 'media_pipeline.dart';

Future<void> runPackageSmoke(List<String> arguments) async {
  if (arguments.length != 3) exit(2);
  final report = File(arguments[1]), input = arguments[2];
  Player? player;
  try {
    final repository = NativeRepository();
    await repository.initialize();
    final executor = FFmpegExecutor();
    final probe = await executor.probe(input);
    verifyMediaDuration(probe, 3);
    final remuxed = File(
      '${report.parent.path}${Platform.pathSeparator}remuxed.mkv',
    );
    await executor.run([
      '-i',
      input,
      '-map',
      '0:v:0',
      '-c',
      'copy',
      remuxed.path,
    ]);
    verifyMediaDuration(await executor.probe(remuxed.path), 3);
    player = Player(
      configuration: const PlayerConfiguration(muted: true, vo: 'null'),
    );
    await player.setAudioTrack(AudioTrack.no());
    final advancing = player.stream.position.firstWhere(
      (time) => time.inMilliseconds >= 400,
    );
    await player.open(Media(remuxed.path));
    await advancing.timeout(const Duration(seconds: 20));
    await report.writeAsString(
      jsonEncode({
        'ok': true,
        'nativeCore': true,
        'ffprobe': true,
        'remux': true,
        'mediaKitPlayback': true,
      }),
      flush: true,
    );
    await player.dispose();
    exit(0);
  } catch (error) {
    await player?.dispose();
    await report.writeAsString(
      jsonEncode({'ok': false, 'error': error.toString()}),
      flush: true,
    );
    exit(1);
  }
}
