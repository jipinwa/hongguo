import argparse
import fnmatch
import hashlib
import os
import shutil
import stat
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path


SOURCE_DIRECTORIES = {
    '.github', 'lib', 'native', 'android', 'macos', 'packages',
    'assets', 'scripts', 'test', 'test_driver', 'integration_test',
}
SOURCE_FILES = {
    '.gitattributes', '.gitignore', '.metadata', '.editorconfig',
    'AGENTS.md', 'README.md', 'pubspec.yaml', 'pubspec.lock',
    'analysis_options.yaml', 'l10n.yaml', 'flutter_launcher_icons.yaml',
}
EXCLUDED_DIRECTORIES = {
    '.git', '.dart_tool', '.pub-cache', '.gradle', '.cxx', '.kotlin',
    '.idea', '.vscode', '__pycache__', '.pytest_cache', '.mypy_cache',
    '.ruff_cache', '.cache', '.venv', 'venv', 'build', 'dist', 'coverage',
    'node_modules', 'vendor', 'Pods', 'Carthage', 'DerivedData', '.swiftpm', 'xcuserdata',
    '.symlinks', 'ephemeral', 'jniLibs', 'CMakeFiles', 'sdk', 'android-sdk',
    'flutter-sdk', 'ndk', 'toolchains',
}
EXCLUDED_NAMES = {
    '.DS_Store', 'Thumbs.db', 'local.properties', 'key.properties',
    '.packages', 'GeneratedPluginRegistrant.java', 'Generated.xcconfig',
    'flutter_export_environment.sh', 'GeneratedPluginRegistrant.h', 'GeneratedPluginRegistrant.m',
    'GeneratedPluginRegistrant.swift',
}
EXCLUDED_PATTERNS = (
    '.flutter-plugins*', '*.iml', '*.log', '*.pyc', '*.pyo', '*.class',
    '*.jar', '*.aar', '*.dll', '*.so', '*.dylib', '*.a', '*.o', '*.obj',
    '*.apk', '*.aab', '*.ipa', '*.exe', '*.zip', '*.7z', '*.tar', '*.gz',
    '*.tgz', '*.jks', '*.keystore', '*.p12', '*.pfx', '*.pem', '*.key',
    '*.suo', '*.user', '*.userosscache', '*.sln.docstates', '*.xcframework',
    '*.framework', '*.xcuserstate', '._*',
)
EXCLUDED_PATHS = {
    'android/gradlew', 'android/gradlew.bat',
}
REQUIRED_FILES = {
    'AGENTS.md', 'README.md', 'pubspec.yaml', 'pubspec.lock', 'lib/main.dart',
    'native/go.mod', 'native/go.sum', 'native/bridge/main.go',
    'android/app/build.gradle.kts', 'android/gradle/wrapper/gradle-wrapper.properties',
    'macos/Runner.xcodeproj/project.pbxproj', 'macos/Runner/Configs/AppInfo.xcconfig',
    '.github/workflows/build.yml', 'scripts/build_native.py',
}


@dataclass(frozen=True)
class SyncResult:
    files: int
    total_bytes: int
    updated: tuple[str, ...]
    removed: tuple[str, ...]

    @property
    def changed(self):
        return bool(self.updated or self.removed)


def excluded(relative):
    if any(part in EXCLUDED_DIRECTORIES for part in relative.parts):
        return True
    name = relative.name
    if name in EXCLUDED_NAMES or relative.as_posix() in EXCLUDED_PATHS:
        return True
    if name.startswith('.env') and name not in {'.env.example', '.env.sample', '.env.template'}:
        return True
    if any(fnmatch.fnmatchcase(part.lower(), pattern) for part in relative.parts for pattern in EXCLUDED_PATTERNS):
        return True
    if relative.parts[:2] in {('windows', 'x64'), ('windows', 'x86')}:
        return True
    return False


def source_files(source):
    selected = {}
    for directory, folders, files in os.walk(source, followlinks=False):
        base = Path(directory)
        folders[:] = sorted(
            name for name in folders
            if not (base / name).is_symlink()
            and not excluded((base / name).relative_to(source))
            and (base != source or name in SOURCE_DIRECTORIES)
        )
        for name in sorted(files):
            path = base / name
            relative = path.relative_to(source)
            if path.is_symlink() or not path.is_file() or excluded(relative):
                continue
            if base == source and name not in SOURCE_FILES and not any(
                fnmatch.fnmatchcase(name, pattern)
                for pattern in ('LICENSE', 'LICENSE.*', 'COPYING', 'COPYING.*', 'NOTICE', 'NOTICE.*')
            ):
                continue
            selected[relative] = path
    missing = REQUIRED_FILES - {path.as_posix() for path in selected}
    if missing:
        raise ValueError('源码缺少必要文件，未执行同步：' + ', '.join(sorted(missing)))
    return selected


def digest(path):
    checksum = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            checksum.update(block)
    return checksum.digest()


def same_file(source, destination):
    first, second = source.stat(), destination.stat()
    return (
        first.st_size == second.st_size
        and stat.S_IMODE(first.st_mode) == stat.S_IMODE(second.st_mode)
        and digest(source) == digest(destination)
    )


def inspect_destination(destination, selected):
    directories = {parent for path in selected for parent in path.parents if parent != Path('.')}
    existing = set()
    removed = []

    def visit(directory):
        for entry in sorted(directory.iterdir()):
            relative = entry.relative_to(destination)
            if relative == Path('.git'):
                continue
            if relative in directories and entry.is_dir() and not entry.is_symlink():
                visit(entry)
            elif relative in selected and entry.is_file() and not entry.is_symlink():
                existing.add(relative)
            else:
                removed.append(relative)

    if destination.exists():
        visit(destination)
    updated = [
        relative for relative, source in sorted(selected.items())
        if relative not in existing or not same_file(source, destination / relative)
    ]
    return updated, removed


def copy_source(source, destination):
    destination.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix='.source-sync-', dir=destination.parent)
    os.close(descriptor)
    temporary = Path(temporary_name)
    try:
        shutil.copy2(source, temporary)
        os.replace(temporary, destination)
    finally:
        temporary.unlink(missing_ok=True)


def synchronize(source, destination, check=False):
    if destination.is_symlink():
        raise ValueError('同步目标不能是符号链接')
    source, destination = source.resolve(), destination.resolve()
    if source == destination or source in destination.parents or destination in source.parents:
        raise ValueError('源码目录与同步目标必须互不包含，不能向自身同步')
    if destination.exists() and not destination.is_dir():
        raise ValueError('同步目标必须是目录')
    selected = source_files(source)
    updated, removed = inspect_destination(destination, selected)
    if not check:
        destination.mkdir(parents=True, exist_ok=True)
        for relative in removed:
            path = destination / relative
            if path.is_dir() and not path.is_symlink():
                shutil.rmtree(path)
            else:
                path.unlink()
        for relative in updated:
            copy_source(selected[relative], destination / relative)
        pending, unexpected = inspect_destination(destination, selected)
        if pending or unexpected:
            raise ValueError('同步后校验未通过，请重新执行同步')
    return SyncResult(
        files=len(selected), total_bytes=sum(path.stat().st_size for path in selected.values()),
        updated=tuple(path.as_posix() for path in updated),
        removed=tuple(path.as_posix() for path in removed),
    )


def main():
    source = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(
        description='将可发布的纯源码同步到 ../guoapp，不执行任何 Git 操作。'
    )
    parser.add_argument('--destination', type=Path, default=source.parent / 'guoapp', help='覆盖默认同步目标')
    parser.add_argument('--check', action='store_true', help='只检查是否一致，不修改文件')
    options = parser.parse_args()
    destination = options.destination.expanduser()
    if destination.resolve() == source:
        print('当前已在发布源码目录，跳过向自身同步；需要另一份镜像时指定 --destination。')
        return 0
    try:
        result = synchronize(source, destination, check=options.check)
    except (OSError, ValueError) as error:
        print(str(error), file=sys.stderr)
        return 2
    if options.check:
        print('源码需要同步。' if result.changed else f'源码一致：{result.files} 个文件。')
    else:
        print(f'已同步 {result.files} 个源码文件（{result.total_bytes / 1024:.1f} KB），'
              f'更新 {len(result.updated)} 个，清理 {len(result.removed)} 项。')
    print('目标：' + str(destination.resolve()))
    if options.check and result.changed:
        for name in result.updated[:10]:
            print('待更新：' + name)
        for name in result.removed[:10]:
            print('待清理：' + name)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
