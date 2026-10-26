"""Re-encode the animated weather backgrounds (public/images/avif) to a lighter size and frame rate.

The browser decodes animated AVIF on the CPU, frame by frame, so pixels x fps is what it costs.
The backgrounds fill panels about 530 layout px tall in the 2560x1440 canvas, so 540 px tall at
30 fps looks the same while decoding a fraction of the 1664x936 / 60 fps originals.

    python scripts/shrink_backgrounds.py                 (540 px tall, 30 fps, in place)
    python scripts/shrink_backgrounds.py --height 720 --crf 30

Bitrate is capped at --max-kbps and at the source's own bitrate, so a file never gets heavier.
Files already at or below the target height and frame rate are skipped, so re-running is safe.
Needs ffmpeg/ffprobe on PATH, built with libsvtav1.
"""
import argparse
import json
import os
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
from fractions import Fraction
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_DIR = REPO_ROOT / "public" / "images" / "avif"


def probe(path):
    """Returns (stream index, width, height, fps, duration s) of the animation track, or None for a still image."""
    out = subprocess.run(
        ["ffprobe", "-v", "error", "-show_entries", "stream=index,width,height,r_frame_rate,nb_frames,duration", "-of", "json", str(path)],
        check=True, capture_output=True, text=True,
    ).stdout
    streams = [s for s in json.loads(out)["streams"] if int(s.get("nb_frames", "1") or 1) > 1]
    if not streams:
        return None
    s = streams[0]
    return s["index"], s["width"], s["height"], float(Fraction(s["r_frame_rate"])), float(s["duration"])


def shrink(path, height, fps, crf, preset, max_kbps):
    info = probe(path)
    if info is None:
        return f"skip  {path.name} (still image)"
    index, width, src_height, src_fps, duration = info
    if src_height <= height and src_fps <= fps + 0.01:
        return f"skip  {path.name} ({width}x{src_height} @ {src_fps:g} fps)"

    tmp = path.with_suffix(".tmp.avif")
    before = path.stat().st_size
    # Grainy footage (wind, snow) balloons at a fixed CRF, so cap the bitrate at the source's own.
    cap_kbps = max(100, min(max_kbps, int(before * 8 / 1000 / duration)))
    filters = f"fps={min(fps, src_fps):g},scale=-2:{min(height, src_height)}:flags=lanczos,format=yuv420p"
    subprocess.run(
        ["ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-i", str(path), "-map", f"0:{index}",
         "-vf", filters, "-c:v", "libsvtav1", "-preset", str(preset), "-crf", str(crf), "-g", "60",
         "-svtav1-params", f"mbr={cap_kbps}:enable-stat-report=0", "-f", "avif", str(tmp)],
        check=True, capture_output=True,
    )
    os.replace(tmp, path)
    return f"done  {path.name}: {width}x{src_height} @ {src_fps:g} fps -> {height} px @ {fps:g} fps, {before // 1024} KB -> {path.stat().st_size // 1024} KB"


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--dir", type=Path, default=DEFAULT_DIR)
    parser.add_argument("--height", type=int, default=540)
    parser.add_argument("--fps", type=float, default=30)
    parser.add_argument("--crf", type=int, default=34, help="SVT-AV1 quality, lower is better and bigger (default 34)")
    parser.add_argument("--preset", type=int, default=6, help="SVT-AV1 speed, higher is faster (default 6)")
    parser.add_argument("--max-kbps", type=int, default=1200, help="bitrate cap; never above the source's own (default 1200)")
    parser.add_argument("--jobs", type=int, default=2, help="files encoded at once; SVT-AV1 is already multi-threaded")
    args = parser.parse_args()

    files = sorted(args.dir.glob("*.avif"))
    if not files:
        sys.exit(f"no .avif files in {args.dir}")
    with ThreadPoolExecutor(args.jobs) as pool:
        for line in pool.map(lambda p: shrink(p, args.height, args.fps, args.crf, args.preset, args.max_kbps), files):
            print(line, flush=True)


if __name__ == "__main__":
    main()
