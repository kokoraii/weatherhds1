//! Web-mercator and tile math. World coordinates are normalised: (0,0) is the north-west corner of
//! the map and (1,1) the south-east corner.

use serde::Serialize;

/// MapLibre's world is this many CSS pixels wide at zoom 0.
pub const MAP_WORLD_PX: f64 = 512.0;
/// The tile server's PNGs are this many pixels square at every zoom.
pub const SOURCE_TILE_PX: u32 = 256;

pub fn lat_lon_to_world(lat: f64, lon: f64) -> (f64, f64) {
    let sin = lat.to_radians().sin();
    let x = (lon + 180.0) / 360.0;
    let y = 0.5 - ((1.0 + sin) / (1.0 - sin)).ln() / (4.0 * std::f64::consts::PI);
    (x, y)
}

/// The world-space rectangle a map camera shows.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct ViewRect {
    pub min_x: f64,
    pub min_y: f64,
    pub width: f64,
    pub height: f64,
}

impl ViewRect {
    pub fn from_camera(lat: f64, lon: f64, zoom: f64, width_px: f64, height_px: f64) -> Self {
        let (x, y) = lat_lon_to_world(lat, lon);
        let world_px = MAP_WORLD_PX * 2f64.powf(zoom);
        let (width, height) = (width_px / world_px, height_px / world_px);
        Self { min_x: x - width / 2.0, min_y: y - height / 2.0, width, height }
    }

    /// Grows the rectangle by `fraction` of its size on every side.
    pub fn expanded(self, fraction: f64) -> Self {
        let (dx, dy) = (self.width * fraction, self.height * fraction);
        Self {
            min_x: self.min_x - dx,
            min_y: self.min_y - dy,
            width: self.width + 2.0 * dx,
            height: self.height + 2.0 * dy,
        }
    }
}

/// A rectangle of tiles at one zoom level. `x1` and `y1` are exclusive.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
pub struct TileRect {
    pub z: u8,
    pub x0: u32,
    pub y0: u32,
    pub x1: u32,
    pub y1: u32,
}

impl TileRect {
    /// The smallest rectangle of tiles covering `view`, clamped to the world (no wrapping: the maps
    /// run with renderWorldCopies off).
    pub fn covering(view: ViewRect, z: u8) -> Self {
        let n = 1u32 << z;
        let scale = f64::from(n);
        let low = |v: f64| ((v * scale).floor().max(0.0) as u32).min(n - 1);
        let high = |v: f64| ((v * scale).ceil().max(1.0) as u32).min(n);
        Self {
            z,
            x0: low(view.min_x),
            y0: low(view.min_y),
            x1: high(view.min_x + view.width),
            y1: high(view.min_y + view.height),
        }
    }

    pub fn contains(&self, other: &TileRect) -> bool {
        self.z == other.z
            && self.x0 <= other.x0
            && self.y0 <= other.y0
            && self.x1 >= other.x1
            && self.y1 >= other.y1
    }

    pub fn tile_count(&self) -> u32 {
        (self.x1 - self.x0) * (self.y1 - self.y0)
    }

    /// Size of the frame these tiles assemble into.
    pub fn pixel_size(&self) -> (u32, u32) {
        ((self.x1 - self.x0) * SOURCE_TILE_PX, (self.y1 - self.y0) * SOURCE_TILE_PX)
    }

    pub fn world_rect(&self) -> ViewRect {
        let scale = f64::from(1u32 << self.z);
        ViewRect {
            min_x: f64::from(self.x0) / scale,
            min_y: f64::from(self.y0) / scale,
            width: f64::from(self.x1 - self.x0) / scale,
            height: f64::from(self.y1 - self.y0) / scale,
        }
    }

    /// Every (x, y) in the rectangle, in reading order.
    pub fn tiles(self) -> impl Iterator<Item = (u32, u32)> {
        (self.y0..self.y1).flat_map(move |y| (self.x0..self.x1).map(move |x| (x, y)))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn world_origin_and_centre() {
        let (x, y) = lat_lon_to_world(0.0, 0.0);
        assert!((x - 0.5).abs() < 1e-12 && (y - 0.5).abs() < 1e-12);
        let (x, y) = lat_lon_to_world(85.0511287798, -180.0);
        assert!(x.abs() < 1e-9 && y.abs() < 1e-6);
    }

    #[test]
    fn toronto_lands_on_its_standard_xyz_tile() {
        let view = ViewRect::from_camera(43.65, -79.38, 7.0, 1.0, 1.0);
        let tiles = TileRect::covering(view, 7);
        assert_eq!((tiles.x0, tiles.y0), (35, 46));
    }

    #[test]
    fn covering_contains_the_view_and_clamps_to_the_world() {
        let view = ViewRect::from_camera(0.0, 0.0, 2.0, 1024.0, 1024.0);
        let rect = TileRect::covering(view, 3);
        let world = rect.world_rect();
        assert!(world.min_x <= view.min_x && world.min_y <= view.min_y);
        assert!(world.min_x + world.width >= view.min_x + view.width);

        let huge = ViewRect { min_x: -1.0, min_y: -1.0, width: 3.0, height: 3.0 };
        let clamped = TileRect::covering(huge, 2);
        assert_eq!((clamped.x0, clamped.y0, clamped.x1, clamped.y1), (0, 0, 4, 4));
    }
}
