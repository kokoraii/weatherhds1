#!/usr/bin/env bash
# Builds public/maps/borders.pmtiles from the source boundary files. Only needed to refresh the data.
# usage: scripts/build-borders.sh <empty or existing data directory, outside the repo>
# Downloads about 600 MB on the first run. Sources and licences are in public/maps/borders-sources.txt.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
data="$1"
mkdir -p "$data/world" "$data/canada" "$data/us/extracted"

world="$data/world/cgaz_adm2.geojson"
[ -f "$world" ] || curl -fL --retry 3 -o "$world" \
    "https://github.com/wmgeolab/geoBoundaries/raw/main/releaseData/CGAZ/geoBoundariesCGAZ_ADM2.geojson"

canada="$data/canada/csd.geojson"
[ -f "$canada" ] || python "$root/scripts/borders/fetch_canada.py" "$canada"

us="$data/us/extracted/cb_2023_us_county_5m.shp"
if [ ! -f "$us" ]; then
    curl -fL --retry 3 -o "$data/us/county.zip" "https://www2.census.gov/geo/tiger/GENZ2023/shp/cb_2023_us_county_5m.zip"
    python -c "import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])" "$data/us/county.zip" "$data/us/extracted"
fi

cargo run --release --manifest-path "$root/scripts/borders/Cargo.toml" -- \
    "$root/public/maps/borders.pmtiles" "$world" "$canada" "$us"
