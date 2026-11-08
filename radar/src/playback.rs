//! The loop clock: turns elapsed time into "between frame i and i+1, this far along".

/// Where playback is. `t` runs from 0 at `from` to 1 at `from + 1`.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Position {
    pub from: usize,
    pub to: usize,
    pub t: f32,
}

impl Position {
    /// Where the moment `at` falls on a product's timestamps (oldest first, not empty). Layers update at
    /// different rates, so each finds its own neighbours for the same moment. Outside its range, a layer
    /// holds its nearest frame.
    pub fn at_time(timestamps: &[i64], at: f64) -> Self {
        let after = timestamps.partition_point(|&ts| (ts as f64) <= at);
        if after == 0 {
            return Self { from: 0, to: 0, t: 0.0 };
        }
        if after == timestamps.len() {
            return Self { from: after - 1, to: after - 1, t: 0.0 };
        }
        let (from, to) = (timestamps[after - 1] as f64, timestamps[after] as f64);
        Self { from: after - 1, to: after, t: ((at - from) / (to - from)) as f32 }
    }
}

/// How up to four frames combine into one moment. Blending only the two nearest frames means anything the
/// motion field doesn't explain has to appear or vanish within one frame interval, and the edge remap
/// turns that dissolve into a sudden flip. A wider weighting in time lets edges change gradually, and keeps
/// one-frame speckle from flashing at all.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct TemporalBlend {
    pub frames: [usize; 4],
    /// Each frame's share, summing to 1. Unused slots are 0.
    pub weights: [f32; 4],
    /// The frame pairs whose motion fields carry the frames to this moment, each by its first frame.
    pub pairs: [usize; 3],
    /// How far each frame (row) moves along each pair's field (column) to line up with this moment.
    /// Positive for frames before the moment. Each frame follows the fields of the pairs it actually
    /// crosses, so the warp stays continuous as the moment passes a source frame.
    pub motion: [[f32; 3]; 4],
}

/// Keeps the blend defined where a long run of repeats leaves no frame within the spline's reach.
const MIN_DISSOLVE: f32 = 1e-6;

/// The blend for moment `at` on a product's timestamps (oldest first, not empty). `softness` runs from 0,
/// a straight dissolve between the two nearest frames, to 1, a cubic B-spline across four. It eases to 0
/// at the ends of the loop's `span`, so the loop starts and holds on a source frame alone rather than one
/// mixed with warped neighbours, which shows as a faint offset copy wherever the motion field is off.
pub fn temporal_blend(timestamps: &[i64], at: f64, softness: f32, span: Span) -> TemporalBlend {
    let at = at.clamp(timestamps[0] as f64, timestamps[timestamps.len() - 1] as f64);
    let position = Position::at_time(timestamps, at);
    // The spline is measured in one fixed unit for the whole series. Measuring it in the length of the pair
    // the moment falls in would change every weight at once wherever a dropped repeat leaves a longer pair.
    let unit = typical_spacing(timestamps);
    let from_ends = (at - span.first).min(span.last - at) / span.step.max(1.0);
    let softness = softness * from_ends.clamp(0.0, 1.0) as f32;

    let first = position.from.saturating_sub(1);
    let mut blend = TemporalBlend {
        frames: [0; 4],
        weights: [0.0; 4],
        pairs: [0; 3],
        motion: [[0.0; 3]; 4],
    };
    let last_pair = timestamps.len().saturating_sub(2);
    for (column, pair) in (first..first + 3).enumerate() {
        blend.pairs[column] = pair.min(last_pair);
    }

    let mut total = 0.0;
    for (slot, frame) in (first..timestamps.len()).take(4).enumerate() {
        let ts = timestamps[frame] as f64;
        // A straight dissolve within the pair the moment falls in, which stays continuous however uneven
        // the pairs are.
        let dissolve = if frame == position.from {
            1.0 - position.t
        } else if frame == position.to {
            position.t
        } else {
            0.0
        };
        let spline = cubic_b_spline(((at - ts) / unit) as f32);
        let weight = dissolve * (1.0 - softness).max(MIN_DISSOLVE) + spline * softness;
        blend.frames[slot] = frame;
        blend.weights[slot] = weight;
        total += weight;

        // The share of each pair's interval lying between this frame and the moment.
        let (low, high) = (ts.min(at), ts.max(at));
        let direction = if ts < at { 1.0 } else { -1.0 };
        for (column, pair) in (first..first + 3).enumerate() {
            let (Some(&start), Some(&end)) = (timestamps.get(pair), timestamps.get(pair + 1)) else { continue };
            let (start, end) = (start as f64, end as f64);
            let overlap = (high.min(end) - low.max(start)).max(0.0) / (end - start);
            blend.motion[slot][column] = (direction * overlap) as f32;
        }
    }
    for weight in &mut blend.weights {
        *weight /= total;
    }
    blend
}

/// The cubic B-spline kernel, `distance` units from its centre.
fn cubic_b_spline(distance: f32) -> f32 {
    let distance = distance.abs();
    if distance < 1.0 {
        (4.0 - 6.0 * distance * distance + 3.0 * distance * distance * distance) / 6.0
    } else {
        (2.0 - distance).max(0.0).powi(3) / 6.0
    }
}

/// The usual gap between a series' frames: the median, so the occasional long pair a dropped repeat leaves
/// doesn't stretch it. Most series sit on a fixed cadence, so most pairs are this long or a multiple of it.
fn typical_spacing(timestamps: &[i64]) -> f64 {
    let mut gaps: Vec<i64> = timestamps.windows(2).map(|pair| pair[1] - pair[0]).collect();
    if gaps.is_empty() {
        return 1.0;
    }
    gaps.sort_unstable();
    gaps[gaps.len() / 2] as f64
}

#[derive(Clone, Copy, Debug)]
pub struct Timing {
    /// Playback time per source frame's worth of data.
    pub frame_delay_ms: f64,
    /// Hold on the newest frame before looping.
    pub loop_gap_ms: f64,
    /// Loops before stopping on the newest frame. None loops forever.
    pub max_loops: Option<u32>,
}

/// The stretch of data a loop plays, in timestamps (seconds).
#[derive(Clone, Copy, Debug)]
pub struct Span {
    pub first: f64,
    pub last: f64,
    /// Seconds between source frames, which sets the playback speed.
    pub step: f64,
}

/// A layer updating at most this many times slower than the clock layer counts as the same cadence.
const SAME_CADENCE: f64 = 1.5;

impl Span {
    /// The loop for a stack, from each layer's own span and the layer that sets the clock. Layers on the
    /// clock's cadence are the same kind of imagery (radar mosaics from different feeds), published a few
    /// minutes apart. Where one is a frame behind, it would hold its newest frame while the others move
    /// on, and every echo would show twice. So the loop covers only the stretch they all have. Slower
    /// layers, like satellite, may stop short and hold their newest frame.
    pub fn shared(layers: &[Span], clock: usize) -> Span {
        let clock = layers[clock];
        let peers = layers.iter().filter(|layer| layer.step <= clock.step * SAME_CADENCE);
        let first = peers.clone().map(|layer| layer.first).fold(clock.first, f64::max);
        let last = peers.map(|layer| layer.last).fold(clock.last, f64::min);
        // Still loading, with no overlap yet: play the clock layer alone for now.
        if first > last {
            return clock;
        }
        Span { first, last, step: clock.step }
    }
}

/// The loop clock. It runs on data time at a constant rate, so echoes keep one speed whatever the frames
/// do: a skipped frame or an uneven series can't speed up or slow down the picture.
#[derive(Clone, Debug)]
pub struct Playback {
    timing: Timing,
    /// The moment on screen. None until the first frames arrive.
    now: Option<f64>,
    gap_left_ms: Option<f64>,
    loops: u32,
}

impl Playback {
    pub fn new(timing: Timing) -> Self {
        Self { timing, now: None, gap_left_ms: None, loops: 0 }
    }

    pub fn restart(&mut self) {
        *self = Self::new(self.timing);
    }

    fn finished(&self) -> bool {
        self.timing.max_loops.is_some_and(|max| self.loops >= max)
    }

    /// Moves the clock on and returns the moment to show. The span can change between calls as frames
    /// arrive or roll off.
    pub fn advance(&mut self, dt_ms: f64, span: Span) -> f64 {
        let mut now = self.now.unwrap_or(span.first);

        if let Some(gap) = self.gap_left_ms {
            let remaining = gap - dt_ms;
            if remaining > 0.0 {
                self.gap_left_ms = Some(remaining);
            } else {
                self.gap_left_ms = None;
                self.loops += 1;
                if !self.finished() {
                    now = span.first;
                }
            }
        } else if self.finished() {
            now = span.last;
        } else {
            now += dt_ms * span.step / self.timing.frame_delay_ms;
            if now >= span.last {
                self.gap_left_ms = Some(self.timing.loop_gap_ms);
            }
        }

        let now = now.clamp(span.first, span.last);
        self.now = Some(now);
        now
    }
}

/// True if a frame should be drawn this long after the last one, to hold about `interval_ms` between
/// frames. The margin makes the rate settle on the display's nearest divisor: a 75 Hz monitor draws every
/// refresh and a 144 Hz one every second refresh, rather than falling to 37.5 or 48 fps. An even cadence
/// looks smoother than hitting the target exactly with uneven gaps.
pub fn frame_due(elapsed_ms: f64, interval_ms: f64) -> bool {
    elapsed_ms >= interval_ms * 0.7
}

#[cfg(test)]
mod tests {
    use super::*;

    const SPAN: Span = Span { first: 0.0, last: 600.0, step: 300.0 };

    fn playback(max_loops: Option<u32>) -> Playback {
        Playback::new(Timing { frame_delay_ms: 100.0, loop_gap_ms: 50.0, max_loops })
    }

    #[test]
    fn layers_with_different_cadences_land_on_the_same_moment() {
        assert_eq!(Position::at_time(&[0, 300, 600, 900], 450.0), Position { from: 1, to: 2, t: 0.5 });
        assert_eq!(Position::at_time(&[0, 900], 450.0), Position { from: 0, to: 1, t: 0.5 });
    }

    #[test]
    fn a_layer_holds_its_nearest_frame_outside_its_history() {
        let short_history = [600, 900];
        assert_eq!(Position::at_time(&short_history, 100.0), Position { from: 0, to: 0, t: 0.0 });
        assert_eq!(Position::at_time(&short_history, 1200.0), Position { from: 1, to: 1, t: 0.0 });
        assert_eq!(Position::at_time(&short_history, 900.0), Position { from: 1, to: 1, t: 0.0 });
    }

    #[test]
    fn a_single_frame_holds_still() {
        assert_eq!(Position::at_time(&[500], 500.0), Position { from: 0, to: 0, t: 0.0 });
        let mut clock = playback(None);
        assert_eq!(clock.advance(16.0, Span { first: 500.0, last: 500.0, step: 300.0 }), 500.0);
    }

    #[test]
    fn the_clock_runs_at_one_speed_across_a_missing_frame() {
        // 300 s of data per 100 ms. Frames at 0, 300 and 900 (600 was a repeat) still pass at that rate.
        let span = Span { first: 0.0, last: 900.0, step: 300.0 };
        let mut clock = playback(None);
        let steps: Vec<f64> = (0..5).map(|_| clock.advance(50.0, span)).collect();
        assert_eq!(steps, vec![150.0, 300.0, 450.0, 600.0, 750.0]);
    }

    #[test]
    fn holds_on_the_newest_frame_for_the_gap_then_loops() {
        let mut clock = playback(None);
        assert_eq!(clock.advance(200.0, SPAN), 600.0);
        assert_eq!(clock.advance(25.0, SPAN), 600.0);
        assert_eq!(clock.advance(30.0, SPAN), 0.0);
    }

    #[test]
    fn stops_on_the_newest_frame_after_max_loops() {
        let mut clock = playback(Some(1));
        clock.advance(200.0, SPAN);
        assert_eq!(clock.advance(60.0, SPAN), 600.0);
        assert_eq!(clock.advance(1000.0, SPAN), 600.0);
    }

    #[test]
    fn a_span_rolling_forward_keeps_the_clock_inside_it() {
        let mut clock = playback(None);
        clock.advance(50.0, SPAN);
        assert_eq!(clock.advance(0.0, Span { first: 300.0, last: 900.0, step: 300.0 }), 300.0);
    }

    fn weight_of(blend: &TemporalBlend, frame: usize) -> f32 {
        (0..4).filter(|&slot| blend.frames[slot] == frame).map(|slot| blend.weights[slot]).sum()
    }

    /// A span reaching well past both ends, so no easing at the ends applies.
    const WIDE: Span = Span { first: -1e6, last: 1e6, step: 300.0 };

    #[test]
    fn a_soft_blend_shares_a_frame_with_its_neighbours() {
        let blend = temporal_blend(&[0, 300, 600, 900, 1200], 600.0, 1.0, WIDE);
        assert!((weight_of(&blend, 2) - 4.0 / 6.0).abs() < 1e-5);
        assert!((weight_of(&blend, 1) - 1.0 / 6.0).abs() < 1e-5);
        assert!((weight_of(&blend, 3) - 1.0 / 6.0).abs() < 1e-5);
    }

    #[test]
    fn no_softness_is_a_plain_dissolve_between_the_nearest_two() {
        let blend = temporal_blend(&[0, 300, 600, 900], 450.0, 0.0, WIDE);
        assert!((weight_of(&blend, 1) - 0.5).abs() < 1e-5);
        assert!((weight_of(&blend, 2) - 0.5).abs() < 1e-5);
        assert_eq!(weight_of(&blend, 0), 0.0);
        assert_eq!(weight_of(&blend, 3), 0.0);
    }

    #[test]
    fn the_loop_starts_and_ends_on_a_source_frame_alone() {
        let timestamps = [0, 300, 600, 900, 1200];
        let span = Span { first: 300.0, last: 900.0, step: 300.0 };
        assert_eq!(weight_of(&temporal_blend(&timestamps, 900.0, 1.0, span), 3), 1.0);
        assert_eq!(weight_of(&temporal_blend(&timestamps, 300.0, 1.0, span), 1), 1.0);
        assert!((weight_of(&temporal_blend(&timestamps, 600.0, 1.0, span), 2) - 4.0 / 6.0).abs() < 1e-5);
    }

    /// Where each frame lands relative to the moment, if pair `i`'s motion field moves things `i + 1`.
    fn displacements(blend: &TemporalBlend, timestamps: &[i64]) -> Vec<f32> {
        (0..timestamps.len())
            .map(|frame| {
                let Some(slot) = (0..4).find(|&slot| blend.frames[slot] == frame && blend.weights[slot] > 0.0) else {
                    return f32::NAN;
                };
                (0..3).map(|column| blend.motion[slot][column] * (blend.pairs[column] + 1) as f32).sum()
            })
            .collect()
    }

    #[test]
    fn weights_and_motion_change_smoothly_as_time_passes() {
        // 900 was a repeat and was dropped, leaving one pair twice as long.
        let timestamps = [0, 300, 600, 1200, 1500, 1800, 2100];
        let mut previous = temporal_blend(&timestamps, 300.0, 1.0, WIDE);
        for step in 1..=2400 {
            let at = 300.0 + f64::from(step) * 0.5;
            let blend = temporal_blend(&timestamps, at, 1.0, WIDE);
            for frame in 0..timestamps.len() {
                let change = (weight_of(&blend, frame) - weight_of(&previous, frame)).abs();
                assert!(change < 0.01, "frame {frame}'s weight jumped by {change} at {at}");
            }
            // A frame in both blends must not jump, even as the moment crosses a source frame.
            for (now, before) in displacements(&blend, &timestamps).iter().zip(displacements(&previous, &timestamps)) {
                if now.is_finite() && before.is_finite() {
                    assert!((now - before).abs() < 0.05, "a frame moved from {before} to {now} at {at}");
                }
            }
            previous = blend;
        }
    }

    #[test]
    fn each_frame_follows_the_pairs_between_it_and_the_moment() {
        // Halfway through the middle pair.
        let blend = temporal_blend(&[0, 300, 600, 900], 450.0, 1.0, WIDE);
        assert_eq!(blend.frames, [0, 1, 2, 3]);
        assert_eq!(blend.pairs, [0, 1, 2]);
        assert_eq!(blend.motion, [[1.0, 0.5, 0.0], [0.0, 0.5, 0.0], [0.0, -0.5, 0.0], [0.0, -0.5, -1.0]]);
    }

    #[test]
    fn the_newest_frame_sits_still_and_its_neighbour_comes_forward() {
        let blend = temporal_blend(&[0, 300, 600], 600.0, 1.0, WIDE);
        assert_eq!(blend.frames[..2], [1, 2]);
        assert_eq!(blend.motion[0], [1.0, 0.0, 0.0]);
        assert_eq!(blend.motion[1], [0.0, 0.0, 0.0]);
        assert!(weight_of(&blend, 2) > weight_of(&blend, 1));
    }

    #[test]
    fn a_lone_frame_or_a_moment_outside_the_history_still_adds_up() {
        let lone = temporal_blend(&[500], 500.0, 1.0, WIDE);
        assert!((weight_of(&lone, 0) - 1.0).abs() < 1e-5);
        for at in [-1000.0, 10_000.0] {
            let blend = temporal_blend(&[600, 900], at, 1.0, WIDE);
            assert!((blend.weights.iter().sum::<f32>() - 1.0).abs() < 1e-5);
        }
    }

    #[test]
    fn a_feed_a_frame_behind_ends_the_loop_early() {
        let mosaic = Span { first: 0.0, last: 3600.0, step: 300.0 };
        let lagging_mosaic = Span { first: -600.0, last: 3300.0, step: 300.0 };
        let satellite = Span { first: -900.0, last: 2700.0, step: 900.0 };
        let shared = Span::shared(&[satellite, lagging_mosaic, mosaic], 2);
        assert_eq!((shared.first, shared.last, shared.step), (0.0, 3300.0, 300.0));
    }

    #[test]
    fn feeds_with_no_overlap_yet_play_the_clock_layer() {
        let mosaic = Span { first: 3000.0, last: 3600.0, step: 300.0 };
        let other = Span { first: 0.0, last: 600.0, step: 300.0 };
        assert_eq!(Span::shared(&[other, mosaic], 1).first, 3000.0);
    }

    /// How many refreshes a display at this rate waits between frames for a 60 fps target.
    fn refreshes_between_frames(hz: f64) -> u32 {
        let (refresh, interval) = (1000.0 / hz, 1000.0 / 60.0);
        (1..).find(|&n| frame_due(refresh * f64::from(n), interval)).unwrap()
    }

    #[test]
    fn the_frame_rate_settles_on_an_even_divisor_of_the_display() {
        assert_eq!(refreshes_between_frames(60.0), 1);
        assert_eq!(refreshes_between_frames(75.0), 1);
        assert_eq!(refreshes_between_frames(120.0), 2);
        assert_eq!(refreshes_between_frames(144.0), 2);
        assert_eq!(refreshes_between_frames(240.0), 3);
    }
}
