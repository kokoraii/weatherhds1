//! Motion estimation between radar frames: pyramidal Lucas-Kanade on a small intensity grid.
//!
//! Runs on the CPU because the pixels are already here at decode time, it only needs a few milliseconds
//! per frame pair, and it can be tested without a GPU. The result is a smooth field the composite shader
//! samples, so the cost of 60 fps is one shader pass rather than hundreds of stored in-between frames.

/// A single-channel image.
#[derive(Clone, Debug, PartialEq)]
pub struct Grid {
    pub width: usize,
    pub height: usize,
    pub data: Vec<f32>,
}

/// Displacement in grid pixels, from the first frame towards the second.
#[derive(Clone, Debug)]
pub struct FlowField {
    pub width: usize,
    pub height: usize,
    pub u: Vec<f32>,
    pub v: Vec<f32>,
}

/// Grid size for a frame, keeping its aspect with the longest side at `longest`.
pub fn grid_size(frame_width: u32, frame_height: u32, longest: u32) -> (usize, usize) {
    let scale = (f64::from(longest) / f64::from(frame_width.max(frame_height))).min(1.0);
    let side = |v: u32| ((f64::from(v) * scale).round() as usize).max(4);
    (side(frame_width), side(frame_height))
}

/// Box-filters an RGBA frame to an intensity grid. Intensity weights brightness by alpha, so transparent
/// pixels are zero and each colour on a radar ramp keeps a consistent value from frame to frame.
pub fn intensity_grid(rgba: &[u8], width: usize, height: usize, grid_width: usize, grid_height: usize) -> Grid {
    let mut sum = vec![0f32; grid_width * grid_height];
    let mut count = vec![0u32; grid_width * grid_height];
    for y in 0..height {
        let gy = y * grid_height / height;
        for x in 0..width {
            let gx = x * grid_width / width;
            let p = &rgba[(y * width + x) * 4..][..4];
            let luma = (0.3 * f32::from(p[0]) + 0.59 * f32::from(p[1]) + 0.11 * f32::from(p[2])) / 255.0;
            sum[gy * grid_width + gx] += f32::from(p[3]) / 255.0 * (0.25 + 0.75 * luma);
            count[gy * grid_width + gx] += 1;
        }
    }
    let mut data: Vec<f32> = sum.iter().zip(&count).map(|(s, &n)| s / n.max(1) as f32).collect();
    box_blur(&mut data, grid_width, grid_height, GRID_BLUR_RADIUS);
    Grid { width: grid_width, height: grid_height, data }
}

impl Grid {
    fn at(&self, x: isize, y: isize) -> f32 {
        let x = x.clamp(0, self.width as isize - 1) as usize;
        let y = y.clamp(0, self.height as isize - 1) as usize;
        self.data[y * self.width + x]
    }

    fn bilinear(&self, x: f32, y: f32) -> f32 {
        let (fx, fy) = (x.floor(), y.floor());
        let (tx, ty) = (x - fx, y - fy);
        let (ix, iy) = (fx as isize, fy as isize);
        let top = self.at(ix, iy) * (1.0 - tx) + self.at(ix + 1, iy) * tx;
        let bottom = self.at(ix, iy + 1) * (1.0 - tx) + self.at(ix + 1, iy + 1) * tx;
        top * (1.0 - ty) + bottom * ty
    }

    /// Half-size copy, or None when too small to be worth another pyramid level.
    fn halved(&self) -> Option<Grid> {
        if self.width.min(self.height) < MIN_LEVEL_SIDE * 2 {
            return None;
        }
        let (width, height) = (self.width / 2, self.height / 2);
        let mut data = Vec::with_capacity(width * height);
        for y in 0..height {
            for x in 0..width {
                let (sx, sy) = ((x * 2) as isize, (y * 2) as isize);
                data.push(
                    (self.at(sx, sy) + self.at(sx + 1, sy) + self.at(sx, sy + 1) + self.at(sx + 1, sy + 1)) / 4.0,
                );
            }
        }
        Some(Grid { width, height, data })
    }
}

const MIN_LEVEL_SIDE: usize = 12;
/// Real motion is under a grid cell per frame, and on sparse echoes deeper levels lock onto false matches.
const PYRAMID_LEVELS: usize = 2;
const ITERATIONS: usize = 4;
const WINDOW_RADIUS: usize = 3;
const FILL_RADIUS: usize = 5;
/// Smallest structure-tensor eigenvalue (over the window) that counts as trackable texture.
const MIN_EIGENVALUE: f32 = 1e-3;
/// Largest correction per iteration, in pixels of the current level. LK diverges on bigger steps.
const MAX_STEP: f32 = 1.5;
/// The final field is blurred over the grid's longest side divided by this.
const FINAL_SMOOTH_DIVISOR: usize = 8;
/// Noise-removing blur on the intensity grid before tracking, in cells.
const GRID_BLUR_RADIUS: usize = 1;
/// Pairs averaged around the one being drawn, nearest counting most.
const TEMPORAL_WEIGHTS: [f32; 7] = [1.0, 2.0, 3.0, 4.0, 3.0, 2.0, 1.0];
/// A frame changing less than this share of the series' typical change is a repeat.
const REPEAT_THRESHOLD: f32 = 0.25;
/// With fewer frames there's no telling what a typical change is.
const MIN_FRAMES_TO_JUDGE: usize = 5;

/// Estimates how `a` moved to become `b`. Both grids must be the same size.
pub fn estimate(a: &Grid, b: &Grid) -> FlowField {
    let mut pyramid = vec![(a.clone(), b.clone())];
    while pyramid.len() < PYRAMID_LEVELS {
        let (top_a, top_b) = pyramid.last().expect("pyramid starts non-empty");
        let (Some(next_a), Some(next_b)) = (top_a.halved(), top_b.halved()) else { break };
        pyramid.push((next_a, next_b));
    }

    let (coarsest_a, _) = pyramid.last().expect("pyramid starts non-empty");
    let mut flow = FlowField::zero(coarsest_a.width, coarsest_a.height);
    for (level, (level_a, level_b)) in pyramid.iter().enumerate().rev() {
        if level + 1 != pyramid.len() {
            flow = flow.doubled(level_a.width, level_a.height);
        }
        refine(level_a, level_b, &mut flow);
    }

    // Storm motion is broad, so anything finer than this is noise that would wobble echo edges.
    let radius = a.width.max(a.height) / FINAL_SMOOTH_DIVISOR;
    box_blur(&mut flow.u, a.width, a.height, radius);
    box_blur(&mut flow.v, a.width, a.height, radius);
    flow
}

/// The motion field to draw for one frame pair: the average velocity of the pairs around it, scaled to
/// this pair's duration. Single pairs swing widely (a mosaic can miss a radar for a scan), and each swing
/// would show as a change of speed at every source frame. `pairs` holds each field with its duration in
/// seconds, in time order, and `centre` is the pair being drawn.
pub fn average_in_time(pairs: &[(&FlowField, f32)], centre: usize) -> FlowField {
    let (first, duration) = pairs[centre];
    let mut out = FlowField::zero(first.width, first.height);
    let half = TEMPORAL_WEIGHTS.len() / 2;
    let mut total = 0.0;
    for (offset, weight) in TEMPORAL_WEIGHTS.iter().enumerate() {
        let Some(index) = (centre + offset).checked_sub(half) else { continue };
        let Some(&(field, seconds)) = pairs.get(index) else { continue };
        total += weight;
        let scale = weight / seconds;
        for i in 0..out.u.len() {
            out.u[i] += field.u[i] * scale;
            out.v[i] += field.v[i] * scale;
        }
    }
    let scale = duration / total;
    for i in 0..out.u.len() {
        out.u[i] *= scale;
        out.v[i] *= scale;
    }
    out
}

/// Which frames actually carry new imagery. The feeds repeat themselves: a timestamp advances while the
/// picture barely changes (satrad's satellite part updates every other frame, and mosaics skip scans).
/// Playing those makes echoes stop and then jump, so a frame only counts if it differs from the last
/// counted one by a real share of this series' typical change. Returns one flag per frame.
pub fn distinct_frames(grids: &[&Grid]) -> Vec<bool> {
    let mut keep = vec![true; grids.len()];
    if grids.len() < MIN_FRAMES_TO_JUDGE {
        return keep;
    }
    let mut changes: Vec<f32> = grids.windows(2).map(|pair| relative_change(pair[0], pair[1])).collect();
    changes.sort_by(f32::total_cmp);
    // The upper quartile, since a series can repeat half its frames and pull the median down.
    let typical = changes[changes.len() * 3 / 4];

    let mut last = 0;
    for i in 1..grids.len() {
        keep[i] = relative_change(grids[last], grids[i]) >= typical * REPEAT_THRESHOLD;
        if keep[i] {
            last = i;
        }
    }
    keep
}

/// Total difference between two grids, relative to how much is in them.
fn relative_change(a: &Grid, b: &Grid) -> f32 {
    let energy: f32 = a.data.iter().chain(&b.data).sum::<f32>() / 2.0;
    let difference: f32 = a.data.iter().zip(&b.data).map(|(x, y)| (x - y).abs()).sum();
    difference / energy.max(1e-6)
}

impl FlowField {
    fn zero(width: usize, height: usize) -> Self {
        Self { width, height, u: vec![0.0; width * height], v: vec![0.0; width * height] }
    }

    /// Upsamples to the next pyramid level, doubling the displacements with it.
    fn doubled(&self, width: usize, height: usize) -> Self {
        let sample = |values: &[f32]| {
            let grid = Grid { width: self.width, height: self.height, data: values.to_vec() };
            let mut out = Vec::with_capacity(width * height);
            for y in 0..height {
                for x in 0..width {
                    out.push(2.0 * grid.bilinear(x as f32 / 2.0, y as f32 / 2.0));
                }
            }
            out
        };
        Self { width, height, u: sample(&self.u), v: sample(&self.v) }
    }
}

/// Lucas-Kanade iterations at one level, then spreads the result from textured areas into flat ones.
fn refine(a: &Grid, b: &Grid, flow: &mut FlowField) {
    let (w, h) = (a.width, a.height);
    let mut ix = vec![0f32; w * h];
    let mut iy = vec![0f32; w * h];
    for y in 0..h {
        for x in 0..w {
            let (sx, sy) = (x as isize, y as isize);
            ix[y * w + x] = (a.at(sx + 1, sy) - a.at(sx - 1, sy)) / 2.0;
            iy[y * w + x] = (a.at(sx, sy + 1) - a.at(sx, sy - 1)) / 2.0;
        }
    }
    let product = |p: &[f32], q: &[f32]| -> Vec<f32> { p.iter().zip(q).map(|(a, b)| a * b).collect() };
    let mut ixx = product(&ix, &ix);
    let mut ixy = product(&ix, &iy);
    let mut iyy = product(&iy, &iy);
    box_blur(&mut ixx, w, h, WINDOW_RADIUS);
    box_blur(&mut ixy, w, h, WINDOW_RADIUS);
    box_blur(&mut iyy, w, h, WINDOW_RADIUS);

    let mut confidence = vec![0f32; w * h];
    for _ in 0..ITERATIONS {
        // How far a, moved by the current flow, still is from b.
        let mut it = vec![0f32; w * h];
        for y in 0..h {
            for x in 0..w {
                let i = y * w + x;
                it[i] = b.bilinear(x as f32 + flow.u[i], y as f32 + flow.v[i]) - a.data[i];
            }
        }
        let mut ixt = product(&ix, &it);
        let mut iyt = product(&iy, &it);
        box_blur(&mut ixt, w, h, WINDOW_RADIUS);
        box_blur(&mut iyt, w, h, WINDOW_RADIUS);

        for i in 0..w * h {
            let det = ixx[i] * iyy[i] - ixy[i] * ixy[i];
            let half_trace = (ixx[i] + iyy[i]) / 2.0;
            let min_eigenvalue = half_trace - (half_trace * half_trace - det).max(0.0).sqrt();
            if min_eigenvalue > MIN_EIGENVALUE {
                flow.u[i] += (-(iyy[i] * ixt[i] - ixy[i] * iyt[i]) / det).clamp(-MAX_STEP, MAX_STEP);
                flow.v[i] += (-(ixx[i] * iyt[i] - ixy[i] * ixt[i]) / det).clamp(-MAX_STEP, MAX_STEP);
                confidence[i] = min_eigenvalue;
            }
        }
    }

    spread(flow, &confidence);
}

/// Replaces the flow with a confidence-weighted average of its neighbourhood. Keeps the estimates where
/// there is texture to track, and carries them out over the empty pixels around a storm cell, which are
/// exactly the pixels the warp samples as the cell moves. With no estimate nearby, the flow is left as
/// the coarser level had it.
fn spread(flow: &mut FlowField, confidence: &[f32]) {
    let (w, h) = (flow.width, flow.height);
    let mut weight: Vec<f32> = confidence.iter().map(|c| (c / MIN_EIGENVALUE).min(4.0)).collect();
    let mut weighted_u: Vec<f32> = flow.u.iter().zip(&weight).map(|(f, c)| f * c).collect();
    let mut weighted_v: Vec<f32> = flow.v.iter().zip(&weight).map(|(f, c)| f * c).collect();
    box_blur(&mut weight, w, h, FILL_RADIUS);
    box_blur(&mut weighted_u, w, h, FILL_RADIUS);
    box_blur(&mut weighted_v, w, h, FILL_RADIUS);
    for i in 0..w * h {
        if weight[i] > 1e-4 {
            flow.u[i] = weighted_u[i] / weight[i];
            flow.v[i] = weighted_v[i] / weight[i];
        }
    }
}

/// Two-axis running-sum box filter, with the window shrinking at the edges.
fn box_blur(values: &mut [f32], width: usize, height: usize, radius: usize) {
    let mut line = vec![0f32; width.max(height)];
    let mut pass = |values: &mut [f32], len: usize, count: usize, stride: usize, step: usize| {
        for n in 0..count {
            let start = n * stride;
            for i in 0..len {
                line[i] = values[start + i * step];
            }
            let mut sum: f32 = line[..=radius.min(len - 1)].iter().sum();
            let mut samples = radius.min(len - 1) + 1;
            for i in 0..len {
                values[start + i * step] = sum / samples as f32;
                if i + radius + 1 < len {
                    sum += line[i + radius + 1];
                    samples += 1;
                }
                if i >= radius {
                    sum -= line[i - radius];
                    samples -= 1;
                }
            }
        }
    };
    pass(values, width, height, width, 1);
    pass(values, height, width, 1, width);
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A soft blob centred at (cx, cy).
    fn blob(width: usize, height: usize, cx: f32, cy: f32) -> Grid {
        let mut data = Vec::with_capacity(width * height);
        for y in 0..height {
            for x in 0..width {
                let (dx, dy) = (x as f32 - cx, y as f32 - cy);
                data.push((-(dx * dx + dy * dy) / 120.0).exp());
            }
        }
        Grid { width, height, data }
    }

    /// A hard-edged disc, like a thresholded radar echo.
    fn disc(width: usize, height: usize, cx: f32, cy: f32, radius: f32) -> Grid {
        let mut data = Vec::with_capacity(width * height);
        for y in 0..height {
            for x in 0..width {
                let (dx, dy) = (x as f32 - cx, y as f32 - cy);
                data.push(if dx * dx + dy * dy < radius * radius { 0.6 } else { 0.0 });
            }
        }
        Grid { width, height, data }
    }

    fn flow_at(flow: &FlowField, x: usize, y: usize) -> (f32, f32) {
        (flow.u[y * flow.width + x], flow.v[y * flow.width + x])
    }

    fn assert_close(actual: (f32, f32), expected: (f32, f32), tolerance: f32) {
        assert!(
            (actual.0 - expected.0).abs() < tolerance && (actual.1 - expected.1).abs() < tolerance,
            "flow {actual:?} is not within {tolerance} of {expected:?}"
        );
    }

    #[test]
    fn recovers_a_blob_translation() {
        let flow = estimate(&blob(128, 96, 60.0, 48.0), &blob(128, 96, 64.0, 45.0));
        assert_close(flow_at(&flow, 62, 46), (4.0, -3.0), 0.75);
    }

    #[test]
    fn tracks_a_hard_edged_echo_and_carries_motion_past_its_edge() {
        let flow = estimate(&disc(128, 96, 60.0, 48.0, 14.0), &disc(128, 96, 63.0, 50.0, 14.0));
        assert_close(flow_at(&flow, 60, 34), (3.0, 2.0), 1.25);
        // Just outside the disc, where there is nothing to track, the warp still needs the motion.
        let outside = flow_at(&flow, 60, 28);
        assert!(outside.0 > 0.5 && outside.1 > 0.5, "motion did not spread outside: {outside:?}");
    }

    #[test]
    fn identical_frames_have_no_motion() {
        let frame = blob(64, 64, 30.0, 30.0);
        let flow = estimate(&frame, &frame);
        assert!(flow.u.iter().chain(&flow.v).all(|v| v.abs() < 1e-3));
    }

    #[test]
    fn empty_frames_have_no_motion() {
        let empty = Grid { width: 64, height: 48, data: vec![0.0; 64 * 48] };
        let flow = estimate(&empty, &empty);
        assert!(flow.u.iter().chain(&flow.v).all(|&v| v == 0.0));
    }

    #[test]
    fn intensity_is_zero_where_transparent_and_keeps_the_grid_shape() {
        // Left half opaque green, right half transparent (but with stray colour that must not count).
        let mut rgba = Vec::new();
        for _ in 0..4 {
            for x in 0..32 {
                rgba.extend_from_slice(if x < 16 { &[0, 255, 0, 255] } else { &[255, 255, 255, 0] });
            }
        }
        let grid = intensity_grid(&rgba, 32, 4, 16, 2);
        assert_eq!((grid.width, grid.height), (16, 2));
        assert!(grid.data[0] > 0.5);
        assert!(grid.data[15].abs() < 1e-5);
    }

    fn uniform_field(u: f32) -> FlowField {
        FlowField { width: 2, height: 2, u: vec![u; 4], v: vec![0.0; 4] }
    }

    #[test]
    fn averaging_in_time_steadies_an_outlier_pair() {
        let fields = [uniform_field(5.0), uniform_field(5.0), uniform_field(20.0), uniform_field(5.0), uniform_field(5.0)];
        let pairs: Vec<(&FlowField, f32)> = fields.iter().map(|field| (field, 300.0)).collect();
        // Weights 3,... around the outlier: (2+3+3+2) * 5 + 4 * 20 over 14.
        let out = average_in_time(&pairs, 2);
        assert!((out.u[0] - 130.0 / 14.0).abs() < 1e-3, "{}", out.u[0]);
    }

    #[test]
    fn averaging_in_time_works_in_velocity_so_a_long_pair_moves_further() {
        // Pair 1 lasts twice as long as its neighbours at the same speed, so it covers twice the distance.
        let fields = [uniform_field(5.0), uniform_field(10.0), uniform_field(5.0)];
        let pairs = [(&fields[0], 300.0), (&fields[1], 600.0), (&fields[2], 300.0)];
        assert!((average_in_time(&pairs, 1).u[0] - 10.0).abs() < 1e-3);
        assert!((average_in_time(&pairs, 0).u[0] - 5.0).abs() < 1e-3);
    }

    #[test]
    fn repeated_frames_are_dropped_whichever_position_they_are_in() {
        let frames: Vec<Grid> = [10.0, 10.0, 20.0, 30.0, 30.0, 40.0, 50.0].iter().map(|&x| blob(64, 48, x, 24.0)).collect();
        let refs: Vec<&Grid> = frames.iter().collect();
        assert_eq!(distinct_frames(&refs), vec![true, false, true, true, false, true, true]);
    }

    #[test]
    fn a_series_repeating_every_other_frame_keeps_the_real_ones() {
        // Near repeats with a tiny change, which is how satrad behaves (its radar part still updates).
        let frames: Vec<Grid> = (0..9)
            .map(|i| {
                let mut grid = blob(64, 48, 10.0 + (i / 2) as f32 * 8.0, 24.0);
                grid.data[0] += i as f32 * 0.001;
                grid
            })
            .collect();
        let refs: Vec<&Grid> = frames.iter().collect();
        let keep = distinct_frames(&refs);
        assert_eq!(keep, vec![true, false, true, false, true, false, true, false, true]);
    }

    #[test]
    fn a_still_or_short_series_keeps_every_frame() {
        let frame = blob(64, 48, 30.0, 24.0);
        assert!(distinct_frames(&[&frame; 8]).iter().all(|&keep| keep));
        assert!(distinct_frames(&[&frame; 3]).iter().all(|&keep| keep));
    }

    #[test]
    fn grid_size_keeps_aspect_and_never_upscales() {
        assert_eq!(grid_size(1024, 512, 192), (192, 96));
        assert_eq!(grid_size(100, 50, 192), (100, 50));
    }
}
