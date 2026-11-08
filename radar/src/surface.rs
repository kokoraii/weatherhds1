//! One on-screen radar: a canvas that plays a stack of interest lists, bottom layer first.

use crate::config::Interpolation;
use crate::engine::TrackedList;
use crate::geo::ViewRect;
use crate::gpu::Gpu;
use crate::gpu::composite::CompositeParams;
use crate::inventory::PlayFrame;
use crate::playback::{Playback, Span, Timing, frame_due, temporal_blend};
use serde::Deserialize;
use std::cell::RefCell;
use std::rc::Rc;
use web_sys::HtmlCanvasElement;

const FADE_IN_MS: f64 = 250.0;
/// Fraction of the fetched area that fades out at its edge.
const EDGE_FEATHER: f32 = 0.02;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct SurfaceOptions {
    pub frame_delay: f64,
    pub loop_gap: f64,
    /// Absent loops forever.
    pub max_loops: Option<u32>,
}

/// Where the map camera is. Passed in each frame because MapLibre owns it.
#[derive(Clone, Copy, Debug)]
pub struct Camera {
    pub lat: f64,
    pub lon: f64,
    pub zoom: f64,
    pub width: f64,
    pub height: f64,
    pub pixel_ratio: f64,
}

/// One product of the stack on screen.
pub struct Layer {
    pub list: Rc<TrackedList>,
    pub opacity: f32,
    /// When this layer first drew. Layers finish loading at different times, so each fades in on its own.
    fade_start_ms: Option<f64>,
}

impl Layer {
    pub fn new(list: Rc<TrackedList>, opacity: f32) -> Self {
        Self { list, opacity, fade_start_ms: None }
    }
}

pub struct Surface {
    pub name: String,
    pub gpu: Rc<Gpu>,
    canvas: HtmlCanvasElement,
    interpolation: Interpolation,
    frame_interval_ms: f64,
    state: RefCell<State>,
}

struct State {
    layers: Vec<Layer>,
    playback: Playback,
    last_tick_ms: Option<f64>,
    last_draw_ms: f64,
    /// What's on the canvas now, to skip redrawing an unchanged picture (the loop gap, a stopped loop).
    drawn: Option<Vec<CompositeParams>>,
    /// The timestamp of the source frame on screen, for the time label.
    frame_ts: Option<f64>,
}

impl Surface {
    pub fn new(
        name: String,
        gpu: Rc<Gpu>,
        canvas: HtmlCanvasElement,
        options: SurfaceOptions,
        interpolation: Interpolation,
        fps: f64,
    ) -> Self {
        let timing = Timing {
            frame_delay_ms: options.frame_delay,
            loop_gap_ms: options.loop_gap,
            max_loops: options.max_loops,
        };
        Self {
            name,
            gpu,
            canvas,
            interpolation,
            frame_interval_ms: 1000.0 / fps,
            state: RefCell::new(State {
                layers: Vec::new(),
                playback: Playback::new(timing),
                last_tick_ms: None,
                last_draw_ms: f64::NEG_INFINITY,
                drawn: None,
                frame_ts: None,
            }),
        }
    }

    /// Starts playing a stack from its oldest frame. A different stack fades in; the same one restarts.
    pub fn show(&self, layers: Vec<Layer>) {
        let mut state = self.state.borrow_mut();
        state.playback.restart();
        let same = state.layers.len() == layers.len()
            && state.layers.iter().zip(&layers).all(|(a, b)| Rc::ptr_eq(&a.list, &b.list));
        if !same {
            state.layers = layers;
            state.drawn = None;
        }
    }

    /// Moves the clock on and draws if the picture changed. Returns the timestamp (seconds) of the source
    /// frame on screen, for the time label.
    pub fn render(&self, now_ms: f64, camera: Camera) -> Option<f64> {
        let mut state = self.state.borrow_mut();
        if !frame_due(now_ms - state.last_draw_ms, self.frame_interval_ms) {
            return state.frame_ts;
        }
        state.last_draw_ms = now_ms;

        let width = (camera.width * camera.pixel_ratio).round().max(1.0) as u32;
        let height = (camera.height * camera.pixel_ratio).round().max(1.0) as u32;
        if self.canvas.width() != width || self.canvas.height() != height {
            self.canvas.set_width(width);
            self.canvas.set_height(height);
            state.drawn = None;
        }
        self.gpu.resize(width, height);

        // A long gap (the radar was off screen) doesn't count as playback time.
        let dt = state.last_tick_ms.map_or(0.0, |last| (now_ms - last).min(250.0));
        state.last_tick_ms = Some(now_ms);

        let state = &mut *state;
        let playable: Vec<Vec<PlayFrame>> =
            state.layers.iter().map(|layer| layer.list.inventory.borrow().playable()).collect();

        // The top layer with frames sets the loop's pace, and its peers trim the span to what they all have.
        // Every layer finds its place by timestamp.
        let showing: Vec<(&Layer, Vec<i64>, &[PlayFrame])> = state
            .layers
            .iter()
            .zip(&playable)
            .filter(|(_, frames)| !frames.is_empty())
            .map(|(layer, frames)| (layer, layer.list.inventory.borrow().ready_timestamps(), frames.as_slice()))
            .collect();
        let Some((_, clock_ready, _)) = showing.last() else {
            state.frame_ts = None;
            if state.drawn.take().is_some() {
                self.present(&[]);
            }
            return None;
        };
        let spans: Vec<Span> = showing.iter().map(|(_, ready, frames)| layer_span(ready, frames)).collect();
        let span = Span::shared(&spans, spans.len() - 1);

        let at = state.playback.advance(dt, span);
        state.frame_ts = Some(clock_ready[clock_ready.partition_point(|&ts| ts as f64 <= at).max(1) - 1] as f64);

        let view = ViewRect::from_camera(camera.lat, camera.lon, camera.zoom, camera.width, camera.height);
        let mut params = Vec::with_capacity(state.layers.len());
        let mut bind_groups = Vec::with_capacity(state.layers.len());
        for (layer, frames) in state.layers.iter_mut().zip(&playable) {
            if frames.is_empty() {
                continue;
            }
            let layer_params = self.layer_params(layer, frames, at, span, view, camera, now_ms);
            let sets = layer.list.frames.borrow();
            let set = sets.as_ref().expect("a list with ready frames has a frame set");
            set.write_params(&self.gpu.queue, &layer_params);
            bind_groups.push(set.bind_group.clone());
            params.push(layer_params);
        }

        if state.drawn.as_ref() != Some(&params) && self.present(&bind_groups) {
            state.drawn = Some(params);
        }
        state.frame_ts
    }

    #[allow(clippy::too_many_arguments)]
    fn layer_params(
        &self,
        layer: &mut Layer,
        frames: &[PlayFrame],
        at: f64,
        span: Span,
        view: ViewRect,
        camera: Camera,
        now_ms: f64,
    ) -> CompositeParams {
        let timestamps: Vec<i64> = frames.iter().map(|frame| frame.ts).collect();
        let tuning = layer.list.tuning;
        let (blend, use_flow) = match self.interpolation {
            Interpolation::Flow => (temporal_blend(&timestamps, at, tuning.temporal, span), true),
            Interpolation::Crossfade => (temporal_blend(&timestamps, at, 0.0, span), false),
            // Hard cuts: the frame at or before this moment, alone.
            Interpolation::None => {
                let frame = timestamps.partition_point(|&ts| ts as f64 <= at).max(1) - 1;
                let mut blend = temporal_blend(&timestamps[frame..=frame], at, 0.0, span);
                blend.frames = [frame; 4];
                blend.pairs = [frame; 3];
                (blend, false)
            }
        };
        // A pair whose field isn't on the GPU yet doesn't move anything.
        let moves = blend.pairs.map(|pair| use_flow && frames[pair].has_flow);
        let motion = blend.motion.map(|row| {
            let mut shares = [0.0; 4];
            for column in 0..3 {
                shares[column] = if moves[column] { row[column] } else { 0.0 };
            }
            shares
        });

        let fade_start = *layer.fade_start_ms.get_or_insert(now_ms);
        let fade = ((now_ms - fade_start) / FADE_IN_MS).clamp(0.0, 1.0) as f32;

        let rect = layer.list.interest.tiles.world_rect();
        let (frame_width, frame_height) = layer.list.interest.tiles.pixel_size();
        let uv_scale = [(view.width / rect.width) as f32, (view.height / rect.height) as f32];
        let [edge_low, edge_high] = tuning.edge.unwrap_or([0.0, 0.0]);
        let [shadow_px, shadow_opacity] = tuning.shadow.unwrap_or([0.0, 0.0]);
        CompositeParams {
            uv_origin: [((view.min_x - rect.min_x) / rect.width) as f32, ((view.min_y - rect.min_y) / rect.height) as f32],
            uv_scale,
            frame_size: [frame_width as f32, frame_height as f32],
            shadow_offset: [shadow_px / camera.width as f32 * uv_scale[0], shadow_px / camera.height as f32 * uv_scale[1]],
            layers: blend.frames.map(|frame| frames[frame].layer as i32),
            weights: blend.weights,
            flow_layers: [blend.pairs[0], blend.pairs[1], blend.pairs[2], blend.pairs[2]].map(|pair| frames[pair].layer as i32),
            motion,
            opacity: layer.opacity * fade,
            feather: EDGE_FEATHER,
            edge_low,
            edge_high,
            shadow_opacity,
            pad: [0.0; 3],
        }
    }

    /// Draws to the canvas. False if the canvas had no texture to give this frame.
    fn present(&self, bind_groups: &[wgpu::BindGroup]) -> bool {
        let Some(texture) = self.gpu.acquire() else { return false };
        let target = texture.texture.create_view(&Default::default());
        self.gpu.compositor.draw(&self.gpu.device, &self.gpu.queue, &target, bind_groups);
        self.gpu.queue.present(texture);
        true
    }
}

/// One layer's own span: its played frames (not empty), stepped at the cadence of all its ready frames.
/// Repeats still count towards the step: they stand for time passing in the source's cadence.
fn layer_span(ready: &[i64], frames: &[PlayFrame]) -> Span {
    Span {
        first: frames[0].ts as f64,
        last: frames[frames.len() - 1].ts as f64,
        step: (ready[ready.len() - 1] - ready[0]) as f64 / (ready.len().max(2) - 1) as f64,
    }
}
