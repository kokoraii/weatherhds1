"""Build MapLibre glyph files (SDF .pbf) for the radar map labels from a web font.

MapLibre draws label text from pre-rendered glyph ranges, and OpenFreeMap only serves Noto Sans,
so the Poppins labels in public/maps/radar-dark.json come from files this script writes to
public/maps/fonts/<font name>/<start>-<end>.pbf.

    python scripts/build_map_glyphs.py

It loads Poppins from Google Fonts in headless Edge/Chrome, rasterises each character with
TinySDF (the same SDF settings MapLibre uses for local glyphs) and encodes the glyphs.proto
format. Only Latin ranges are built; place names in other scripts fall back to nothing.
Needs: pip install playwright
"""
import argparse
import base64
from pathlib import Path

from playwright.sync_api import sync_playwright

REPO_ROOT = Path(__file__).resolve().parent.parent
OUT_DIR = REPO_ROOT / "public" / "maps" / "fonts"

# fontstack name -> CSS font-weight
FONTS = {"Poppins Regular": 400, "Poppins Bold": 700}
# Basic Latin + Latin-1, Latin Extended A/B, Latin Extended Additional, general punctuation.
RANGES = [0, 256, 512, 7680, 8192]

PAGE = """<!doctype html><html><head>
<link href="https://fonts.googleapis.com/css2?family=Poppins:wght@400;700&display=block" rel="stylesheet">
</head><body></body></html>"""

# Renders one 256-codepoint range and returns it encoded as a glyphs.proto message (base64).
BUILD_RANGE = r"""
async ([name, weight, start]) => {
  await document.fonts.load(`${weight} 24px Poppins`, String.fromCharCode(...Array.from({ length: 256 }, (_, i) => start + i)));
  const { default: TinySDF } = await import('https://unpkg.com/@mapbox/tiny-sdf@2.0.6/index.js');
  const sdf = new TinySDF({ fontSize: 24, buffer: 3, radius: 8, cutoff: 0.25, fontFamily: 'Poppins', fontWeight: String(weight) });

  const bytes = [];
  const varint = (out, v) => { v = v >>> 0; while (v > 127) { out.push((v & 127) | 128); v >>>= 7; } out.push(v); };
  const zigzag = v => (v << 1) ^ (v >> 31);
  const field = (out, num, type) => varint(out, (num << 3) | type);
  const lenDelim = (out, num, payload) => { field(out, num, 2); varint(out, payload.length); for (const b of payload) out.push(b); };

  const stack = [];
  lenDelim(stack, 1, [...new TextEncoder().encode(name)]);
  lenDelim(stack, 2, [...new TextEncoder().encode(`${start}-${start + 255}`)]);
  for (let id = start; id < start + 256; id++) {
    const ch = String.fromCharCode(id);
    if (id < 32 || (id >= 127 && id < 160)) continue; // control characters
    const g = sdf.draw(ch);
    const glyph = [];
    field(glyph, 1, 0); varint(glyph, id);
    if (g.glyphWidth > 0 && g.glyphHeight > 0) lenDelim(glyph, 2, Array.from(g.data));
    field(glyph, 3, 0); varint(glyph, g.glyphWidth);
    field(glyph, 4, 0); varint(glyph, g.glyphHeight);
    // Same metric mapping MapLibre applies to TinySDF glyphs (glyph_manager): top is offset by the 24px baseline.
    field(glyph, 5, 0); varint(glyph, zigzag(Math.round(g.glyphLeft)));
    field(glyph, 6, 0); varint(glyph, zigzag(Math.round(g.glyphTop - 27)));
    field(glyph, 7, 0); varint(glyph, Math.round(g.glyphAdvance));
    lenDelim(stack, 3, glyph);
  }
  lenDelim(bytes, 1, stack);
  let bin = ''; for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin);
}
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--browser", default="msedge", help="Playwright channel: msedge, chrome, or chromium")
    args = parser.parse_args()

    with sync_playwright() as p:
        browser = p.chromium.launch(channel=None if args.browser == "chromium" else args.browser)
        page = browser.new_page()
        page.set_content(PAGE, wait_until="networkidle")
        for name, weight in FONTS.items():
            folder = OUT_DIR / name
            folder.mkdir(parents=True, exist_ok=True)
            for start in RANGES:
                data = base64.b64decode(page.evaluate(BUILD_RANGE, [name, weight, start]))
                (folder / f"{start}-{start + 255}.pbf").write_bytes(data)
                print(f"{name} {start}-{start + 255}: {len(data) // 1024} KB")
        browser.close()


if __name__ == "__main__":
    main()
