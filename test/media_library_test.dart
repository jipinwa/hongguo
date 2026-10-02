import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/media_library.dart';
import 'package:duanju_app/media_pipeline.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:path/path.dart' as path;
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class ProcessMediaExecutor implements MediaExecutor {
  final commands = <List<String>>[];
  Process? _process;

  @override
  Future<MediaProbe> probe(String file) async {
    final result = await Process.run('ffprobe', [
      '-v',
      'error',
      '-show_format',
      '-show_streams',
      '-show_data_hash',
      'sha256',
      '-of',
      'json',
      file,
    ]);
    if (result.exitCode != 0) throw StateError(result.stderr.toString());
    return MediaProbe(
      jsonDecode(result.stdout as String) as Map<String, dynamic>,
    );
  }

  @override
  Future<void> run(
    List<String> arguments, {
    double duration = 0,
    void Function(double)? progress,
  }) async {
    commands.add(List.from(arguments));
    final process = await Process.start('ffmpeg', [
      '-v',
      'error',
      '-y',
      '-protocol_whitelist',
      'file,crypto,data',
      ...arguments,
    ]);
    _process = process;
    final stdout = utf8.decoder.bind(process.stdout).join();
    final stderr = utf8.decoder.bind(process.stderr).join();
    final code = await process.exitCode;
    await stdout;
    final errors = await stderr;
    _process = null;
    if (code != 0) throw StateError(errors);
    progress?.call(1);
  }

  @override
  Future<void> cancel() async {
    _process?.kill();
  }
}

class FileRepository extends FixtureRepository {
  FileRepository(this.root, this.files, this.jobs);
  final String root;
  final List<String> files;
  final List<DownloadJob> jobs;
  bool leased = false;
  void Function()? onLocalPlayback;
  @override
  Future<String> downloadDirectory() async => root;
  @override
  Future<int> workLease(String id, String command) async {
    if (command == 'start') {
      if (leased) throw StateError('leased');
      leased = true;
    } else if (command == 'end') {
      leased = false;
    }
    return leased ? 1 : 0;
  }

  @override
  Future<List<DownloadJob>> downloads() async => jobs;
  @override
  Future<PlaybackPlan?> localPlayback(Drama drama, Episode episode) async {
    onLocalPlayback?.call();
    return PlaybackPlan(
      url: files[episode.number - 1],
      local: true,
      decryptionKey: episode.number == 3
          ? '00112233445566778899aabbccddeeff'
          : '',
    );
  }
}

MediaProbe fixtureProbe({
  String codec = 'h264',
  int width = 320,
  String audio = 'aac',
  int rate = 44100,
}) => MediaProbe({
  'format': {'duration': '4'},
  'streams': [
    {
      'codec_type': 'video',
      'codec_name': codec,
      'width': width,
      'height': 180,
      'pix_fmt': 'yuv420p',
      'sample_aspect_ratio': '1:1',
    },
    if (audio.isNotEmpty)
      {
        'codec_type': 'audio',
        'codec_name': audio,
        'sample_rate': '$rate',
        'channels': 2,
        'channel_layout': 'stereo',
      },
  ],
});

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
    'merge retains every matching video and normalizes only the minority',
    () {
      final plan = MergePlan.create([
        fixtureProbe(),
        fixtureProbe(),
        fixtureProbe(codec: 'hevc', width: 640),
      ]);
      expect(plan.videoTranscodes, 1);
      expect(plan.audioTranscodes, 0);
      expect(plan.video.videoCodec, 'h264');
      final audioOnly = MergePlan.create([
        fixtureProbe(),
        fixtureProbe(),
        fixtureProbe(rate: 48000),
      ]);
      expect(audioOnly.videoTranscodes, 0);
      expect(audioOnly.audioTranscodes, 1);
      final identical = MergePlan.create([fixtureProbe(), fixtureProbe()]);
      expect(identical.videoTranscodes, 0);
      expect(identical.audioTranscodes, 0);
    },
  );

  test('missing audio gets silence without discarding existing sound', () {
    final plan = MergePlan.create([
      fixtureProbe(audio: ''),
      fixtureProbe(audio: ''),
      fixtureProbe(),
    ]);
    expect(plan.audio, isNotNull);
    expect(plan.videoTranscodes, 0);
    expect(plan.audioTranscodes, 2);
  });

  test(
    'NFO escapes metadata and includes an HTTP poster URL without fetching it',
    () {
      const drama = Drama(
        id: 'hongguo:1',
        source: 'hongguo',
        title: '甲 & <乙>',
        cover: 'https://example.invalid/poster.jpg?a=1&b=2',
      );
      final body = embyShowNfo(drama);
      expect(body, contains('甲 &amp; &lt;乙&gt;'));
      expect(body, contains('https://example.invalid/poster.jpg?a=1&amp;b=2'));
      expect(body, contains('<thumb aspect="poster">'));
      expect(
        embyShowNfo(drama, localPoster: true),
        contains('>poster.jpg</thumb>'),
      );
      expect(
        embyShowNfo(
          const Drama(
            id: '1',
            source: 'hongguo',
            title: 'x',
            cover: 'file:///secret',
          ),
        ),
        isNot(contains('<thumb')),
      );
      expect(
        () => LocalMediaItem.fromJson({'file': '../elsewhere'}),
        throwsFormatException,
      );
    },
  );

  test(
    'cancelling while resolving local media releases the lease without starting FFmpeg',
    () async {
      final directory = await Directory.systemTemp.createTemp('zgj-cancel-');
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final executor = ProcessMediaExecutor();
      const drama = Drama(
        id: 'hongguo:cancel',
        source: 'hongguo',
        title: '取消测试',
      );
      final jobs = [
        for (var i = 0; i < 2; i++)
          DownloadJob(
            id: 'cancel-$i',
            drama: drama,
            episode: Episode({'id': '$i', 'currentEpisode': i + 1}, i + 1),
            state: 'completed',
            created: i + 1,
          ),
      ];
      final repository = FileRepository(directory.path, [
        'unused-1',
        'unused-2',
      ], jobs);
      final library = MediaLibrary(repository, store, executor: executor);
      repository.onLocalPlayback = () => unawaited(library.cancel());
      try {
        await expectLater(
          library.merge(jobs),
          throwsA(
            predicate((Object error) => error.toString().contains('已取消')),
          ),
        );
        expect(executor.commands, isEmpty);
        expect(repository.leased, isFalse);
        expect(library.busy, isFalse);
        expect(directory.listSync(), isEmpty);
      } finally {
        library.dispose();
        store.dispose();
        await directory.delete(recursive: true);
      }
    },
  );

  bool available;
  try {
    available = Process.runSync('ffmpeg', ['-version']).exitCode == 0;
  } catch (_) {
    available = false;
  }

  test(
    'real offline merge, CENC remux and Emby export remain playable after deleting inputs',
    () async {
      final directory = await Directory.systemTemp.createTemp("真果鉴 '合成-");
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final executor = ProcessMediaExecutor();
      MediaLibrary? library;
      try {
        final files = <String>[];
        for (var i = 0; i < 3; i++) {
          final file = path.join(directory.path, 'input-$i.mp4');
          final result = await Process.run('ffmpeg', [
            '-v',
            'error',
            '-y',
            '-f',
            'lavfi',
            '-i',
            'testsrc2=size=${i == 2 ? '320x180' : '160x90'}:rate=12',
            '-f',
            'lavfi',
            '-i',
            'sine=frequency=440:sample_rate=44100',
            '-t',
            '2',
            '-c:v',
            'libx264',
            '-threads',
            '1',
            '-g',
            '12',
            '-pix_fmt',
            'yuv420p',
            '-c:a',
            'aac',
            '-ac',
            '2',
            if (i == 2) ...[
              '-encryption_scheme',
              'cenc-aes-ctr',
              '-encryption_key',
              '00112233445566778899aabbccddeeff',
              '-encryption_kid',
              '11223344556677889900aabbccddeeff',
            ],
            file,
          ]);
          expect(result.exitCode, 0, reason: result.stderr.toString());
          files.add(file);
        }
        const drama = Drama(
          id: 'hongguo:100',
          source: 'hongguo',
          title: '合成 <&> 测试',
          episodes: 3,
          cover: 'https://example.invalid/poster.jpg',
        );
        final jobs = [
          for (var i = 0; i < 3; i++)
            DownloadJob(
              id: 'job$i',
              drama: drama,
              episode: Episode({'id': '$i', 'currentEpisode': i + 1}, i + 1),
              state: 'completed',
              created: i + 1,
            ),
        ];
        final repository = FileRepository(directory.path, files, jobs);
        library = MediaLibrary(repository, store, executor: executor);
        final merged = await library.merge(jobs);
        expect(merged.videoTranscodes, 1);
        expect(merged.audioTranscodes, 0);
        expect(repository.leased, isFalse);
        expect(merged.episodes, [1, 2, 3]);
        await library.exportJobs(jobs);
        final exported = library.items.where((i) => !i.merged).toList();
        expect(exported, hasLength(3));
        final show = File(library.fileFor(exported.first)).parent.parent;
        await File(path.join(show.path, 'tvshow.nfo')).delete();
        final episodeNfo = File(
          path.setExtension(library.fileFor(exported.first), '.nfo'),
        );
        await episodeNfo.delete();
        final commandsBefore = executor.commands.length;
        await library.exportJobs(jobs);
        expect(await episodeNfo.exists(), isTrue);
        expect(
          executor.commands.length,
          commandsBefore,
          reason: 'unchanged exports should not be remuxed again',
        );
        final nfo = await File(
          path.join(show.path, 'tvshow.nfo'),
        ).readAsString();
        expect(nfo, contains('合成 &lt;&amp;&gt; 测试'));
        expect(nfo, contains('https://example.invalid/poster.jpg'));
        for (final file in files) {
          await File(file).delete();
        }
        for (final item in library.items) {
          final decoded = await Process.run('ffmpeg', [
            '-v',
            'error',
            '-xerror',
            '-protocol_whitelist',
            'file,crypto,data',
            '-i',
            library.fileFor(item),
            '-f',
            'null',
            '-',
          ]);
          expect(
            decoded.exitCode,
            0,
            reason: '${item.file}: ${decoded.stderr}',
          );
          verifyMediaDuration(
            await executor.probe(library.fileFor(item)),
            item.merged ? 6 : 2,
          );
        }
        expect(
          directory.listSync().where(
            (entry) => path.basename(entry.path).startsWith('.media-work-'),
          ),
          isEmpty,
        );
        await library.remove(exported.first);
        expect(await File(library.fileFor(exported.first)).exists(), isFalse);
        expect(await File(library.fileFor(merged)).exists(), isTrue);
        await library.reload();
        expect(library.items, hasLength(3));
      } finally {
        library?.dispose();
        store.dispose();
        await directory.delete(recursive: true);
      }
    },
    skip: available ? false : 'requires ffmpeg and ffprobe',
    timeout: const Timeout(Duration(minutes: 3)),
  );
}
