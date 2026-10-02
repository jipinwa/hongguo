import argparse
import json
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

root = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description='从统一图形生成真果鉴 / 红果鉴平台资源；需要 Pillow。')
parser.add_argument('--output', type=Path, default=root)
parser.add_argument('--font', type=Path, default=Path('C:/Windows/Fonts/msyh.ttc'))
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

for density, size in [('mdpi', 48), ('hdpi', 72), ('xhdpi', 96), ('xxhdpi', 144), ('xxxhdpi', 192)]:
    save(icon.resize((size, size), Image.Resampling.LANCZOS),
         f'android/app/src/main/res/mipmap-{density}/ic_launcher.png')
macos = root / 'macos/Runner/Assets.xcassets/AppIcon.appiconset/Contents.json'
if macos.exists():
    for entry in json.loads(macos.read_text())['images']:
        if 'filename' in entry:
            size = round(float(entry['size'].split('x')[0]) * float(entry['scale'].rstrip('x')))
            save(icon.resize((size, size), Image.Resampling.LANCZOS),
                 'macos/Runner/Assets.xcassets/AppIcon.appiconset/' + entry['filename'])
font = ImageFont.truetype(str(options.font), 76)
for name, resource in [('红果鉴', 'tv_banner'), ('真果鉴', 'tv_banner_all_sources')]:
    banner = Image.new('RGB', (640, 360), '#101114')
    banner.paste(icon.resize((180, 180), Image.Resampling.LANCZOS), (44, 90))
    draw = ImageDraw.Draw(banner)
    draw.text((255, 128), name, font=font, fill='white')
    save(banner, f'android/app/src/main/res/drawable-xhdpi/{resource}.png')
