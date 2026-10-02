import json
import re
import secrets
import tempfile
from contextlib import contextmanager
from pathlib import Path


@contextmanager
def mirrored_pub_lockfile(root, environment):
    mirror = environment.get('PUB_HOSTED_URL', 'https://pub.dev').rstrip('/')
    if mirror in {'https://pub.dev', 'https://pub.dartlang.org'}:
        yield
        return
    lockfile = Path(root) / 'pubspec.lock'
    original = lockfile.read_bytes()
    rewritten = re.sub(r'(?m)^([ \t]+url:[ \t]*)"?https://pub\.(?:dev|dartlang\.org)/?"?([ \t]*)$',
                       lambda match: match[1] + json.dumps(mirror) + match[2],
                       original.decode('utf-8')).encode('utf-8')
    if rewritten == original:
        yield
        return
    backup_directory = Path(root) / 'build'
    backup_directory.mkdir(parents=True, exist_ok=True)
    descriptor = tempfile.NamedTemporaryFile(prefix='pubspec-before-mirror-', suffix='.lock',
                                             dir=backup_directory, delete=False)
    backup = Path(descriptor.name)
    with descriptor:
        descriptor.write(original)
    try:
        lockfile.write_bytes(rewritten)
        yield
    finally:
        if lockfile.read_bytes() != rewritten:
            raise RuntimeError('构建期间锁文件发生变化，未覆盖；原文件保存在 ' + str(backup))
        lockfile.write_bytes(original)
        backup.unlink()


@contextmanager
def china_mirror_environment(environment, enabled, *, gradle=True):
    result = environment.copy()
    if not enabled:
        yield result
        return
    result.setdefault('PUB_HOSTED_URL', 'https://pub.flutter-io.cn')
    result.setdefault('FLUTTER_STORAGE_BASE_URL', 'https://storage.flutter-io.cn')
    if not gradle:
        yield result
        return
    gradle_directory = Path(result.get('GRADLE_USER_HOME') or Path.home() / '.gradle')
    init_directory = gradle_directory.expanduser().resolve() / 'init.d'
    created_directory = not init_directory.exists()
    init_directory.mkdir(parents=True, exist_ok=True)
    session = secrets.token_hex(16)
    result['ZHENGUOJIAN_MIRROR_SESSION'] = session
    template = Path(__file__).with_name('gradle_mirrors.init.gradle').read_text(encoding='utf-8')
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8',
                                         prefix='zhenguojian-', suffix='.gradle',
                                         dir=init_directory, delete=False) as stream:
            temporary = Path(stream.name)
            stream.write(template.replace('__MIRROR_SESSION__', session))
        yield result
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
        if created_directory:
            try:
                init_directory.rmdir()
            except OSError:
                pass
