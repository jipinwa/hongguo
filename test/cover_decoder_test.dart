import 'dart:io';

import 'package:duanju_app/cover_decoder.dart';
import 'package:duanju_app/media_pipeline.dart';
import 'package:flutter_test/flutter_test.dart';

class CoverExecutor implements MediaExecutor {
  CoverExecutor(this.onStart, this.onEnd, {this.fail = false});
  final void Function() onStart;
  final void Function() onEnd;
  final bool fail;

  @override
  Future<void> run(
    List<String> arguments, {
    double duration = 0,
    void Function(double)? progress,
  }) async {
    onStart();
    try {
      await Future<void>.delayed(const Duration(milliseconds: 10));
      if (fail) throw StateError('synthetic decoder failure');
      await File(
        arguments.last,
      ).writeAsBytes(await File('test/fixtures/cover.jpg').readAsBytes());
    } finally {
      onEnd();
    }
  }

  @override
  Future<MediaProbe> probe(String file) => throw UnimplementedError();
  @override
  Future<void> cancel() async {}
}

void main() {
  test(
    'HEIC conversion serializes work, shares duplicates and reuses JPEG cache',
    () async {
      final directory = await Directory.systemTemp.createTemp(
        'synthetic-cover-',
      );
      addTearDown(() => directory.delete(recursive: true));
      final originals = [
        File('${directory.path}/first.img'),
        File('${directory.path}/second.img'),
      ];
      for (final file in originals) {
        await file.writeAsBytes([1, 2, 3]);
      }
      var active = 0, peak = 0, conversions = 0, preparations = 0;
      final decoder = CoverDecoder(
        createExecutor: () => CoverExecutor(() {
          conversions++;
          active++;
          if (active > peak) peak = active;
        }, () => active--),
      );
      Future<Map<String, dynamic>> prepare() async {
        final file = File('${directory.path}/prepared-${preparations++}.hevc');
        await file.writeAsBytes([0, 0, 0, 1, 2, 3]);
        return {
          'heic': true,
          'input': file.path,
          'filters': 'transpose=cclock',
        };
      }

      final outputs = await Future.wait([
        decoder.convert(originals[0].path, prepare),
        decoder.convert(originals[0].path, prepare),
        decoder.convert(originals[1].path, prepare),
      ]);
      expect(peak, 1);
      expect(conversions, 2);
      expect(preparations, 2);
      expect(outputs[0], outputs[1]);
      expect(
        await File(outputs[0]).readAsBytes(),
        await File('test/fixtures/cover.jpg').readAsBytes(),
      );
      await decoder.convert(originals[0].path, prepare);
      expect(conversions, 2);
      expect(
        directory.listSync().whereType<File>().where(
          (file) => file.path.endsWith('.hevc'),
        ),
        isEmpty,
      );
      for (final file in originals) {
        expect(await file.exists(), isTrue);
      }
    },
  );

  test(
    'conversion failures clean temporary data and allow a later retry',
    () async {
      final directory = await Directory.systemTemp.createTemp(
        'synthetic-cover-failure-',
      );
      addTearDown(() => directory.delete(recursive: true));
      final original = File('${directory.path}/original.img');
      await original.writeAsBytes([1, 2, 3]);
      var fail = true;
      final decoder = CoverDecoder(
        createExecutor: () => CoverExecutor(() {}, () {}, fail: fail),
      );
      Future<Map<String, dynamic>> prepare() async {
        final file = File('${directory.path}/prepared.hevc');
        await file.writeAsBytes([0, 0, 0, 1]);
        return {'heic': true, 'input': file.path};
      }

      await expectLater(
        decoder.convert(original.path, prepare),
        throwsStateError,
      );
      expect(await File('${directory.path}/prepared.hevc').exists(), isFalse);
      expect(await original.exists(), isTrue);
      fail = false;
      final output = await decoder.convert(original.path, prepare);
      expect(await File(output).exists(), isTrue);
      expect(
        directory
            .listSync(recursive: true)
            .where((file) => file.path.endsWith('.part')),
        isEmpty,
      );
    },
  );
}
