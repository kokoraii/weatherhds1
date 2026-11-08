//! Cuts border lines into vector tiles (Mapbox Vector Tile format, encoded by hand: one layer, one feature).

use crate::lines::{WorldPoint, simplify};
use std::collections::HashMap;

pub const LAYER_NAME: &str = "borders";
const EXTENT: i32 = 4096;
/// How far past its edge a tile carries lines, in tile units, so lines don't visibly end at tile seams.
const BUFFER: f64 = 32.0;
/// Simplification tolerance, in MapLibre's CSS pixels at the tile's own zoom.
const TOLERANCE_PX: f64 = 0.4;
/// A tile is 512 CSS pixels at its own zoom.
const TILE_CSS_PX: f64 = 512.0;

type TileKey = (u32, u32);

/// Every non-empty tile at zoom `z`, as encoded MVT bytes.
pub fn build_zoom(lines: &[Vec<WorldPoint>], z: u8) -> HashMap<TileKey, Vec<u8>> {
    let scale = f64::from(1u32 << z);
    let tolerance = TOLERANCE_PX / (TILE_CSS_PX * scale);
    let mut tiles: HashMap<TileKey, Vec<Vec<[i32; 2]>>> = HashMap::new();
    for line in lines {
        cut_line(&simplify(line, tolerance), z, &mut tiles);
    }
    tiles
        .into_iter()
        .filter_map(|(key, lines)| {
            let lines: Vec<Vec<[i32; 2]>> = lines.into_iter().filter(|line| line.len() >= 2).collect();
            (!lines.is_empty()).then(|| (key, encode_tile(&lines)))
        })
        .collect()
}

/// Appends a line's pieces to every tile it crosses, in integer tile coordinates.
fn cut_line(line: &[WorldPoint], z: u8, tiles: &mut HashMap<TileKey, Vec<Vec<[i32; 2]>>>) {
    let n = 1u32 << z;
    let scale = f64::from(n);
    // Where this line's last piece in each tile ended, to continue it rather than start a new one.
    let mut open: HashMap<TileKey, [i32; 2]> = HashMap::new();
    for pair in line.windows(2) {
        let (a, b) = ([pair[0][0] * scale, pair[0][1] * scale], [pair[1][0] * scale, pair[1][1] * scale]);
        let margin = BUFFER / f64::from(EXTENT);
        let tile_range = |p: f64, q: f64| {
            let low = (p.min(q) - margin).floor().max(0.0) as u32;
            let high = ((p.max(q) + margin).floor().max(0.0) as u32).min(n - 1);
            low..=high
        };
        for y in tile_range(a[1], b[1]) {
            for x in tile_range(a[0], b[0]) {
                let Some((start, end)) = clip_to_tile(a, b, x, y) else { continue };
                if start == end {
                    continue;
                }
                let pieces = tiles.entry((x, y)).or_default();
                if open.get(&(x, y)) == Some(&start) {
                    pieces.last_mut().expect("an open piece exists").push(end);
                } else {
                    pieces.push(vec![start, end]);
                }
                open.insert((x, y), end);
            }
        }
    }
}

/// Liang-Barsky clip of a segment (in tile units at this zoom) to one tile and its buffer, converted to
/// integer tile coordinates.
fn clip_to_tile(a: [f64; 2], b: [f64; 2], x: u32, y: u32) -> Option<([i32; 2], [i32; 2])> {
    let extent = f64::from(EXTENT);
    let local = |p: [f64; 2]| [(p[0] - f64::from(x)) * extent, (p[1] - f64::from(y)) * extent];
    let (from, to) = (local(a), local(b));
    let (mut enter, mut leave) = (0.0f64, 1.0f64);
    for axis in 0..2 {
        let delta = to[axis] - from[axis];
        for (bound, sign) in [(-BUFFER, -1.0), (extent + BUFFER, 1.0)] {
            let distance = sign * (bound - from[axis]);
            let rate = sign * delta;
            if rate == 0.0 {
                if distance < 0.0 {
                    return None;
                }
            } else {
                let t = distance / rate;
                if rate < 0.0 { enter = enter.max(t) } else { leave = leave.min(t) }
            }
        }
    }
    if enter > leave {
        return None;
    }
    let at = |t: f64| {
        [
            (from[0] + (to[0] - from[0]) * t).round() as i32,
            (from[1] + (to[1] - from[1]) * t).round() as i32,
        ]
    };
    Some((at(enter), at(leave)))
}

fn put_varint(out: &mut Vec<u8>, mut value: u64) {
    while value >= 0x80 {
        out.push((value as u8) | 0x80);
        value >>= 7;
    }
    out.push(value as u8);
}

fn put_field_bytes(out: &mut Vec<u8>, field: u64, bytes: &[u8]) {
    put_varint(out, field << 3 | 2);
    put_varint(out, bytes.len() as u64);
    out.extend_from_slice(bytes);
}

fn put_field_varint(out: &mut Vec<u8>, field: u64, value: u64) {
    put_varint(out, field << 3);
    put_varint(out, value);
}

fn zigzag(value: i32) -> u64 {
    u64::from(((value << 1) ^ (value >> 31)) as u32)
}

/// One tile holding one MultiLineString feature in one layer, per the MVT 2.1 spec.
fn encode_tile(lines: &[Vec<[i32; 2]>]) -> Vec<u8> {
    const MOVE_TO: u64 = 1;
    const LINE_TO: u64 = 2;
    const LINE_STRING: u64 = 2;
    let command = |id: u64, count: u64| count << 3 | id;

    let mut geometry = Vec::new();
    let mut cursor = [0i32, 0];
    for line in lines {
        // Quantizing can leave repeated points, which the format has no use for.
        let mut points = line.clone();
        points.dedup();
        if points.len() < 2 {
            continue;
        }
        let mut step = |point: [i32; 2], geometry: &mut Vec<u8>| {
            put_varint(geometry, zigzag(point[0] - cursor[0]));
            put_varint(geometry, zigzag(point[1] - cursor[1]));
            cursor = point;
        };
        put_varint(&mut geometry, command(MOVE_TO, 1));
        step(points[0], &mut geometry);
        put_varint(&mut geometry, command(LINE_TO, points.len() as u64 - 1));
        for &point in &points[1..] {
            step(point, &mut geometry);
        }
    }

    let mut feature = Vec::new();
    put_field_varint(&mut feature, 3, LINE_STRING);
    put_field_bytes(&mut feature, 4, &geometry);

    let mut layer = Vec::new();
    put_field_bytes(&mut layer, 1, LAYER_NAME.as_bytes());
    put_field_bytes(&mut layer, 2, &feature);
    put_field_varint(&mut layer, 5, EXTENT as u64);
    put_field_varint(&mut layer, 15, 2);

    let mut tile = Vec::new();
    put_field_bytes(&mut tile, 3, &layer);
    tile
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn zigzag_matches_the_spec() {
        assert_eq!([zigzag(0), zigzag(-1), zigzag(1), zigzag(-2)], [0, 1, 2, 3]);
    }

    #[test]
    fn a_line_inside_one_tile_stays_in_that_tile() {
        let line = vec![[0.125, 0.125], [0.2, 0.2]]; // z2 tile (0,0)
        let tiles = build_zoom(&[line], 2);
        assert_eq!(tiles.keys().copied().collect::<Vec<_>>(), vec![(0, 0)]);
    }

    #[test]
    fn a_line_crossing_a_seam_lands_in_both_tiles_past_the_edge() {
        let mut tiles = HashMap::new();
        cut_line(&[[0.2, 0.1], [0.3, 0.1]], 2, &mut tiles); // crosses x = 0.25, the z2 seam
        let left = &tiles[&(0, 0)][0];
        let right = &tiles[&(1, 0)][0];
        assert!(left.last().unwrap()[0] >= EXTENT, "left tile stops short: {left:?}");
        assert!(right.first().unwrap()[0] <= 0, "right tile starts late: {right:?}");
    }

    #[test]
    fn consecutive_segments_join_into_one_piece() {
        let mut tiles = HashMap::new();
        cut_line(&[[0.05, 0.05], [0.1, 0.05], [0.1, 0.1]], 2, &mut tiles);
        assert_eq!(tiles[&(0, 0)].len(), 1);
        assert_eq!(tiles[&(0, 0)][0].len(), 3);
    }

    #[test]
    fn a_segment_outside_a_tile_is_rejected() {
        assert!(clip_to_tile([3.0, 3.0], [3.5, 3.5], 0, 0).is_none());
    }

    #[test]
    fn encodes_a_known_line_exactly() {
        // MoveTo(1) to (2,2); LineTo(1) by (0,1): commands 9 and 10, zigzagged deltas 4,4 and 0,2.
        let tile = encode_tile(&[vec![[2, 2], [2, 3]]]);
        assert!(tile.windows(6).any(|window| window == [9, 4, 4, 10, 0, 2]), "{tile:?}");
        assert!(tile.windows(LAYER_NAME.len()).any(|window| window == LAYER_NAME.as_bytes()));
    }

    #[test]
    fn a_line_that_quantizes_to_one_point_writes_no_geometry() {
        let tile = encode_tile(&[vec![[5, 5], [5, 5]]]);
        assert!(!tile.windows(1).any(|window| window == [9]));
    }
}
