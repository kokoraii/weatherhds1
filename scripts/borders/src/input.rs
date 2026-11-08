//! Reads polygon rings from the source files: GeoJSON, and ESRI shapefiles in lat/lon.

use crate::lines::Point;
use serde::Deserialize;
use std::collections::HashSet;
use std::fs;
use std::io;

#[derive(Deserialize)]
struct FeatureCollection {
    features: Vec<Feature>,
}

#[derive(Deserialize)]
struct Feature {
    #[serde(default)]
    properties: Option<Properties>,
    geometry: Option<Geometry>,
}

#[derive(Deserialize)]
struct Properties {
    #[serde(rename = "shapeGroup", default)]
    group: Option<String>,
}

#[derive(Deserialize)]
#[serde(tag = "type", content = "coordinates")]
enum Geometry {
    Polygon(Vec<Vec<[f64; 2]>>),
    MultiPolygon(Vec<Vec<Vec<[f64; 2]>>>),
    #[serde(other)]
    Other,
}

/// (polygon id, ring) from a GeoJSON file, skipping features whose `shapeGroup` property is in `skip_groups`.
/// Every ring of one feature, islands included, shares an id.
pub fn read_geojson(path: &str, skip_groups: &HashSet<String>) -> io::Result<Vec<(u32, Vec<Point>)>> {
    let bytes = fs::read(path)?;
    let collection: FeatureCollection = serde_json::from_slice(&bytes)?;
    let mut rings = Vec::new();
    for (id, feature) in collection.features.into_iter().enumerate() {
        let id = id as u32;
        let group = feature.properties.and_then(|properties| properties.group);
        if group.is_some_and(|group| skip_groups.contains(&group)) {
            continue;
        }
        match feature.geometry {
            Some(Geometry::Polygon(polygon)) => rings.extend(polygon.into_iter().map(|ring| (id, ring))),
            Some(Geometry::MultiPolygon(polygons)) => {
                rings.extend(polygons.into_iter().flatten().map(|ring| (id, ring)));
            }
            Some(Geometry::Other) | None => {}
        }
    }
    Ok(rings)
}

/// Rings from a polygon shapefile (the `.shp` part).
pub fn read_shapefile(path: &str) -> io::Result<Vec<Vec<Point>>> {
    let bytes = fs::read(path)?;
    parse_shapefile(&bytes).ok_or_else(|| io::Error::new(io::ErrorKind::InvalidData, format!("{path} is malformed")))
}

const SHP_HEADER_BYTES: usize = 100;
const SHP_POLYGON: i32 = 5;

fn parse_shapefile(bytes: &[u8]) -> Option<Vec<Vec<Point>>> {
    let i32_le = |at: usize| Some(i32::from_le_bytes(bytes.get(at..at + 4)?.try_into().ok()?));
    let f64_le = |at: usize| Some(f64::from_le_bytes(bytes.get(at..at + 8)?.try_into().ok()?));
    let mut rings = Vec::new();
    let mut record = SHP_HEADER_BYTES;
    while record + 8 <= bytes.len() {
        // Record lengths are big-endian, counted in 16-bit words, after an 8 byte record header.
        let words = i32::from_be_bytes(bytes.get(record + 4..record + 8)?.try_into().ok()?) as usize;
        let body = record + 8;
        if i32_le(body)? == SHP_POLYGON {
            let parts = i32_le(body + 36)? as usize;
            let points = i32_le(body + 40)? as usize;
            let (first_part, first_point) = (body + 44, body + 44 + parts * 4);
            for part in 0..parts {
                let start = i32_le(first_part + part * 4)? as usize;
                let end = if part + 1 < parts { i32_le(first_part + (part + 1) * 4)? as usize } else { points };
                let ring = (start..end)
                    .map(|point| Some([f64_le(first_point + point * 16)?, f64_le(first_point + point * 16 + 8)?]))
                    .collect::<Option<Vec<Point>>>()?;
                rings.push(ring);
            }
        }
        record = body + words * 2;
    }
    Some(rings)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A shapefile holding one polygon with the given rings.
    fn shapefile(rings: &[&[Point]]) -> Vec<u8> {
        let mut body = Vec::new();
        body.extend_from_slice(&SHP_POLYGON.to_le_bytes());
        body.extend_from_slice(&[0; 32]); // bounding box
        let points: usize = rings.iter().map(|ring| ring.len()).sum();
        body.extend_from_slice(&(rings.len() as i32).to_le_bytes());
        body.extend_from_slice(&(points as i32).to_le_bytes());
        let mut start = 0;
        for ring in rings {
            body.extend_from_slice(&(start as i32).to_le_bytes());
            start += ring.len();
        }
        for point in rings.iter().flat_map(|ring| ring.iter()) {
            body.extend_from_slice(&point[0].to_le_bytes());
            body.extend_from_slice(&point[1].to_le_bytes());
        }
        let mut file = vec![0; SHP_HEADER_BYTES];
        file.extend_from_slice(&1i32.to_be_bytes());
        file.extend_from_slice(&((body.len() / 2) as i32).to_be_bytes());
        file.extend(body);
        file
    }

    #[test]
    fn reads_every_ring_of_a_polygon() {
        let outer: &[Point] = &[[0.0, 0.0], [1.0, 0.0], [1.0, 1.0], [0.0, 0.0]];
        let hole: &[Point] = &[[0.2, 0.2], [0.3, 0.2], [0.2, 0.2]];
        let rings = parse_shapefile(&shapefile(&[outer, hole])).unwrap();
        assert_eq!(rings, vec![outer.to_vec(), hole.to_vec()]);
    }

    #[test]
    fn a_truncated_shapefile_is_an_error_not_a_panic() {
        let mut file = shapefile(&[&[[0.0, 0.0], [1.0, 0.0], [0.0, 0.0]]]);
        file.truncate(file.len() - 4);
        assert!(parse_shapefile(&file).is_none());
    }

    #[test]
    fn geojson_skips_the_named_groups() {
        let path = std::env::temp_dir().join("build-borders-test.geojson");
        fs::write(
            &path,
            r#"{"type":"FeatureCollection","features":[
                {"properties":{"shapeGroup":"CAN"},"geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[0,0]]]}},
                {"properties":{"shapeGroup":"FRA"},"geometry":{"type":"MultiPolygon","coordinates":[[[[2,2],[3,2],[2,2]]]]}},
                {"properties":null,"geometry":null}
            ]}"#,
        )
        .unwrap();
        let skip = HashSet::from(["CAN".to_owned()]);
        let rings = read_geojson(path.to_str().unwrap(), &skip).unwrap();
        fs::remove_file(path).unwrap();
        assert_eq!(rings, vec![(1, vec![[2.0, 2.0], [3.0, 2.0], [2.0, 2.0]])]);
    }
}
