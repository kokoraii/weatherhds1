#!/usr/bin/env python3
"""Builds public/maps/provinces.json: rough outlines of Canada's provinces and territories, used to pick
each province's highway shield (OpenStreetMap tags every provincial route the same, "ca-provincial").

Source: geoBoundaries gbOpen CAN ADM1 (Statistics Canada Open Licence), simplified file:
  https://www.geoboundaries.org/api/current/gbOpen/CAN/ADM1/  ->  simplifiedGeometryGeoJSON
usage: build-provinces.py <geoBoundaries-CAN-ADM1_simplified.geojson>
"""

import json
import os
import sys

TOLERANCE = 0.05  # degrees, about 5 km: plenty to tell which province a highway is in
MIN_AREA = 0.3  # square degrees; smaller islands carry no shielded highways worth placing
SKIP = {"CA-NU"}  # no highways
# geoBoundaries spells Quebec "CA-QB"; ISO 3166-2 is "CA-QC".
CODES = {"CA-QB": "CA-QC"}
OUTPUT = os.path.join(os.path.dirname(__file__), "..", "public", "maps", "provinces.json")


def area(ring):
    return abs(sum(x0 * y1 - x1 * y0 for (x0, y0), (x1, y1) in zip(ring, ring[1:]))) / 2


def simplify(points, tolerance):
    """Douglas-Peucker, iterative so long coastlines don't hit the recursion limit."""
    keep = [False] * len(points)
    keep[0] = keep[-1] = True
    stack = [(0, len(points) - 1)]
    while stack:
        first, last = stack.pop()
        (ax, ay), (bx, by) = points[first], points[last]
        dx, dy = bx - ax, by - ay
        length = (dx * dx + dy * dy) ** 0.5 or 1e-12
        far, far_distance = None, tolerance
        for i in range(first + 1, last):
            px, py = points[i]
            distance = abs(dy * (px - ax) - dx * (py - ay)) / length
            if distance > far_distance:
                far, far_distance = i, distance
        if far is not None:
            keep[far] = True
            stack += [(first, far), (far, last)]
    return [p for p, k in zip(points, keep) if k]


def simplify_ring(ring, tolerance):
    """A closed ring starts and ends on the same point, which leaves Douglas-Peucker no baseline, so it's
    simplified as two halves split at the point farthest from the start."""
    x0, y0 = ring[0]
    far = max(range(len(ring)), key=lambda i: (ring[i][0] - x0) ** 2 + (ring[i][1] - y0) ** 2)
    return simplify(ring[: far + 1], tolerance) + simplify(ring[far:], tolerance)[1:]


def main():
    source = json.load(open(sys.argv[1], encoding="utf-8"))
    provinces = {}
    for feature in source["features"]:
        geometry = feature["geometry"]
        polygons = geometry["coordinates"] if geometry["type"] == "MultiPolygon" else [geometry["coordinates"]]
        kept = []
        for polygon in polygons:
            outer = polygon[0]  # holes are lakes; a highway is never inside one
            if area(outer) < MIN_AREA:
                continue
            ring = [[round(x, 2), round(y, 2)] for x, y in simplify_ring(outer, TOLERANCE)]
            if len(ring) >= 4:
                kept.append([ring])
        code = feature["properties"]["shapeISO"]
        if code not in SKIP:
            provinces[CODES.get(code, code)] = kept
    with open(OUTPUT, "w", encoding="utf-8") as output:
        json.dump(provinces, output, separators=(",", ":"))
    print(f"wrote {OUTPUT}: {os.path.getsize(OUTPUT) / 1000:.0f} kB, {len(provinces)} provinces")


if __name__ == "__main__":
    main()
