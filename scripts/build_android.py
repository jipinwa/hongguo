import argparse
import os
import platform
import shutil
import subprocess
import sys
from pathlib import Path

from build_mirrors import china_mirror_environment, mirrored_pub_lockfile
from app_build import BuildVariant, add_variant_argument

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('--abi', action='append', choices=['arm64-v8a', 'armeabi-v7a', 'x86_64'])
parser.add_argument('--cn-mirrors', action='store_true', help='使用 Flutter 中国镜像和阿里云 Maven 镜像')
add_variant_argument(parser)
options = parser.parse_args()
variant = BuildVariant(options.all_sources)
environment = os.environ.copy()
if platform.system() == 'Darwin':
    environment['LANG'] = 'en_US.UTF-8'
    environment['LC_ALL'] = 'en_US.UTF-8'
environment.setdefault('GOPROXY', 'https://goproxy.cn,direct')
environment.setdefault('GOSUMDB', 'off')
flutter = shutil.which('flutter')
if not flutter:
    raise SystemExit('请先将 Flutter SDK 的 bin 目录加入 PATH。')
abi_args = [item for abi in options.abi or [] for item in ['--abi', abi]]
with china_mirror_environment(environment, options.cn_mirrors) as env, mirrored_pub_lockfile(root, env):
    if options.cn_mirrors:
        print('本次构建启用国内依赖镜像，保留官方 Maven 仓库备用。', flush=True)
    subprocess.run([sys.executable, str(root / 'scripts' / 'build_native.py'), '--platform', 'android', *abi_args, *variant.arguments],
                   cwd=root, env=env, check=True)
    subprocess.run([flutter, 'pub', 'get', '--enforce-lockfile'], cwd=root, env=env, check=True)
    build_args = [flutter, 'build', 'apk', '--release', '--split-per-abi', '--no-pub', *variant.flutter_arguments]
    if options.abi:
        targets = {'arm64-v8a': 'android-arm64', 'armeabi-v7a': 'android-arm', 'x86_64': 'android-x64'}
        build_args += ['--target-platform', ','.join(targets[abi] for abi in options.abi)]
    subprocess.run(build_args, cwd=root, env=env, check=True)
    subprocess.run([sys.executable, str(root / 'scripts' / 'package_release.py'), '--platform', 'android', *abi_args, *variant.arguments],
                   cwd=root, env=env, check=True)
