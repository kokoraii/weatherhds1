//! Builds public/maps/borders.pmtiles: county-level borders the map's own tiles only carry when zoomed in
//! much further than the radars go. See scripts/build-borders.sh for where the source files come from.
//!
//! usage: build-borders <output.pmtiles> <world.geojson> <canada.geojson> <us.shp>

mod input;
mod lines;
mod pmtiles;
mod tiles;

use lines::WorldPoint;
use pmtiles::Coverage;
use std::collections::HashSet;
use std::fs::File;
use std::io::{self, BufWriter};

/// Lower zooms would be a solid mesh of lines, and past this MapLibre just overzooms the last level.
const MIN_ZOOM: u8 = 4;
const MAX_ZOOM: u8 = 8;
/// Countries the worldwide file skips, because a national source covers them better.
const NATIONAL_SOURCES: [&str; 2] = ["CAN", "USA"];

fn report(name: &str, rings: usize, total: usize, shared: usize, lines: usize) {
    let share = shared as f64 / total.max(1) as f64 * 100.0;
    println!("{name}: {rings} rings, {total} edges, {share:.1}% shared, {lines} lines");
}

fn main() -> io::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let [output, world, canada, us] = args.as_slice() else {
        eprintln!("usage: build-borders <output.pmtiles> <world.geojson> <canada.geojson> <us.shp>");
        std::process::exit(2);
    };

    let skip: HashSet<String> = NATIONAL_SOURCES.iter().map(|group| (*group).to_owned()).collect();
    let mut all_lines: Vec<Vec<WorldPoint>> = Vec::new();

    // The worldwide file's polygons don't share vertices with their neighbours; the national files' do.
    let rings = input::read_geojson(world, &skip)?;
    let (found, total, kept) = lines::nearby_lines(&rings);
    report("world", rings.len(), total, kept, found.len());
    all_lines.extend(found);
    drop(rings);

    let canada_rings: Vec<_> = input::read_geojson(canada, &HashSet::new())?.into_iter().map(|(_, ring)| ring).collect();
    for (name, rings) in [("canada", canada_rings), ("us", input::read_shapefile(us)?)] {
        let (found, total, shared) = lines::shared_lines(&rings);
        report(name, rings.len(), total, shared, found.len());
        all_lines.extend(found);
    }

    let mut archive_tiles = Vec::new();
    for z in MIN_ZOOM..=MAX_ZOOM {
        let tiles = tiles::build_zoom(&all_lines, z);
        let raw: usize = tiles.values().map(Vec::len).sum();
        println!("z{z}: {} tiles, {:.1} MB raw", tiles.len(), raw as f64 / 1e6);
        for ((x, y), bytes) in tiles {
            archive_tiles.push((z, x, y, pmtiles::gzip(&bytes)?));
        }
    }

    let metadata = format!(
        r#"{{"name":"WeatherHDS borders","format":"pbf","vector_layers":[{{"id":"{}","fields":{{}},"minzoom":{MIN_ZOOM},"maxzoom":{MAX_ZOOM}}}]}}"#,
        tiles::LAYER_NAME
    );
    let coverage = Coverage { min_zoom: MIN_ZOOM, max_zoom: MAX_ZOOM };
    let mut file = BufWriter::new(File::create(output)?);
    pmtiles::write_archive(&mut file, archive_tiles, &coverage, &metadata)?;
    drop(file);
    println!("wrote {output}: {:.1} MB", std::fs::metadata(output)?.len() as f64 / 1e6);
    Ok(())
}
