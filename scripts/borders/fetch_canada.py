#!/usr/bin/env python3
"""Downloads Statistics Canada's 2021 census subdivisions as one GeoJSON file, in lat/lon.

Uses StatCan's public cartographic boundary map service. Large responses make it return HTTP 500, so a
failing batch is split in half until it succeeds.
usage: fetch_canada.py <output.geojson>
"""

import json
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

LAYER_URL = "https://geo.statcan.gc.ca/geo_wa/rest/services/2021/Cartographic_boundary_files/MapServer/9/query"
BATCH_SIZE = 50
GENERALIZE_DEGREES = 0.0005  # about 50 m
HEADERS = {"User-Agent": "WeatherHDS-borders"}


def query(**params):
    # POST, since a long id list can outgrow a URL.
    body = urllib.parse.urlencode(params).encode()
    request = urllib.request.Request(LAYER_URL, data=body, headers=HEADERS)
    with urllib.request.urlopen(request, timeout=300) as response:
        return json.load(response)


def fetch_batch(ids):
    """The features for these ids, splitting the batch if the service can't return it whole."""
    try:
        page = query(objectIds=",".join(map(str, ids)), outFields="CSDUID", returnGeometry="true", outSR=4326, f="geojson")
        return page["features"]
    except (urllib.error.URLError, TimeoutError, json.JSONDecodeError) as error:
        if len(ids) == 1:
            # A few giant northern subdivisions are too big to return at full detail. Their borders with
            # neighbours then won't match exactly, so the builder drops those lines; they're remote enough
            # that no radar reaches them.
            print(f"object {ids[0]} is too big, taking it generalized", flush=True)
            page = query(objectIds=ids[0], outFields="CSDUID", returnGeometry="true", outSR=4326,
                         maxAllowableOffset=GENERALIZE_DEGREES, f="geojson")
            return page["features"]
        print(f"splitting {len(ids)} after: {error}", flush=True)
        time.sleep(3)
        middle = len(ids) // 2
        return fetch_batch(ids[:middle]) + fetch_batch(ids[middle:])


def main():
    ids = query(where="1=1", returnIdsOnly="true", f="json")["objectIds"]
    features = []
    for start in range(0, len(ids), BATCH_SIZE):
        features += fetch_batch(ids[start:start + BATCH_SIZE])
        print(f"{len(features)}/{len(ids)}", flush=True)
    if len(features) != len(ids):
        sys.exit(f"expected {len(ids)} features, got {len(features)}")
    with open(sys.argv[1], "w", encoding="utf-8") as output:
        json.dump({"type": "FeatureCollection", "features": features}, output)


if __name__ == "__main__":
    main()
