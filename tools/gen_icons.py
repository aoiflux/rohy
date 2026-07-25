#!/usr/bin/env python3
"""Generate rohy's application icons from the single source mark.

Run from the repository root:

    python tools/gen_icons.py

Outputs:

    build/appicon.png        1024x1024 — what Wails derives platform icons from
    build/windows/icon.ico   16/32/48/64/128/256 — Windows taskbar, Alt-Tab, Explorer
    build/icon-preview.png   a side-by-side sheet for eyeballing small sizes

Why this exists rather than a one-off export
--------------------------------------------
The icons are DERIVED, never hand-drawn. `frontend/src/assets/logo.svg` is the one place
the mark is defined; re-running this script is how a change to it reaches the taskbar. A
hand-exported PNG would drift from the SVG silently, and nobody would notice until the two
marks visibly disagreed.

Why it parses the SVG instead of using a rasteriser
--------------------------------------------------
No SVG rasteriser is guaranteed present on a contributor's machine — and on Windows the
obvious-looking `convert` is the FAT-to-NTFS filesystem utility, not ImageMagick, which is
a genuinely dangerous thing to invoke by mistake. Pillow is the only image dependency, and
the mark uses a deliberately small set of primitives (straight paths, lines, circles), so
parsing it exactly is a few dozen lines. If the mark ever needs curves or gradients, this
script must grow with it rather than silently mis-render them — hence the hard failure on
any element it does not understand.

Anti-aliasing is by supersampling: draw at 8x and downscale with a high-quality filter,
because Pillow's drawing primitives have no anti-aliasing of their own.
"""

from __future__ import annotations

import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

try:
    from PIL import Image, ImageDraw
except ImportError:
    sys.exit("Pillow is required: pip install pillow")

ROOT = Path(__file__).resolve().parent.parent
SVG = ROOT / "frontend" / "src" / "assets" / "logo.svg"
APPICON = ROOT / "build" / "appicon.png"
ICO = ROOT / "build" / "windows" / "icon.ico"
PREVIEW = ROOT / "build" / "icon-preview.png"

SVG_NS = "{http://www.w3.org/2000/svg}"
SUPERSAMPLE = 8
APPICON_SIZE = 1024
# The full set Windows asks for. Shipping only 256 and letting the OS downscale is what
# makes a taskbar icon look muddy; each size is rendered from the vector instead.
ICO_SIZES = [16, 32, 48, 64, 128, 256]

# Below this size the mark is drawn in a simplified form. See simplify_for().
SIMPLIFY_BELOW = 40
# How much to enlarge the graph nodes once the edges between them are dropped, by size.
# Tuned by rendering each candidate at 8-14x and looking at the actual pixels, not derived:
# 1.45 is right at 32 px, and visibly wrong at 16 px, where it merges the three nodes into
# the ring and produces a blob. Icon work is judged by eye or not at all.
NODE_SCALE = {16: 1.20, 32: 1.45}
NODE_SCALE_DEFAULT = 1.45


def simplify_for(size: int) -> bool:
    """Whether to draw the reduced mark at this size.

    The full mark is a magnifier ringing a three-node graph. That reads down to about 48 px
    and then stops: at 16 px — the taskbar and Alt-Tab size at standard DPI — the interior
    strokes are thinner than a pixel and the whole thing collapses into a blue smudge.

    So small sizes get a deliberately reduced drawing: the connecting lines are dropped and
    the nodes are enlarged. Dots survive downscaling in a way that hairlines do not, and the
    silhouette that actually identifies the app — ring plus handle — is preserved. This is a
    design decision, not a rendering shortcut, which is why it lives here explicitly rather
    than being left to the resampler.
    """
    return size < SIMPLIFY_BELOW


def node_scale_for(size: int) -> float:
    """Node enlargement for a simplified render at this size."""
    return NODE_SCALE.get(size, NODE_SCALE_DEFAULT)


def parse_color(value: str, opacity: float = 1.0) -> tuple[int, int, int, int] | None:
    """Resolve an SVG paint value to RGBA, or None for 'none'."""
    if not value or value == "none":
        return None
    v = value.strip()
    if not v.startswith("#"):
        raise ValueError(f"unsupported colour {value!r}; the mark should use hex colours")
    v = v[1:]
    if len(v) == 3:
        v = "".join(c * 2 for c in v)
    if len(v) != 6:
        raise ValueError(f"unsupported colour #{v}")
    r, g, b = (int(v[i : i + 2], 16) for i in (0, 2, 4))
    return (r, g, b, round(255 * opacity))


def attr(el, inherited: dict, name: str, default=None):
    """Element attribute, falling back to what an enclosing <g> supplied."""
    return el.get(name, inherited.get(name, default))


def draw_round_line(d: ImageDraw.ImageDraw, p0, p1, width: float, colour) -> None:
    """A stroked segment with round caps.

    Pillow's line() has no cap style, so the caps are drawn as circles at each endpoint.
    Without them the mark's strokes end in visible square corners, which reads as a
    different logo at small sizes.
    """
    d.line([p0, p1], fill=colour, width=max(1, round(width)))
    r = width / 2
    for (x, y) in (p0, p1):
        d.ellipse([x - r, y - r, x + r, y + r], fill=colour)


def render(size: int) -> Image.Image:
    """Rasterise the mark at the given pixel size, simplifying it if it is small."""
    simplified = simplify_for(size)
    node_scale = node_scale_for(size)
    tree = ET.parse(SVG)
    root = tree.getroot()

    vb = root.get("viewBox")
    if not vb:
        raise ValueError("logo.svg has no viewBox; cannot scale it reliably")
    _, _, vw, vh = (float(x) for x in re.split(r"[ ,]+", vb.strip()))
    if vw != vh:
        raise ValueError("a non-square viewBox would distort the icon")

    ss = size * SUPERSAMPLE
    scale = ss / vw
    img = Image.new("RGBA", (ss, ss), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)

    def S(v: float) -> float:
        return float(v) * scale

    def walk(node, inherited: dict) -> None:
        for el in node:
            tag = el.tag.replace(SVG_NS, "")
            if tag in ("title", "desc", "metadata"):
                continue

            # A <g> contributes presentation attributes to everything inside it.
            if tag == "g":
                passed = dict(inherited)
                for k in ("stroke", "fill", "stroke-width", "stroke-linecap", "fill-opacity"):
                    if el.get(k) is not None:
                        passed[k] = el.get(k)
                walk(el, passed)
                continue

            stroke = parse_color(attr(el, inherited, "stroke", "none"))
            fill_op = float(attr(el, inherited, "fill-opacity", 1.0))
            fill = parse_color(attr(el, inherited, "fill", "none"), fill_op)
            sw = float(attr(el, inherited, "stroke-width", 1.0))

            if tag == "circle":
                cx, cy, r = (float(el.get(k, 0)) for k in ("cx", "cy", "r"))
                # Enlarge the small node dots (not the big ring) so three distinct nodes are
                # still visible once the edges between them are gone.
                if simplified and r < 6:
                    r *= node_scale
                box = [S(cx - r), S(cy - r), S(cx + r), S(cy + r)]
                if fill:
                    d.ellipse(box, fill=fill)
                if stroke:
                    d.ellipse(box, outline=stroke, width=max(1, round(S(sw))))

            elif tag == "line":
                # The graph's connecting edges are the first thing to disappear at small
                # sizes; drawn anyway they only muddy the interior.
                if simplified:
                    continue
                p0 = (S(float(el.get("x1"))), S(float(el.get("y1"))))
                p1 = (S(float(el.get("x2"))), S(float(el.get("y2"))))
                if stroke:
                    draw_round_line(d, p0, p1, S(sw), stroke)

            elif tag == "path":
                # Only straight "M x y L x y" segments are used by this mark. Anything else
                # is refused loudly rather than approximated into a subtly wrong icon.
                dattr = (el.get("d") or "").strip()
                m = re.fullmatch(
                    r"M\s*(-?[\d.]+)[ ,]+(-?[\d.]+)\s*L\s*(-?[\d.]+)[ ,]+(-?[\d.]+)",
                    dattr,
                    re.IGNORECASE,
                )
                if not m:
                    raise ValueError(
                        f"path data {dattr!r} is not a straight segment; this generator "
                        f"only understands 'M x y L x y' — extend it before changing the mark"
                    )
                x0, y0, x1, y1 = (float(g) for g in m.groups())
                if stroke:
                    draw_round_line(d, (S(x0), S(y0)), (S(x1), S(y1)), S(sw), stroke)

            else:
                raise ValueError(
                    f"unsupported SVG element <{tag}>; extend gen_icons.py rather than "
                    f"letting the icon quietly lose part of the mark"
                )

    walk(root, {})
    return img.resize((size, size), Image.LANCZOS)


def main() -> None:
    if not SVG.exists():
        sys.exit(f"missing {SVG}")

    APPICON.parent.mkdir(parents=True, exist_ok=True)
    ICO.parent.mkdir(parents=True, exist_ok=True)

    app = render(APPICON_SIZE)
    app.save(APPICON, format="PNG")
    print(f"wrote {APPICON.relative_to(ROOT)}  {APPICON_SIZE}x{APPICON_SIZE}")

    # Each ICO frame is rendered from the vector at its own size rather than downscaled
    # from one large raster, so small sizes keep their strokes.
    frames = [render(s) for s in ICO_SIZES]
    frames[-1].save(ICO, format="ICO", sizes=[(s, s) for s in ICO_SIZES], append_images=frames[:-1])
    print(f"wrote {ICO.relative_to(ROOT)}  {', '.join(str(s) for s in ICO_SIZES)}")

    # A contact sheet, so "is it legible at 16 px?" is answered by looking rather than hoping.
    pad = 8
    sheet_w = sum(s + pad for s in ICO_SIZES) + pad
    sheet = Image.new("RGBA", (sheet_w, 256 + 2 * pad), (255, 255, 255, 255))
    x = pad
    for s, f in zip(ICO_SIZES, frames):
        sheet.paste(f, (x, pad + (256 - s) // 2), f)
        x += s + pad
    sheet.save(PREVIEW, format="PNG")
    print(f"wrote {PREVIEW.relative_to(ROOT)}  (visual check for small sizes)")


if __name__ == "__main__":
    main()
