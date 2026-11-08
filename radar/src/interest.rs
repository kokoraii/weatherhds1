//! Interest lists: what an on-screen radar needs, generated from where it is pointed.
//!
//! Nothing is declared by hand. A surface asks for a view (product, camera, size, frame count) and the
//! list works out which tile zoom gives the best picture and which tiles cover it. Views that fit
//! inside a list the surface already has reuse it, so a resize or repeat visit costs nothing.

use crate::geo::{TileRect, ViewRect};
use serde::{Deserialize, Serialize};

/// Extra coverage around the view, so a slightly larger container doesn't need a new list.
const COVER_MARGIN: f64 = 0.05;

/// One product in a stack.
#[derive(Clone, Debug, Deserialize)]
pub struct LayerRequest {
    pub product: String,
    pub opacity: f32,
}

/// What a surface asks for: products bottom layer first, over one camera. `frames` is the top layer's
/// frame count, and sets how much time the loop covers. `width` and `height` are CSS pixels.
#[derive(Clone, Debug, Deserialize)]
pub struct StackRequest {
    pub layers: Vec<LayerRequest>,
    pub frames: u32,
    /// Zoom levels above the map's to fetch tiles at. 1 draws tiles 1:1; 0 draws them at 2x, which takes
    /// a quarter of the memory.
    pub detail: u8,
    pub lat: f64,
    pub lon: f64,
    pub zoom: f64,
    pub width: f64,
    pub height: f64,
}

/// One product of a stack, which becomes one interest list.
#[derive(Clone, Debug)]
pub struct ViewRequest {
    pub product: String,
    pub frames: u32,
    pub detail: u8,
    pub lat: f64,
    pub lon: f64,
    pub zoom: f64,
    pub width: f64,
    pub height: f64,
}

impl StackRequest {
    pub fn view_of(&self, product: &str, frames: u32) -> ViewRequest {
        ViewRequest {
            product: product.to_owned(),
            frames,
            detail: self.detail,
            lat: self.lat,
            lon: self.lon,
            zoom: self.zoom,
            width: self.width,
            height: self.height,
        }
    }
}

/// The time between frames in a series (newest first), averaged because products can skip a slot.
pub fn series_step(series: &[i64]) -> Option<i64> {
    let (first, last) = (series.first()?, series.last()?);
    (series.len() > 1).then(|| ((first - last) / (series.len() as i64 - 1)).max(1))
}

/// How much time a loop of `frames` frames covers on this series.
pub fn loop_span(frames: u32, series: &[i64]) -> i64 {
    series_step(series).unwrap_or(0) * i64::from(frames.saturating_sub(1))
}

/// A series (newest first) cut down to at most one frame per `cadence` seconds, for a layer that updates
/// faster than the layer setting the loop (NWS mosaics land every 2 minutes, unevenly). Its extra frames
/// would only cost memory and downloads. Keeps the oldest frame in each fixed slot of clock time, so the
/// same frames stay picked as the series rolls forward.
pub fn thin_to_cadence(series: &[i64], cadence: i64) -> Vec<i64> {
    let mut thinned: Vec<i64> = Vec::new();
    for &ts in series.iter().rev() {
        if thinned.last().is_none_or(|&kept| kept.div_euclid(cadence) != ts.div_euclid(cadence)) {
            thinned.push(ts);
        }
    }
    thinned.reverse();
    thinned
}

/// Frames a slower or faster product needs to cover `span` seconds, with one extra at each end so the
/// loop's first and last moments still sit between two of its frames. Computed from the cadence rather
/// than by counting series entries, so the answer doesn't wobble as the series rolls forward and force
/// a new list on every refresh.
pub fn frames_for_span(span: i64, series: &[i64]) -> u32 {
    let step = series_step(series).unwrap_or(1);
    (span as f64 / step as f64).ceil() as u32 + 2
}

#[derive(Clone, Debug, PartialEq, Serialize)]
pub struct InterestList {
    pub id: String,
    /// The on-screen radar this list feeds.
    pub surface: String,
    pub product: String,
    pub frames: u32,
    pub tiles: TileRect,
}

/// The tile zoom to fetch for a map zoom. A z+1 tile is drawn 1:1 on MapLibre's 512px world (`detail` 1);
/// past the product's native zoom the server only upscales, so we stop there and upscale better ourselves.
pub fn tile_zoom(map_zoom: f64, native_zoom: u8, detail: u8) -> u8 {
    ((map_zoom.floor() as i32 + i32::from(detail)).max(0) as u8).min(native_zoom)
}

impl InterestList {
    pub fn generate(surface: &str, request: &ViewRequest, native_zoom: u8) -> Self {
        let view = ViewRect::from_camera(
            request.lat,
            request.lon,
            request.zoom,
            request.width,
            request.height,
        )
        .expanded(COVER_MARGIN);
        let tiles = TileRect::covering(view, tile_zoom(request.zoom, native_zoom, request.detail));
        let id = format!(
            "{surface}:{}:z{}:{}-{}x{}-{}",
            request.product, tiles.z, tiles.x0, tiles.x1, tiles.y0, tiles.y1
        );
        Self { id, surface: surface.to_owned(), product: request.product.clone(), frames: request.frames, tiles }
    }

    /// True if this list already provides everything `wanted` asks for.
    pub fn satisfies(&self, wanted: &InterestList) -> bool {
        self.surface == wanted.surface
            && self.product == wanted.product
            && self.frames >= wanted.frames
            && self.tiles.contains(&wanted.tiles)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn request(zoom: f64, width: f64, height: f64) -> ViewRequest {
        ViewRequest { product: "satrad".into(), frames: 16, detail: 1, lat: 43.65, lon: -79.38, zoom, width, height }
    }

    #[test]
    fn slower_products_cover_the_same_time_with_fewer_frames() {
        let radar: Vec<i64> = (0..48).map(|i| 100_000 - i * 300).collect();
        let satellite: Vec<i64> = (0..16).map(|i| 100_000 - i * 900).collect();
        let span = loop_span(36, &radar);
        assert_eq!(span, 35 * 300);
        // 35 x 5 min is 175 min: 12 steps at 15 min (rounded up), plus one extra frame at each end.
        assert_eq!(frames_for_span(span, &satellite), 14);
    }

    #[test]
    fn a_fast_series_thins_to_the_loops_cadence_and_keeps_its_picks_as_it_rolls() {
        // Every 2 minutes, unevenly, newest first.
        let series = [1090, 960, 845, 720, 600, 485, 360, 240, 125];
        assert_eq!(thin_to_cadence(&series, 360), [1090, 720, 360, 125]);
        assert_eq!(thin_to_cadence(&[1200, 1090, 960, 845, 720], 360), [1090, 720]);
    }

    #[test]
    fn frames_for_span_holds_steady_as_the_series_rolls_forward() {
        let series = |newest: i64| -> Vec<i64> { (0..16).map(|i| newest - i * 900).collect() };
        assert_eq!(frames_for_span(10_500, &series(100_000)), frames_for_span(10_500, &series(100_900)));
    }

    #[test]
    fn fetches_one_zoom_up_until_the_native_zoom() {
        assert_eq!(tile_zoom(4.0, 5, 1), 5);
        assert_eq!(tile_zoom(4.5, 5, 1), 5);
        assert_eq!(tile_zoom(6.0, 5, 1), 5);
        assert_eq!(tile_zoom(7.0, 7, 1), 7);
    }

    #[test]
    fn lower_detail_fetches_a_zoom_down_for_a_quarter_of_the_tiles() {
        assert_eq!(tile_zoom(5.0, 7, 0), 5);
        let view = |detail| InterestList::generate("national", &ViewRequest { detail, ..request(5.0, 960.0, 800.0) }, 7);
        assert!(view(0).tiles.tile_count() * 2 < view(1).tiles.tile_count());
    }

    #[test]
    fn a_smaller_view_of_the_same_place_reuses_the_list() {
        let full = InterestList::generate("main", &request(6.0, 1920.0, 1080.0), 7);
        let small = InterestList::generate("main", &request(6.0, 800.0, 600.0), 7);
        assert!(full.satisfies(&small));
        assert!(!small.satisfies(&full));
    }

    #[test]
    fn lists_are_not_shared_across_products_or_when_short_of_frames() {
        let list = InterestList::generate("main", &request(6.0, 800.0, 600.0), 7);
        let mut other = request(6.0, 800.0, 600.0);
        other.product = "twcRadarHcMosaic".into();
        assert!(!list.satisfies(&InterestList::generate("main", &other, 7)));
        other = request(6.0, 800.0, 600.0);
        other.frames = 48;
        assert!(!list.satisfies(&InterestList::generate("main", &other, 7)));
    }
}
