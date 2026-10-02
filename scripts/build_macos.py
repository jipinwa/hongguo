import argparse
import hashlib
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

from app_build import BuildVariant, add_variant_argument

root = Path(__file__).resolve().parents[1]


def run(arguments, **kwargs):
    subprocess.run(arguments, cwd=kwargs.pop('cwd', root), check=True, **kwargs)


def build_core(variant=BuildVariant()):
    if platform.system() != 'Darwin':
        raise SystemExit('macOS 构建需要 macOS 和 Xcode Command Line Tools。')
    go = shutil.which('go')
    if not go:
        raise SystemExit('请安装 Go 1.24.1 或更新版本。')
    clang = shutil.which('clang')
    if not clang:
        raise SystemExit('需要安装 Xcode Command Line Tools。')
    environment = os.environ.copy()
    environment.setdefault('GOPROXY', 'https://goproxy.cn,direct')
    environment.setdefault('GOSUMDB', 'off')
    environment['CGO_ENABLED'] = '1'
    environment['GOTOOLCHAIN'] = os.environ.get('GOTOOLCHAIN', 'auto')
    libraries = []
    for architecture, clang_arch in [('arm64', 'arm64'), ('amd64', 'x86_64')]:
        directory = root / 'native' / 'build' / 'macos' / architecture
        directory.mkdir(parents=True, exist_ok=True)
        output = directory / 'libduanju_core.dylib'
        flags = f'-arch {clang_arch}'
        build_env = environment | {
            'GOOS': 'darwin', 'GOARCH': architecture, 'CC': clang,
            'CGO_CFLAGS': flags, 'CGO_LDFLAGS': flags,
        }
        print('Building ' + str(output.relative_to(root)), flush=True)
        run([go, 'build', '-trimpath', '-buildmode=c-shared',
             '-ldflags=' + variant.linker_flags, '-o', str(output), './bridge'],
            cwd=root / 'native', env=build_env)
        libraries.append(output)
    universal = root / 'native' / 'build' / 'macos' / 'libduanju_core.dylib'
    run(['xcrun', 'lipo', '-create', *[str(item) for item in libraries], '-output', str(universal)])
    return universal


def sign_bundle(application):
    frameworks = application / 'Contents' / 'Frameworks'
    for junk in sorted(application.rglob('*')):
        if junk.is_file() and (junk.name.startswith('._') or junk.name == '.DS_Store'):
            junk.unlink()
    for leftover in sorted(frameworks.rglob('_Resources')):
        shutil.rmtree(leftover)
    for framework in sorted(frameworks.glob('*.framework')):
        run(['codesign', '--force', '--deep', '--sign', '-', str(framework)])
    for library in sorted(frameworks.glob('*.dylib')):
        run(['codesign', '--force', '--sign', '-', str(library)])
    run(['codesign', '--force', '--sign', '-', str(application)])
    run(['codesign', '--verify', '--deep', '--strict', str(application)])


def main():
    parser = argparse.ArgumentParser(description='构建红果鉴 / 真果鉴 macOS 核心和应用')
    parser.add_argument('--core-only', action='store_true')
    add_variant_argument(parser)
    options = parser.parse_args()
    variant = BuildVariant(options.all_sources)
    library = build_core(variant)
    if options.core_only:
        return
    flutter = shutil.which('flutter')
    if not flutter:
        raise SystemExit('请安装 Flutter。')
    run([flutter, 'config', '--enable-macos-desktop'])
    run([flutter, 'pub', 'get', '--enforce-lockfile'])
    run([flutter, 'build', 'macos', '--release', '--no-pub', *variant.flutter_arguments])
    application = root / 'build' / 'macos' / 'Build' / 'Products' / 'Release' / 'duanju_app.app'
    if not application.is_dir():
        raise SystemExit('缺少 macOS 应用包：' + str(application))
    frameworks = application / 'Contents' / 'Frameworks'
    frameworks.mkdir(parents=True, exist_ok=True)
    shutil.copy2(library, frameworks / 'libduanju_core.dylib')
    symbols = subprocess.check_output(['xcrun', 'nm', '-gU', str(frameworks / 'libduanju_core.dylib')], text=True)
    for symbol in ['_DuanjuRequest', '_DuanjuFree']:
        if symbol not in symbols:
            raise SystemExit('macOS 核心缺少 FFI 入口：' + symbol)
    sign_bundle(application)
    output = root / 'dist' / 'macos'
    output.mkdir(parents=True, exist_ok=True)
    version = re.search(r'^version:\s*(\S+)', (root / 'pubspec.yaml').read_text(encoding='utf-8'), re.MULTILINE).group(1)
    with tempfile.TemporaryDirectory(prefix='zhenguojian-macos-') as temporary:
        staged = Path(temporary) / f'{variant.slug}.app'
        shutil.copytree(application, staged, symlinks=True)
        destination = output / f'{variant.slug}-{version}-macos.zip'
        run(['ditto', '-c', '-k', '--sequesterRsrc', '--keepParent', str(staged), str(destination)])
    checksums = []
    for artifact in sorted(output.glob(f'*-{version}-macos*')):
        digest = hashlib.sha256()
        with artifact.open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(chunk)
        checksums.append(f'{digest.hexdigest()}  {artifact.name}')
        print(artifact)
    (output / 'SHA256SUMS.txt').write_text('\n'.join(checksums) + '\n', encoding='ascii')


if __name__ == '__main__':
    main()
