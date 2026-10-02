import base64
import os
from pathlib import Path
import runpy
import shutil
import sys
import unittest
from unittest import mock

from app_build import BuildVariant


def dart_defines(*values):
    return ','.join(base64.b64encode(value.encode()).decode() for value in values)


class AppBuildTests(unittest.TestCase):
    def test_omitted_or_false_flag_keeps_hongguo_only(self):
        for encoded in ['', dart_defines('ALL_SOURCES=false'), dart_defines('OTHER=true')]:
            variant = BuildVariant.from_dart_defines(encoded)
            self.assertFalse(variant.all_sources)
            self.assertEqual(variant.name, '红果鉴')
            self.assertEqual(variant.slug, 'hongguojian')

    def test_full_edition_decodes_among_other_flutter_defines(self):
        variant = BuildVariant.from_dart_defines(dart_defines(
            'OTHER=中文', 'ALL_SOURCES=true', 'VALUE=a=b'))
        self.assertTrue(variant.all_sources)
        self.assertEqual(variant.name, '真果鉴')
        self.assertEqual(variant.slug, 'zhenguojian')

    def test_android_propagates_one_edition_to_core_flutter_and_package(self):
        root = Path(__file__).resolve().parent
        for enabled in [False, True]:
            with self.subTest(all_sources=enabled):
                script = root / 'build_android.py'
                arguments = [str(script)] + (['--all-sources'] if enabled else [])
                with mock.patch.object(sys, 'argv', arguments), \
                        mock.patch.dict(os.environ, {'PATH': '/tools'}, clear=True), \
                        mock.patch('shutil.which', return_value='/tools/flutter'), \
                        mock.patch('platform.system', return_value='Linux'), \
                        mock.patch('subprocess.run') as run:
                    runpy.run_path(str(script), run_name='__main__')
                calls = [call.args[0] for call in run.call_args_list]
                native = next(call for call in calls if any(str(arg).endswith('build_native.py') for arg in call))
                flutter = next(call for call in calls if 'build' in call)
                package = next(call for call in calls if any(str(arg).endswith('package_release.py') for arg in call))
                self.assertEqual('--all-sources' in native, enabled)
                self.assertEqual('--all-sources' in package, enabled)
                self.assertIn('--dart-define=ALL_SOURCES=' + str(enabled).lower(), flutter)
                self.assertIn('core.buildAllSources=' + str(enabled).lower(), BuildVariant(enabled).linker_flags)


if __name__ == '__main__':
    unittest.main()
