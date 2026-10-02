import tempfile
import unittest
from pathlib import Path

from build_mirrors import china_mirror_environment, mirrored_pub_lockfile


class BuildMirrorTests(unittest.TestCase):
    def test_default_leaves_environment_and_gradle_directory_untouched(self):
        with tempfile.TemporaryDirectory() as temporary:
            gradle_directory = Path(temporary) / 'gradle'
            environment = {'GRADLE_USER_HOME': str(gradle_directory)}
            with china_mirror_environment(environment, False) as result:
                self.assertEqual(result, environment)
                self.assertIsNot(result, environment)
            self.assertFalse(gradle_directory.exists())

    def test_scoped_configuration_preserves_existing_files_and_cleans_after_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            gradle_directory = Path(temporary) / 'gradle'
            init_directory = gradle_directory / 'init.d'
            init_directory.mkdir(parents=True)
            existing = init_directory / 'user.gradle'
            existing.write_text('user configuration', encoding='utf-8')
            environment = {
                'GRADLE_USER_HOME': str(gradle_directory),
                'PUB_HOSTED_URL': 'https://example.invalid/pub',
            }
            with self.assertRaisesRegex(RuntimeError, 'build failed'):
                with china_mirror_environment(environment, True) as result:
                    self.assertEqual(result['PUB_HOSTED_URL'], environment['PUB_HOSTED_URL'])
                    self.assertEqual(result['FLUTTER_STORAGE_BASE_URL'], 'https://storage.flutter-io.cn')
                    self.assertNotIn('ZHENGUOJIAN_MIRROR_SESSION', environment)
                    scripts = list(init_directory.glob('zhenguojian-*.gradle'))
                    self.assertEqual(len(scripts), 1)
                    generated = scripts[0].read_text(encoding='utf-8')
                    self.assertIn(result['ZHENGUOJIAN_MIRROR_SESSION'], generated)
                    self.assertNotIn('__MIRROR_SESSION__', generated)
                    throwaway_environment = result.copy()
                    with china_mirror_environment(environment, True) as other:
                        self.assertNotEqual(other['ZHENGUOJIAN_MIRROR_SESSION'],
                                            throwaway_environment['ZHENGUOJIAN_MIRROR_SESSION'])
                    self.assertTrue(scripts[0].is_file())
                    raise RuntimeError('build failed')
            self.assertEqual(list(init_directory.iterdir()), [existing])
            self.assertEqual(existing.read_text(encoding='utf-8'), 'user configuration')

    def test_pub_mirror_keeps_versions_and_hashes_and_restores_lock_on_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            lock = root / 'pubspec.lock'
            original = (
                'packages:\n  public:\n    description:\n      url: "https://pub.dev"\n'
                '      sha256: abc123\n    version: "1.2.3"\n'
                '  custom:\n    description:\n      url: "https://example.invalid"\n'
                '      sha256: def456\n    version: "4.5.6"\n'
            )
            lock.write_bytes(original.encode('utf-8'))
            with self.assertRaisesRegex(RuntimeError, 'build failed'):
                with mirrored_pub_lockfile(root, {'PUB_HOSTED_URL': 'https://pub.flutter-io.cn'}):
                    self.assertEqual(lock.read_text(encoding='utf-8'),
                                     original.replace('https://pub.dev', 'https://pub.flutter-io.cn'))
                    raise RuntimeError('build failed')
            self.assertEqual(lock.read_text(encoding='utf-8'), original)
            self.assertEqual(list((root / 'build').iterdir()), [])

    def test_pub_mirror_preserves_concurrent_user_edit_and_original_backup(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            lock = root / 'pubspec.lock'
            original = b'packages:\n  test:\n    description:\n      url: "https://pub.dev"\n'
            lock.write_bytes(original)
            with self.assertRaisesRegex(RuntimeError, '未覆盖'):
                with mirrored_pub_lockfile(root, {'PUB_HOSTED_URL': 'https://pub.flutter-io.cn'}):
                    lock.write_text('user change', encoding='utf-8')
            self.assertEqual(lock.read_text(encoding='utf-8'), 'user change')
            backups = list((root / 'build').glob('pubspec-before-mirror-*.lock'))
            self.assertEqual(len(backups), 1)
            self.assertEqual(backups[0].read_bytes(), original)


if __name__ == '__main__':
    unittest.main()
