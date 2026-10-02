import tempfile
import unittest
from pathlib import Path

from sync_source import REQUIRED_FILES, synchronize


class SourceSyncTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='duanju-source-sync-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source = self.root / '源码'
        self.destination = self.root / 'guoapp'
        for name in REQUIRED_FILES:
            self.write(self.source / name, name)

    def write(self, path, content):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding='utf-8')

    def test_only_source_is_copied_and_git_is_preserved(self):
        for name in [
            'lib/pages/首页.dart', 'assets/icon.svg', 'test/fixtures/synthetic.json',
            'native/core/provider.go', 'macos/Runner/AppDelegate.swift', 'scripts/build_android.py',
        ]:
            self.write(self.source / name, 'source')
        excluded = [
            'build/app.apk', 'dist/app.zip', '.dart_tool/package_config.json',
            'android/local.properties', 'android/key.properties', 'android/signing.jks',
            'android/gradlew', 'android/gradle/wrapper/gradle-wrapper.jar',
            'android/app/src/main/jniLibs/arm64-v8a/libduanju_core.so',
            'native/vendor/example/dependency.go', 'native/build/core.dll',
            'scripts/__pycache__/script.pyc', 'scripts/.env.local', '.DS_Store',
            '.git/config', 'sdk/flutter/lib/framework.dart', 'docs/old-note.md',
        ]
        for name in excluded:
            self.write(self.source / name, 'must not be copied')
        git_files = {'HEAD': 'ref: refs/heads/main\n', 'config': 'keep remote', 'index': 'keep staging'}
        for name, content in git_files.items():
            self.write(self.destination / '.git' / name, content)
        self.write(self.destination / 'build/old.apk', 'old artifact')
        self.write(self.destination / 'lib/removed.dart', 'old source')
        result = synchronize(self.source, self.destination)
        self.assertTrue(result.changed)
        self.assertEqual((self.destination / 'lib/pages/首页.dart').read_text(), 'source')
        for name in excluded:
            if not name.startswith('.git/'):
                self.assertFalse((self.destination / name).exists(), name)
        self.assertFalse((self.destination / 'lib/removed.dart').exists())
        for name, content in git_files.items():
            self.assertEqual((self.destination / '.git' / name).read_text(), content)
        self.assertFalse(synchronize(self.source, self.destination).changed)

    def test_check_does_not_write_and_updates_include_deletions(self):
        added = self.source / 'lib/temporary.dart'
        self.write(added, 'first')
        synchronize(self.source, self.destination)
        added.unlink()
        self.write(self.source / 'README.md', 'updated')
        before = (self.destination / 'README.md').read_bytes()
        report = synchronize(self.source, self.destination, check=True)
        self.assertIn('README.md', report.updated)
        self.assertIn('lib/temporary.dart', report.removed)
        self.assertEqual((self.destination / 'README.md').read_bytes(), before)
        self.assertTrue((self.destination / 'lib/temporary.dart').exists())
        synchronize(self.source, self.destination)
        self.assertFalse(synchronize(self.source, self.destination, check=True).changed)

    def test_symlinks_never_export_or_overwrite_external_files(self):
        outside = self.root / 'external'
        self.write(outside / 'main.dart', 'external file')
        self.destination.mkdir()
        try:
            (self.source / 'lib/leak.dart').symlink_to(outside / 'main.dart')
            (self.destination / 'lib').symlink_to(outside, target_is_directory=True)
        except (NotImplementedError, OSError):
            self.skipTest('symlinks are not available')
        synchronize(self.source, self.destination)
        self.assertEqual((outside / 'main.dart').read_text(), 'external file')
        self.assertFalse((self.destination / 'lib').is_symlink())
        self.assertFalse((self.destination / 'lib/leak.dart').exists())
        self.assertEqual((self.destination / 'lib/main.dart').read_text(), 'lib/main.dart')

    def test_unsafe_targets_and_incomplete_source_are_rejected(self):
        for destination in [self.source, self.source / 'export', self.source.parent]:
            with self.assertRaises(ValueError):
                synchronize(self.source, destination)
        self.write(self.destination / 'preserved.txt', 'keep')
        (self.source / 'pubspec.lock').unlink()
        with self.assertRaises(ValueError):
            synchronize(self.source, self.destination)
        self.assertEqual((self.destination / 'preserved.txt').read_text(), 'keep')


if __name__ == '__main__':
    unittest.main()
