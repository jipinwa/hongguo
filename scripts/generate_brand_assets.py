import argparse
import json
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description='从统一图形生成红果鉴 / 真果鉴平台资源；需要 Pillow。')
parser.add_argument('--output', type=Path, default=root)
parser.add_argument('--font', type=Path, default=Path('/System/Library/Fonts/PingFang.ttc'))
options = parser.parse_args()
output = options.output
icon = Image.new('RGB', (1024, 1024), '#101114')
draw = ImageDraw.Draw(icon)
draw.rounded_rectangle((110, 110, 914, 914), radius=236, fill='#FF765F')
draw.polygon([(418, 303), (418, 721), (734, 512)], fill='white')

def save(image, name):
    destination = output / name
    destination.parent.mkdir(parents=True, exist_ok=True)
    image.save(destination)

contents = json.loads((root / 'ios/Runner/Assets.xcassets/AppIcon.appiconset/Contents.json').read_text())
for entry in contents['images']:
    if 'filename' in entry:
        size = round(float(entry['size'].split('x')[0]) * float(entry['scale'].rstrip('x')))
        save(icon.resize((size, size), Image.Resampling.LANCZOS),
             'ios/Runner/Assets.xcassets/AppIcon.appiconset/' + entry['filename'])
save(icon.resize((256, 256), Image.Resampling.LANCZOS), 'windows/runner/resources/app_icon.ico')
font = ImageFont.truetype(str(options.font), 76)
for name, resource in [('红果鉴', 'tv_banner'), ('真果鉴', 'tv_banner_all_sources')]:
    banner = Image.new('RGB', (640, 360), '#101114')
    banner.paste(icon.resize((180, 180), Image.Resampling.LANCZOS), (44, 90))
    draw = ImageDraw.Draw(banner)
    draw.text((255, 128), name, font=font, fill='white')
    save(banner, f'android/app/src/main/res/drawable-xhdpi/{resource}.png')
