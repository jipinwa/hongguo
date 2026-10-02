import 'dart:convert';
import 'dart:io';

import 'package:integration_test/integration_test_driver_extended.dart';

Future<void> main() async {
  final output = Directory('build/device-test/results/appearance');
  await output.create(recursive: true);
  await integrationDriver(
    onScreenshot: (name, bytes, [args]) async {
      await File('${output.path}/$name.png').writeAsBytes(bytes);
      return true;
    },
    writeResponseOnFailure: true,
    responseDataCallback: (data) async {
      final report = Map<String, dynamic>.from(data ?? {})
        ..remove('screenshots');
      await File(
        '${output.path}/appearance.json',
      ).writeAsString(const JsonEncoder.withIndent('  ').convert(report));
    },
  );
}
