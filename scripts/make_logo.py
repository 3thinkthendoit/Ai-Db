"""Generate AI-DB brand mark (SVG + PNG + Windows ICO).

Usage (from repo root, needs Pillow):
  python scripts/make_logo.py

Writes:
  frontend/src/assets/logo.svg   — in-app sidebar (also imported by App.tsx)
  frontend/src/assets/logo.png   — raster spare / previews
  build/appicon.png              — Wails app icon source
  build/windows/icon.ico         — Windows exe / title-bar icon (gitignored dir;
                                   regenerated here; `wails build` can also derive from appicon.png)
"""
from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
ASSETS = ROOT / "frontend" / "src" / "assets"
BUILD = ROOT / "build"

BG = (10, 12, 10, 255)
A_COLOR = (0, 224, 127, 255)   # mint — aligns with accent
I_COLOR = (214, 226, 214, 255)  # soft off-white

SVG = """\
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" fill="none">
  <rect width="32" height="32" rx="8" fill="#0a0c0a"/>
  <path fill="#00e07f" fill-rule="evenodd" d="M7.2 21 12.1 11h2.8L19.8 21h-2.35l-.85-2.35h-4.1L11.65 21H7.2zm4.55-4.15h3.3L14 13.1l-2.25 3.75z"/>
  <path fill="#d6e2d6" d="M21.2 11h4.2v1.7h-1.25v6.6H25.4V21h-4.2v-1.7h1.25v-6.6H21.2V11z"/>
</svg>
"""


def rounded_rect_mask(size: int, radius: int) -> Image.Image:
    mask = Image.new("L", (size, size), 0)
    draw = ImageDraw.Draw(mask)
    draw.rounded_rectangle((0, 0, size - 1, size - 1), radius=radius, fill=255)
    return mask


def load_font(px: int) -> ImageFont.ImageFont:
    candidates = [
        r"C:\Windows\Fonts\segoeuib.ttf",
        r"C:\Windows\Fonts\arialbd.ttf",
        r"C:\Windows\Fonts\calibrib.ttf",
        "/System/Library/Fonts/SFNS.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
    ]
    for path in candidates:
        try:
            return ImageFont.truetype(path, px)
        except OSError:
            continue
    return ImageFont.load_default()


def render_mark(size: int) -> Image.Image:
    """Crisp AI squircle for app / window icons."""
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    base = Image.new("RGBA", (size, size), BG)
    radius = max(2, round(size * 0.25))
    img.paste(base, (0, 0), rounded_rect_mask(size, radius))

    draw = ImageDraw.Draw(img)
    font = load_font(max(8, int(size * 0.52)))
    bbox_a = draw.textbbox((0, 0), "A", font=font)
    bbox_i = draw.textbbox((0, 0), "I", font=font)
    aw = bbox_a[2] - bbox_a[0]
    iw = bbox_i[2] - bbox_i[0]
    ah = bbox_a[3] - bbox_a[1]
    gap = max(0, int(size * 0.04))
    total_w = aw + gap + iw
    x0 = (size - total_w) / 2 - bbox_a[0]
    y0 = (size - ah) / 2 - bbox_a[1] - size * 0.02
    draw.text((x0, y0), "A", font=font, fill=A_COLOR)
    draw.text((x0 + aw + gap, y0), "I", font=font, fill=I_COLOR)
    return img


def main() -> None:
    ASSETS.mkdir(parents=True, exist_ok=True)
    BUILD.mkdir(parents=True, exist_ok=True)

    svg_path = ASSETS / "logo.svg"
    svg_path.write_text(SVG, encoding="utf-8")
    print("wrote", svg_path)

    logo = render_mark(256)
    logo.save(ASSETS / "logo.png", optimize=True)
    print("wrote", ASSETS / "logo.png")

    appicon = render_mark(512)
    appicon.save(BUILD / "appicon.png", optimize=True)
    print("wrote", BUILD / "appicon.png")

    ico_sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    ico_imgs = [render_mark(s[0]) for s in ico_sizes]
    ico_path = BUILD / "windows" / "icon.ico"
    ico_path.parent.mkdir(parents=True, exist_ok=True)
    ico_imgs[-1].save(ico_path, format="ICO", sizes=ico_sizes, append_images=ico_imgs[:-1])
    print("wrote", ico_path)


if __name__ == "__main__":
    main()
