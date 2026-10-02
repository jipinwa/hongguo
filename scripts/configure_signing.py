import argparse
import base64
import os
from pathlib import Path

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('--clean', action='store_true')
options = parser.parse_args()
runner_temp = os.environ.get('RUNNER_TEMP')
if not runner_temp:
    raise SystemExit('此脚本用于 GitHub Actions；本机构建请配置 android/key.properties。')
key_file = Path(runner_temp) / 'duanju-release.jks'
properties_file = root / 'android' / 'key.properties'
if options.clean:
    if key_file.exists():
        key_file.unlink()
        properties_file.unlink(missing_ok=True)
    raise SystemExit(0)

names = ['ANDROID_KEYSTORE_BASE64', 'ANDROID_KEYSTORE_PASSWORD',
         'ANDROID_KEY_ALIAS', 'ANDROID_KEY_PASSWORD']
values = [os.environ.get(name, '') for name in names]
if not any(values):
    print('未配置签名 Secrets：生成预览 APK；正式发布请配置固定签名。')
    raise SystemExit(0)
if not all(values):
    raise SystemExit('Android 签名需要同时配置全部四个 Secrets。')
try:
    data = base64.b64decode(''.join(values[0].split()), validate=True)
except ValueError:
    raise SystemExit('ANDROID_KEYSTORE_BASE64 不是有效的 Base64。')
if not data:
    raise SystemExit('Android 签名文件为空。')
key_file.write_bytes(data)
key_file.chmod(0o600)


def escape(value):
    result = ''
    for character in value:
        if character in '\\:=#! ':
            result += '\\' + character
        elif character == '\n':
            result += '\\n'
        elif character == '\r':
            result += '\\r'
        elif character == '\t':
            result += '\\t'
        elif ord(character) > 126:
            encoded = character.encode('utf-16-be')
            result += ''.join('\\u' + encoded[i:i + 2].hex() for i in range(0, len(encoded), 2))
        else:
            result += character
    return result


properties = {'storeFile': key_file.as_posix(), 'storePassword': values[1],
              'keyAlias': values[2], 'keyPassword': values[3]}
properties_file.write_text(''.join(name + '=' + escape(value) + '\n'
                                for name, value in properties.items()), encoding='ascii')
properties_file.chmod(0o600)
print('已配置固定 Android 发布签名。')
