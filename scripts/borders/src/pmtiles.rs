//! Writes a PMTiles v3 archive: one file holding every tile, which a browser reads with range requests.
//! Spec: https://github.com/protomaps/PMTiles/blob/main/spec/v3/spec.md

use flate2::Compression;
use flate2::write::GzEncoder;
use std::io::{self, Write};

const HEADER_BYTES: u64 = 127;
/// The spec requires the header and root directory to fit in the first 16 KiB.
const MAX_ROOT_BYTES: usize = 16_384 - HEADER_BYTES as usize;
const COMPRESSION_GZIP: u8 = 2;
const TILE_TYPE_MVT: u8 = 1;

pub fn gzip(bytes: &[u8]) -> io::Result<Vec<u8>> {
    let mut encoder = GzEncoder::new(Vec::new(), Compression::best());
    encoder.write_all(bytes)?;
    encoder.finish()
}

/// The archive-wide tile id: tiles ordered by zoom, then along a Hilbert curve.
pub fn tile_id(z: u8, x: u32, y: u32) -> u64 {
    let base: u64 = (0..u32::from(z)).map(|level| 1u64 << (2 * level)).sum();
    let n = 1u64 << z;
    let (mut x, mut y) = (u64::from(x), u64::from(y));
    let mut distance = 0;
    let mut side = n / 2;
    while side > 0 {
        let rx = u64::from(x & side > 0);
        let ry = u64::from(y & side > 0);
        distance += side * side * ((3 * rx) ^ ry);
        // Rotate the quadrant.
        if ry == 0 {
            if rx == 1 {
                x = n - 1 - x;
                y = n - 1 - y;
            }
            std::mem::swap(&mut x, &mut y);
        }
        side /= 2;
    }
    base + distance
}

#[derive(Clone, Copy, Debug, PartialEq)]
struct Entry {
    id: u64,
    run_length: u32,
    offset: u64,
    length: u32,
}

fn put_varint(out: &mut Vec<u8>, mut value: u64) {
    while value >= 0x80 {
        out.push((value as u8) | 0x80);
        value >>= 7;
    }
    out.push(value as u8);
}

/// A gzipped directory, in the spec's columnar form.
fn encode_directory(entries: &[Entry]) -> io::Result<Vec<u8>> {
    let mut out = Vec::new();
    put_varint(&mut out, entries.len() as u64);
    let mut last_id = 0;
    for entry in entries {
        put_varint(&mut out, entry.id - last_id);
        last_id = entry.id;
    }
    for entry in entries {
        put_varint(&mut out, u64::from(entry.run_length));
    }
    for entry in entries {
        put_varint(&mut out, u64::from(entry.length));
    }
    for (i, entry) in entries.iter().enumerate() {
        // 0 means "straight after the previous tile"; anything else is the offset plus one.
        let follows = i > 0 && entry.offset == entries[i - 1].offset + u64::from(entries[i - 1].length);
        put_varint(&mut out, if follows { 0 } else { entry.offset + 1 });
    }
    gzip(&out)
}

/// The root directory and concatenated leaf directories, growing the leaves until the root fits.
fn build_directories(entries: &[Entry]) -> io::Result<(Vec<u8>, Vec<u8>)> {
    let root = encode_directory(entries)?;
    if root.len() <= MAX_ROOT_BYTES {
        return Ok((root, Vec::new()));
    }
    let mut leaf_size = 4096;
    loop {
        let mut leaves = Vec::new();
        let mut root_entries = Vec::new();
        for chunk in entries.chunks(leaf_size) {
            let leaf = encode_directory(chunk)?;
            // Run length 0 marks an entry that points at a leaf directory rather than a tile.
            root_entries.push(Entry { id: chunk[0].id, run_length: 0, offset: leaves.len() as u64, length: leaf.len() as u32 });
            leaves.extend(leaf);
        }
        let root = encode_directory(&root_entries)?;
        if root.len() <= MAX_ROOT_BYTES {
            return Ok((root, leaves));
        }
        leaf_size *= 2;
    }
}

/// Which part of the world and which zooms an archive holds.
pub struct Coverage {
    pub min_zoom: u8,
    pub max_zoom: u8,
}

/// Writes an archive. `tiles` holds (z, x, y, gzipped MVT bytes).
pub fn write_archive<W: Write>(
    output: &mut W,
    tiles: Vec<(u8, u32, u32, Vec<u8>)>,
    coverage: &Coverage,
    metadata_json: &str,
) -> io::Result<()> {
    let mut sorted: Vec<(u64, Vec<u8>)> = tiles.into_iter().map(|(z, x, y, bytes)| (tile_id(z, x, y), bytes)).collect();
    sorted.sort_by_key(|tile| tile.0);

    let mut entries = Vec::with_capacity(sorted.len());
    let mut offset = 0u64;
    for (id, bytes) in &sorted {
        entries.push(Entry { id: *id, run_length: 1, offset, length: bytes.len() as u32 });
        offset += bytes.len() as u64;
    }
    let tile_bytes = offset;

    let (root, leaves) = build_directories(&entries)?;
    let metadata = gzip(metadata_json.as_bytes())?;
    let metadata_offset = HEADER_BYTES + root.len() as u64;
    let leaves_offset = metadata_offset + metadata.len() as u64;
    let tiles_offset = leaves_offset + leaves.len() as u64;

    let mut header = Vec::with_capacity(HEADER_BYTES as usize);
    header.extend_from_slice(b"PMTiles");
    header.push(3);
    let count = sorted.len() as u64;
    for value in [
        HEADER_BYTES, root.len() as u64,
        metadata_offset, metadata.len() as u64,
        leaves_offset, leaves.len() as u64,
        tiles_offset, tile_bytes,
        count, count, count, // addressed tiles, tile entries, tile contents
    ] {
        header.extend_from_slice(&value.to_le_bytes());
    }
    header.push(1); // clustered: tile data is in id order
    header.push(COMPRESSION_GZIP); // directories and metadata
    header.push(COMPRESSION_GZIP); // tiles
    header.push(TILE_TYPE_MVT);
    header.push(coverage.min_zoom);
    header.push(coverage.max_zoom);
    // Bounds and centre, in degrees times 10^7: the whole mercator world, centred on the origin.
    for degrees in [-180i32, -85, 180, 85] {
        header.extend_from_slice(&(degrees * 10_000_000).to_le_bytes());
    }
    header.push(coverage.min_zoom);
    header.extend_from_slice(&0i32.to_le_bytes());
    header.extend_from_slice(&0i32.to_le_bytes());
    assert_eq!(header.len() as u64, HEADER_BYTES);

    output.write_all(&header)?;
    output.write_all(&root)?;
    output.write_all(&metadata)?;
    output.write_all(&leaves)?;
    for (_, bytes) in &sorted {
        output.write_all(bytes)?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tile_ids_match_the_specs_examples() {
        assert_eq!(tile_id(0, 0, 0), 0);
        assert_eq!(tile_id(1, 0, 0), 1);
        assert_eq!(tile_id(1, 0, 1), 2);
        assert_eq!(tile_id(1, 1, 1), 3);
        assert_eq!(tile_id(1, 1, 0), 4);
        assert_eq!(tile_id(2, 0, 0), 5);
    }

    #[test]
    fn varints_use_seven_bits_per_byte() {
        let mut out = Vec::new();
        put_varint(&mut out, 300);
        assert_eq!(out, [0xAC, 0x02]);
    }

    #[test]
    fn a_header_lands_on_the_specs_offsets() {
        let mut file = Vec::new();
        let tiles = vec![(0, 0, 0, gzip(b"tile").unwrap())];
        write_archive(&mut file, tiles, &Coverage { min_zoom: 0, max_zoom: 0 }, "{}").unwrap();
        assert_eq!(&file[..7], b"PMTiles");
        assert_eq!(file[7], 3);
        let u64_at = |at: usize| u64::from_le_bytes(file[at..at + 8].try_into().unwrap());
        assert_eq!(u64_at(8), HEADER_BYTES);
        assert_eq!(u64_at(72), 1, "addressed tiles");
        let (tile_offset, tile_length) = (u64_at(56) as usize, u64_at(64) as usize);
        assert_eq!(tile_offset + tile_length, file.len());
        assert_eq!(file[99], TILE_TYPE_MVT);
    }

    #[test]
    fn a_big_directory_splits_into_leaves_and_keeps_the_root_small() {
        // Pseudo-random lengths and gaps, so the directory doesn't compress away.
        let mut state = 88172645463325252u64;
        let mut next = move || {
            state ^= state << 13;
            state ^= state >> 7;
            state ^= state << 17;
            state
        };
        let mut offset = 0;
        let entries: Vec<Entry> = (0..200_000u64)
            .map(|i| {
                let length = (next() % 60_000) as u32 + 1;
                let entry = Entry { id: i * 3, run_length: 1, offset, length };
                offset += u64::from(length);
                entry
            })
            .collect();
        let (root, leaves) = build_directories(&entries).unwrap();
        assert!(root.len() <= MAX_ROOT_BYTES);
        assert!(!leaves.is_empty());
    }
}
