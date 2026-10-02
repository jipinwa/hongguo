import 'dart:convert';
import 'dart:isolate';
import 'dart:math';

import 'package:crypto/crypto.dart';

import 'models.dart';

class LocalProfile {
  const LocalProfile({
    required this.id,
    required this.name,
    this.admin = false,
    this.sources = const [],
    this.download = true,
    this.salt = '',
    this.pinHash = '',
  });
  final String id;
  final String name;
  final bool admin;
  final List<String> sources;
  final bool download;
  final String salt;
  final String pinHash;
  bool get protected => salt.isNotEmpty && pinHash.isNotEmpty;
  bool allows(String source) => admin || sources.contains(source);

  Map<String, dynamic> toJson() => {
    'id': id,
    'name': name,
    'admin': admin,
    'sources': sources,
    'download': download,
    'salt': salt,
    'pinHash': pinHash,
  };

  factory LocalProfile.fromJson(Map<String, dynamic> value) {
    final id = value['id'] as String;
    final name = (value['name'] as String).trim();
    final admin = value['admin'] == true;
    final sources = (value['sources'] as List).cast<String>().toSet().toList();
    final salt = value['salt'] as String? ?? '';
    final hash = value['pinHash'] as String? ?? '';
    if (!RegExp(r'^[a-zA-Z0-9_-]{1,64}$').hasMatch(id) ||
        value['admin'] is! bool ||
        (value.containsKey('download') && value['download'] is! bool) ||
        name.isEmpty ||
        name.length > 40 ||
        admin != (id == 'default') ||
        sources.any((id) => !SourceSite.isKnown(id)) ||
        (salt.isEmpty != hash.isEmpty) ||
        (salt.isNotEmpty &&
            (!RegExp(r'^[a-f0-9]{32}$').hasMatch(salt) ||
                !RegExp(r'^[a-f0-9]{64}$').hasMatch(hash)))) {
      throw const FormatException('用户配置无效');
    }
    return LocalProfile(
      id: id,
      name: name,
      admin: admin,
      sources: sources,
      download: value['download'] != false,
      salt: salt,
      pinHash: hash,
    );
  }
}

String randomProfileToken() {
  final random = Random.secure();
  return List.generate(
    16,
    (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'),
  ).join();
}

String _derivePin(String pin, String salt) {
  final key = Hmac(sha256, utf8.encode(pin));
  var value = key.convert([...utf8.encode(salt), 0, 0, 0, 1]).bytes;
  final result = List<int>.from(value);
  for (var i = 1; i < 120000; i++) {
    value = key.convert(value).bytes;
    for (var j = 0; j < result.length; j++) {
      result[j] ^= value[j];
    }
  }
  return result.map((byte) => byte.toRadixString(16).padLeft(2, '0')).join();
}

Future<String> hashProfilePin(String pin, String salt) =>
    Isolate.run(() => _derivePin(pin, salt));

Future<bool> checkProfilePin(LocalProfile profile, String pin) async {
  if (!profile.protected) return true;
  final actual = await hashProfilePin(pin, profile.salt);
  var difference = actual.length ^ profile.pinHash.length;
  for (var i = 0; i < actual.length && i < profile.pinHash.length; i++) {
    difference |= actual.codeUnitAt(i) ^ profile.pinHash.codeUnitAt(i);
  }
  return difference == 0;
}
