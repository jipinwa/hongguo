import tempfile
import unittest
import zipfile
from pathlib import Path

from finish_task import finish_task
from sync_source import REQUIRED_FILES, synchronize


class TaskSnapshotTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='duanju-snapshot-test-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source = self.root / '源码'
        self.destination = self.root / 'guoapp'
        for name in REQUIRED_FILES:
            self.write(self.source / name, name)
        self.write(self.source / 'pubspec.yaml', 'name: synthetic_app\nversion: 0.1.3+4\n')
        self.destination.mkdir()

    def write(self, path, content):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding='utf-8')

    def test_clean_source_snapshot_needs_no_git_and_excludes_build_dependencies(self):
        self.write(self.source / 'lib/电视.dart', '电视界面')
        self.write(self.source / 'build/app.apk', 'compiled binary')
        self.write(self.source / 'android/key.properties', 'private settings')
        result = finish_task(self.source, self.destination, '完成电视界面')
        self.assertEqual(result.files, len(REQUIRED_FILES) + 1)
        self.assertEqual(result.total_bytes, sum(
            path.stat().st_size
            for path in self.destination.rglob('*')
            if path.is_file() and path.relative_to(self.destination).as_posix() != 'build/app.apk'
        ))
        self.assertFalse((self.destination / 'build').exists())
        self.assertFalse((self.destination / 'android/key.properties').exists())
        self.assertFalse(synchronize(self.source, self.destination, check=True).changed)
        self.assertTrue(result.archive.is_file())
        self.assertRegex(result.archive.name, r'^真果·鉴-\d{12}(?:-\d+)?\.zip$')
        with zipfile.ZipFile(result.archive) as archive:
            names = set(archive.namelist())
        self.assertIn('guoapp/lib/电视.dart', names)
        self.assertNotIn('guoapp/build/app.apk', names)

    def test_existing_git_metadata_is_not_modified(self):
        self.write(self.destination / '.git/config', 'keep local metadata')
        self.write(self.destination / '.git/HEAD', 'keep local head')
        result = finish_task(self.source, self.destination, '保留镜像元数据')
        self.assertTrue(result.archive.is_file())
        self.assertEqual(
            (self.destination / '.git/config').read_text(encoding='utf-8'),
            'keep local metadata',
        )
        self.assertEqual(
            (self.destination / '.git/HEAD').read_text(encoding='utf-8'),
            'keep local head',
        )

    def test_source_changes_update_mirror_and_archive(self):
        self.write(self.source / 'lib/old.dart', 'old behavior')
        first = finish_task(self.source, self.destination, '第一版')
        self.write(self.source / 'lib/main.dart', 'next behavior')
        self.write(self.source / 'pubspec.yaml', 'name: synthetic_app\nversion: 0.1.4+5\n')
        (self.source / 'lib/old.dart').unlink()
        second = finish_task(self.source, self.destination, '第二版')
        self.assertTrue(first.archive.is_file())
        self.assertTrue(second.archive.is_file())
        self.assertNotEqual(first.archive, second.archive)
        self.assertFalse((self.destination / 'lib/old.dart').exists())
        self.assertEqual(
            (self.destination / 'lib/main.dart').read_text(encoding='utf-8'),
            'next behavior',
        )

    def test_working_in_published_directory_only_creates_archive(self):
        synchronize(self.source, self.destination)
        self.write(self.destination / 'lib/main.dart', 'published source')
        result = finish_task(self.destination, self.destination, '直接在镜像目录收尾')
        self.assertTrue(result.archive.is_file())
        self.assertEqual(
            (self.destination / 'lib/main.dart').read_text(encoding='utf-8'),
            'published source',
        )


if __name__ == '__main__':
    unittest.main()
