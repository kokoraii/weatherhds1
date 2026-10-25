#!/usr/bin/env python3
"""Convert the animated (SMIL) SVG weather icons into animated, transparent AVIF images.

    pip install pillow playwright          (Pillow 11.3+ for AVIF support)
    python scripts/svg2avif.py             (public/graphics/smil -> public/graphics/avif)

SMIL animation is only implemented by browsers, so every frame is rendered by a headless
Chromium-family browser: the SVG's timeline is paused and seeked to each frame time, then
screenshotted with a transparent background. Edge or Chrome are used if installed; otherwise run
`playwright install chromium` once.

The loop length is worked out from the SVG's own timing (dur + the gap in `begin="x.end+1s"`
chains), taking the least common multiple of every element's period so the AVIF loops with no
visible seam. Icons whose periods never line up within --max-seconds get the best-fitting length
and a warning. Each loop is verified by comparing frames one full loop apart.

Output keeps each SVG's name with an .avif extension, so public/graphics/smil/clear-day.svg becomes
public/graphics/avif/clear-day.avif. Setting `animatedIconFormat` in public/config.js to "avif"
makes the page load these files instead of the SVGs.
"""
from __future__ import annotations

import argparse
import io
import math
import os
import re
import sys
import time
import xml.etree.ElementTree as ET
from concurrent.futures import ProcessPoolExecutor
from dataclasses import dataclass, field
from pathlib import Path

try:
    from PIL import Image, ImageChops, features
    from playwright.sync_api import sync_playwright
except ImportError as exc:
    sys.exit(f"Missing dependency '{exc.name}'. Install with: pip install pillow playwright")

REPO_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_SRC = REPO_ROOT / "public" / "graphics" / "smil"
DEFAULT_OUT = REPO_ROOT / "public" / "graphics" / "avif"

ANIMATION_TAGS = {"animate", "animateTransform", "animateMotion", "animateColor", "set"}
UNIT_MS = {"ms": 1.0, "s": 1000.0, "min": 60000.0, "h": 3600000.0}
CLOCK = re.compile(r"^\s*(-?\d*\.?\d+)\s*(ms|s|min|h)?\s*$")
# Where along a loop to compare against one loop later. Irregular on purpose: evenly spaced probes
# can all land on the same phase of a blinking element and hide a seam.
PROBES = (0.0, 0.137, 0.331, 0.529, 0.743, 0.911)
# Chained animations (begin="x1.end+1s") can take a loop or two to settle into their steady cycle.
SETTLE_LOOPS = 3
SYNCBASE = re.compile(r"^\s*[\w\-:]+\.(begin|end)\s*(?:([+-])\s*(\S+))?\s*$")


@dataclass
class Timing:
    periods_ms: list[int]
    start_ms: float
    plays_once: bool = False
    notes: list[str] = field(default_factory=list)

    @property
    def animated(self) -> bool:
        return bool(self.periods_ms)


@dataclass
class Result:
    src: Path
    out: Path | None = None
    frames: int = 0
    loop_ms: int = 0
    size_bytes: int = 0
    loop_error: float = 0.0
    skipped: bool = False
    error: str = ""
    warnings: list[str] = field(default_factory=list)


def parse_clock(text: str | None) -> float | None:
    """SMIL clock value ("6s", ".67s", "250ms", "1min") to milliseconds."""
    match = CLOCK.match(text or "")
    if not match:
        return None
    return float(match.group(1)) * UNIT_MS[match.group(2) or "s"]


def analyse(svg: Path) -> Timing:
    """Work out each animation's repeat period, and when the steady state begins."""
    periods: set[int] = set()
    notes: list[str] = []
    start = 0.0
    plays_once = False

    for el in ET.parse(svg).getroot().iter():
        if el.tag.rsplit("}", 1)[-1] not in ANIMATION_TAGS:
            continue
        dur = parse_clock(el.get("dur"))
        if not dur or dur <= 0:
            continue

        gap = None
        for item in (s.strip() for s in (el.get("begin") or "0").split(";")):
            if not item:
                continue
            chained = SYNCBASE.match(item)
            if chained:
                # "x1.end+.67s": restarts once the element ends, so period = dur + gap.
                if chained.group(1) == "end":
                    offset = parse_clock(chained.group(3)) or 0.0
                    gap = -offset if chained.group(2) == "-" else offset
                continue
            offset = parse_clock(item)
            if offset is None:
                notes.append(f"ignored begin='{item}'")
            elif offset > 0:
                start = max(start, offset)

        if gap is not None:
            period = dur + gap
        else:
            period = dur
            if el.get("repeatCount") != "indefinite" and el.get("repeatDur") != "indefinite":
                plays_once = True
                notes.append(f"plays once in the SVG ({dur / 1000:g}s), looped here")
        if period > 0:
            periods.add(round(period))

    return Timing(sorted(periods), start, plays_once, sorted(set(notes)))


def choose_loop(periods: list[int], cap_ms: int, step_ms: int = 10) -> tuple[int, float]:
    """Loop length (ms) and its worst phase error (ms). Exact whenever the LCM fits under the cap."""
    exact = math.lcm(*periods)
    cap_ms = max(cap_ms, max(periods))
    if exact <= cap_ms:
        return exact, 0.0

    best = (exact, float("inf"))
    for length in range(max(step_ms, max(periods) // step_ms * step_ms), cap_ms + 1, step_ms):
        error = max(min(length % p, p - length % p) for p in periods)
        if error < best[1]:
            best = (length, float(error))
    return best


def frame_durations(count: int, loop_ms: int) -> list[int]:
    """Per-frame durations in whole ms that add up to exactly loop_ms."""
    edges = [round(i * loop_ms / count) for i in range(count + 1)]
    return [max(1, edges[i + 1] - edges[i]) for i in range(count)]


def canvas_size(svg: Path, size: int) -> tuple[int, int]:
    root = ET.parse(svg).getroot()
    try:
        _, _, w, h = (float(v) for v in re.split(r"[\s,]+", root.get("viewBox", "").strip()))
    except ValueError:
        return size, size
    scale = size / max(w, h)
    return max(1, round(w * scale)), max(1, round(h * scale))


def differing_percent(a: Image.Image, b: Image.Image, threshold: int = 32) -> float:
    """Share of pixels (percent) that differ noticeably between two frames."""
    r, g, bl, al = ImageChops.difference(a, b).split()
    worst = ImageChops.lighter(ImageChops.lighter(r, g), ImageChops.lighter(bl, al))
    changed = sum(worst.histogram()[threshold + 1:])
    return 100.0 * changed / (a.width * a.height)


class Renderer:
    """Seeks an SVG's SMIL timeline and screenshots it with a transparent background."""

    def __init__(self, pw, browser_name: str):
        order = ["msedge", "chrome", "chromium"] if browser_name == "auto" else [browser_name]
        failures = []
        self.browser = None
        for name in order:
            try:
                options = {"headless": True, "args": ["--force-color-profile=srgb", "--hide-scrollbars"]}
                if name != "chromium":
                    options["channel"] = name
                self.browser = pw.chromium.launch(**options)
                break
            except Exception as exc:  # noqa: BLE001 - playwright raises a generic Error
                failures.append(f"{name}: {str(exc).strip().splitlines()[0]}")
        if self.browser is None:
            raise RuntimeError(
                "No usable browser (" + "; ".join(failures) + "). "
                "Install Edge/Chrome or run: playwright install chromium"
            )
        self.page = self.browser.new_page(device_scale_factor=1)

    def open(self, svg: Path, width: int, height: int) -> None:
        self.page.set_viewport_size({"width": width, "height": height})
        self.page.goto(svg.as_uri())
        self.page.evaluate("() => document.documentElement.pauseAnimations()")

    def frame(self, t_ms: float) -> Image.Image:
        self.page.evaluate("t => document.documentElement.setCurrentTime(t)", t_ms / 1000.0)
        png = self.page.screenshot(type="png", omit_background=True)
        return Image.open(io.BytesIO(png)).convert("RGBA")

    def close(self) -> None:
        self.browser.close()


def convert_one(renderer: Renderer, src: Path, out: Path, opts: dict) -> Result:
    result = Result(src=src, out=out)
    timing = analyse(src)
    width, height = canvas_size(src, opts["size"])
    renderer.open(src, width, height)

    if not timing.animated:
        frames, durations = [renderer.frame(0)], [0]
    else:
        loop_ms, phase_error = choose_loop(timing.periods_ms, round(opts["max_seconds"] * 1000))
        count = max(2, round(loop_ms / 1000 * opts["fps"]))
        durations = frame_durations(count, loop_ms)
        result.loop_ms = loop_ms
        start_ms = timing.start_ms

        if phase_error:
            if phase_error > 1000 / opts["fps"]:
                result.warnings.append(
                    f"periods {timing.periods_ms} never line up within {opts['max_seconds']:g}s; "
                    f"best loop is {loop_ms / 1000:g}s with up to {phase_error:.0f}ms of drift at the seam"
                )
        else:
            # Frames one loop apart must match: frame 0 against frame `count` is the seam the viewer sees,
            # the other probes confirm the SVG really repeats with this period. Play-once icons only
            # have the seam to check.
            fractions = (0.0,) if timing.plays_once else PROBES
            best_error = None
            for settle in range(SETTLE_LOOPS):
                candidate = timing.start_ms + settle * loop_ms
                error = max(
                    differing_percent(renderer.frame(candidate + f * loop_ms),
                                      renderer.frame(candidate + (1 + f) * loop_ms))
                    for f in fractions
                )
                if best_error is None or error < best_error:
                    start_ms, best_error = candidate, error
                if error <= opts["loop_tolerance"]:
                    break
            result.loop_error = best_error
            if best_error > opts["loop_tolerance"]:
                result.warnings.append(
                    f"loop does not close cleanly ({best_error:.2f}% of pixels differ one loop apart)"
                )

        frames = [renderer.frame(start_ms + i * loop_ms / count) for i in range(count)]
        result.warnings.extend(timing.notes)

    result.frames = len(frames)
    save_avif(frames, durations, out, opts)
    result.size_bytes = out.stat().st_size
    return result


def save_avif(frames: list[Image.Image], durations: list[int], out: Path, opts: dict) -> None:
    settings = {
        "format": "AVIF",
        "quality": opts["quality"],
        "speed": opts["speed"],
        "subsampling": opts["subsampling"],
        "max_threads": opts["threads"],
    }
    if len(frames) > 1:
        settings.update(save_all=True, append_images=frames[1:], duration=durations)

    tmp = out.with_name(out.name + ".tmp")
    try:
        frames[0].save(tmp, **settings)
        os.replace(tmp, out)
    finally:
        tmp.unlink(missing_ok=True)


def run_chunk(jobs: list[tuple[Path, Path]], opts: dict) -> list[Result]:
    results = []
    with sync_playwright() as pw:
        renderer = Renderer(pw, opts["browser"])
        try:
            for src, out in jobs:
                started = time.time()
                try:
                    result = convert_one(renderer, src, out, opts)
                except Exception as exc:  # noqa: BLE001 - keep going, report at the end
                    result = Result(src=src, error=f"{type(exc).__name__}: {exc}")
                results.append(result)
                print(describe(result, time.time() - started), flush=True)
        finally:
            renderer.close()
    return results


def describe(r: Result, seconds: float) -> str:
    if r.error:
        return f"[FAIL] {r.src.name}: {r.error}"
    kind = f"{r.loop_ms / 1000:g}s loop, {r.frames}f" if r.frames > 1 else "still"
    line = f"[ ok ] {r.src.name} -> {r.out.name}  {kind}, {r.size_bytes / 1024:.1f} KiB ({seconds:.1f}s)"
    return line + "".join(f"\n         warning: {w}" for w in r.warnings)


def collect(paths: list[Path]) -> list[Path]:
    found: list[Path] = []
    for path in (p.resolve() for p in paths):
        if path.is_dir():
            found.extend(sorted(path.rglob("*.svg")))
        elif path.suffix.lower() == ".svg" and path.is_file():
            found.append(path)
        else:
            sys.exit(f"Not an SVG file or directory: {path}")
    return found


def parse_args() -> argparse.Namespace:
    cpus = os.cpu_count() or 2
    p = argparse.ArgumentParser(description="Convert animated SMIL SVGs to animated transparent AVIF.")
    p.add_argument("paths", nargs="*", type=Path, default=[DEFAULT_SRC],
                   help=f"SVG files or folders (default: {DEFAULT_SRC.relative_to(REPO_ROOT)})")
    p.add_argument("--out-dir", type=Path, default=DEFAULT_OUT,
                   help=f"where the .avif files go (default: {DEFAULT_OUT.relative_to(REPO_ROOT)})")
    p.add_argument("--size", type=int, default=256, help="longest side in pixels (default 256)")
    p.add_argument("--fps", type=int, default=30, help="frames per second; 30 paces evenly on 60Hz screens (default 30)")
    p.add_argument("--quality", type=int, default=70, help="AVIF quality 0-100 (default 70)")
    p.add_argument("--speed", type=int, default=6, help="encoder speed 0 (slow/small) to 10 (fast) (default 6)")
    p.add_argument("--subsampling", default="4:4:4", choices=["4:4:4", "4:2:2", "4:2:0"],
                   help="chroma subsampling; 4:4:4 keeps flat icon colours clean at edges (default 4:4:4)")
    p.add_argument("--max-seconds", type=float, default=24.0, help="longest loop to allow (default 24)")
    p.add_argument("--loop-tolerance", type=float, default=0.05,
                   help="warn when more than this %% of pixels differ across the loop seam (default 0.05)")
    p.add_argument("--browser", default="auto", choices=["auto", "msedge", "chrome", "chromium"])
    p.add_argument("--jobs", type=int, default=max(1, min(4, cpus // 2)), help="parallel browsers")
    p.add_argument("--force", action="store_true", help="re-convert files that are already up to date")
    return p.parse_args()


def main() -> int:
    args = parse_args()
    if not features.check("avif"):
        sys.exit("This Pillow build has no AVIF support. Upgrade it: pip install -U pillow (11.3+ needed)")

    sources = collect(args.paths)
    if not sources:
        sys.exit("No SVG files found.")

    out_dir = args.out_dir.resolve()
    jobs = []
    skipped = 0
    for src in sources:
        out = out_dir / src.with_suffix(".avif").name
        if not args.force and out.exists() and out.stat().st_mtime >= src.stat().st_mtime:
            skipped += 1
            continue
        jobs.append((src, out))

    try:
        shown = out_dir.relative_to(REPO_ROOT)
    except ValueError:
        shown = out_dir
    print(f"{len(sources)} SVGs found, {skipped} up to date, {len(jobs)} to convert "
          f"-> {shown} ({args.size}px, {args.fps}fps, q{args.quality}, {args.jobs} browser(s))")
    if not jobs:
        return 0
    out_dir.mkdir(parents=True, exist_ok=True)

    workers = max(1, min(args.jobs, len(jobs)))
    opts = {
        "size": args.size, "fps": args.fps, "quality": args.quality, "speed": args.speed,
        "subsampling": args.subsampling, "max_seconds": args.max_seconds,
        "loop_tolerance": args.loop_tolerance, "browser": args.browser,
        "threads": max(1, (os.cpu_count() or 2) // workers),
    }

    started = time.time()
    # Deal files out round-robin so every browser gets a similar mix of cheap and expensive icons.
    chunks = [jobs[i::workers] for i in range(workers)]
    if workers == 1:
        results = run_chunk(chunks[0], opts)
    else:
        with ProcessPoolExecutor(max_workers=workers) as pool:
            results = [r for batch in pool.map(run_chunk, chunks, [opts] * workers) for r in batch]

    failed = [r for r in results if r.error]
    warned = [r for r in results if r.warnings and not r.error]
    total = sum(r.size_bytes for r in results)
    print(f"\nDone in {time.time() - started:.0f}s: {len(results) - len(failed)} converted "
          f"({total / 1024 / 1024:.1f} MiB), {len(warned)} with warnings, {len(failed)} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
