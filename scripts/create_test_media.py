import argparse
import subprocess
from pathlib import Path

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument('--output', type=Path, default=root / 'build' / 'device-test' / 'media')
options = parser.parse_args()
output = options.output.resolve()
output.mkdir(parents=True, exist_ok=True)
clear = output / 'clear.mp4'
subprocess.run(['ffmpeg', '-v', 'error', '-y', '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=12',
                '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=44100', '-t', '20',
                '-c:v', 'libx264', '-threads', '1', '-g', '24', '-pix_fmt', 'yuv420p',
                '-c:a', 'aac', '-movflags', '+faststart', str(clear)], check=True)
(output / 'key.bin').write_bytes(b'0123456789abcdef')
(output / 'key-info.txt').write_text('key.bin\n' + str(output / 'key.bin') + '\n')
subprocess.run(['ffmpeg', '-v', 'error', '-y', '-i', str(clear), '-c', 'copy', '-hls_time', '2',
                '-hls_list_size', '0', '-hls_key_info_file', str(output / 'key-info.txt'),
                '-hls_segment_filename', str(output / 'segment-%d.ts'), str(output / 'index.m3u8')], check=True)
subprocess.run(['ffmpeg', '-v', 'error', '-y', '-i', str(clear), '-c', 'copy', '-movflags', '+faststart',
                '-encryption_scheme', 'cenc-aes-ctr', '-encryption_key', '00112233445566778899aabbccddeeff',
                '-encryption_kid', '11223344556677889900aabbccddeeff', str(output / 'encrypted.mp4')], check=True)
print(output)
