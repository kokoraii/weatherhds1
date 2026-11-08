//! The ledger of one interest list: which frames it wants, where each one is in the pipeline, which
//! texture layer it occupies, and which motion fields are computed. Holds no GPU or browser state.

use crate::flow::{self, FlowField, Grid};
use serde::Serialize;
use std::collections::BTreeMap;
use std::rc::Rc;

/// How many pairs either side of a field its time average reaches. Matches flow::TEMPORAL_WEIGHTS.
const TEMPORAL_REACH: usize = 3;

/// A layer count that wgpu's WebGL2 backend will store as a plain 2D array, which is how the shader reads
/// it. That backend guesses a texture's kind from its shape: one layer makes a 2D texture, and a square
/// texture with a multiple of 6 layers makes a cube map, which samples as blank.
fn gpu_safe_layers(layers: u32) -> u32 {
    let layers = layers.max(2);
    if layers % 6 == 0 { layers + 1 } else { layers }
}

#[derive(Clone, Copy, Debug, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum FrameState {
    Queued,
    Fetching,
    /// Downloaded and assembled, waiting for its turn on the GPU.
    Decoded,
    Ready,
    Failed,
}

#[derive(Debug)]
struct Entry {
    state: FrameState,
    layer: Option<u32>,
    /// Low-resolution intensity, kept so a motion field can be computed against any neighbour.
    grid: Option<Rc<Grid>>,
    /// False if this frame only repeats the one before it, which keeps it out of playback.
    distinct: bool,
    /// The raw motion field towards the next distinct frame, and which frame that was.
    field: Option<Rc<FlowField>>,
    flow_to: Option<i64>,
    /// The frame that the field in this frame's GPU layer leads to.
    published_to: Option<i64>,
    /// True if a neighbouring field changed, so the time-averaged field on the GPU is out of date.
    stale: bool,
}

/// A frame ready to play.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct PlayFrame {
    pub ts: i64,
    pub layer: u32,
    /// True if this frame's layer holds a motion field towards the next playable frame.
    pub has_flow: bool,
}

/// A time-averaged motion field ready for the GPU.
#[derive(Debug)]
pub struct PublishJob {
    pub from: i64,
    pub to: i64,
    pub layer: u32,
    pub field: FlowField,
}

/// A raw motion field to compute.
#[derive(Debug)]
pub struct FlowJob {
    pub from: i64,
    pub to: i64,
    pub a: Rc<Grid>,
    pub b: Rc<Grid>,
}

#[derive(Debug, Default, Serialize)]
pub struct Counts {
    pub queued: u32,
    pub fetching: u32,
    pub decoded: u32,
    pub ready: u32,
    pub failed: u32,
    /// Ready frames dropped for repeating the one before them.
    pub repeats: u32,
    pub flows: u32,
}

#[derive(Debug)]
pub struct Inventory {
    capacity: u32,
    frames: BTreeMap<i64, Entry>,
    free_layers: Vec<u32>,
    /// False until the first series has settled `capacity` and handed out the layers.
    capacity_known: bool,
}

impl Inventory {
    /// `wanted` is the frame count the list asked for; the real capacity is settled by the first series.
    pub fn new(wanted: u32) -> Self {
        Self { capacity: wanted, frames: BTreeMap::new(), free_layers: Vec::new(), capacity_known: false }
    }

    pub fn capacity(&self) -> u32 {
        self.capacity
    }

    /// Lines the ledger up with the latest series (newest first). Frames that fell out of the window
    /// give their layer back, new ones are queued, and failed ones get another try. Returns the
    /// timestamps to fetch, newest first, so the current picture is on screen first.
    pub fn reconcile(&mut self, series: &[i64]) -> Vec<i64> {
        if !self.capacity_known {
            // A short series never needs the full request's worth of layers. The slack covers the
            // series length wobbling by a frame.
            self.capacity = gpu_safe_layers(self.capacity.min(series.len() as u32 + 2));
            self.free_layers = (0..self.capacity).rev().collect();
            self.capacity_known = true;
        }
        let window = &series[..series.len().min(self.capacity as usize)];

        let stale: Vec<i64> = self.frames.keys().copied().filter(|ts| !window.contains(ts)).collect();
        for ts in stale {
            if let Some(layer) = self.frames.remove(&ts).and_then(|entry| entry.layer) {
                self.free_layers.push(layer);
            }
        }
        self.judge_repeats();

        let mut fetch = Vec::new();
        for &ts in window {
            let entry = self.frames.entry(ts).or_insert(Entry {
                state: FrameState::Queued,
                layer: None,
                grid: None,
                distinct: true,
                field: None,
                flow_to: None,
                published_to: None,
                stale: false,
            });
            match entry.state {
                FrameState::Queued => fetch.push(ts),
                FrameState::Failed => {
                    entry.state = FrameState::Queued;
                    fetch.push(ts);
                }
                _ => {}
            }
        }
        fetch
    }

    /// Marks a queued frame as being fetched. False if it left the window while waiting.
    pub fn begin_fetch(&mut self, ts: i64) -> bool {
        match self.frames.get_mut(&ts) {
            Some(entry) if entry.state == FrameState::Queued => {
                entry.state = FrameState::Fetching;
                true
            }
            _ => false,
        }
    }

    pub fn mark_failed(&mut self, ts: i64) {
        if let Some(entry) = self.frames.get_mut(&ts) {
            entry.state = FrameState::Failed;
        }
    }

    /// Records a fetched frame's intensity grid. False if it left the window during the download.
    pub fn mark_decoded(&mut self, ts: i64, grid: Rc<Grid>) -> bool {
        match self.frames.get_mut(&ts) {
            Some(entry) => {
                entry.state = FrameState::Decoded;
                entry.grid = Some(grid);
                true
            }
            None => false,
        }
    }

    /// Takes a layer for a decoded frame about to go to the GPU, and marks it ready. None if the frame
    /// left the window.
    pub fn claim_layer(&mut self, ts: i64) -> Option<u32> {
        let entry = self.frames.get_mut(&ts)?;
        let layer = self.free_layers.pop()?;
        entry.layer = Some(layer);
        entry.state = FrameState::Ready;
        self.judge_repeats();
        Some(layer)
    }

    /// Reworks which ready frames carry new imagery. Runs whenever the ready frames change.
    fn judge_repeats(&mut self) {
        let ready = self.frames.values().filter(|entry| entry.state == FrameState::Ready);
        let grids: Vec<Rc<Grid>> = ready.map(|entry| entry.grid.clone().expect("ready frames hold a grid")).collect();
        let refs: Vec<&Grid> = grids.iter().map(Rc::as_ref).collect();
        let distinct = flow::distinct_frames(&refs);
        let ready = self.frames.values_mut().filter(|entry| entry.state == FrameState::Ready);
        for (entry, keep) in ready.zip(distinct) {
            entry.distinct = keep;
        }
    }

    /// Timestamps of every ready frame including repeats, oldest first, for the loop's span and time label.
    pub fn ready_timestamps(&self) -> Vec<i64> {
        self.frames.iter().filter(|(_, entry)| entry.state == FrameState::Ready).map(|(&ts, _)| ts).collect()
    }

    fn playing(&self) -> Vec<(i64, &Entry)> {
        self.frames
            .iter()
            .filter(|(_, entry)| entry.state == FrameState::Ready && entry.distinct)
            .map(|(&ts, entry)| (ts, entry))
            .collect()
    }

    /// The frames to play, oldest first.
    pub fn playable(&self) -> Vec<PlayFrame> {
        let playing = self.playing();
        playing
            .iter()
            .enumerate()
            .map(|(i, &(ts, entry))| PlayFrame {
                ts,
                layer: entry.layer.expect("ready frames hold a layer"),
                has_flow: playing.get(i + 1).is_some_and(|&(next, _)| entry.published_to == Some(next)),
            })
            .collect()
    }

    /// The next raw motion field missing between neighbouring frames, newest first.
    pub fn next_flow_job(&self) -> Option<FlowJob> {
        self.playing().windows(2).rev().find_map(|pair| {
            let [(from, a), (to, b)] = pair else { unreachable!() };
            (a.flow_to != Some(*to)).then(|| FlowJob {
                from: *from,
                to: *to,
                a: a.grid.clone().expect("ready frames hold a grid"),
                b: b.grid.clone().expect("ready frames hold a grid"),
            })
        })
    }

    /// Records a raw field. The averages of the pairs around it are now out of date.
    pub fn set_flow(&mut self, from: i64, to: i64, field: FlowField) {
        let Some(entry) = self.frames.get_mut(&from) else { return };
        entry.field = Some(Rc::new(field));
        entry.flow_to = Some(to);

        let playing: Vec<i64> = self.playing().iter().map(|&(ts, _)| ts).collect();
        let Some(centre) = playing.iter().position(|&ts| ts == from) else { return };
        let (start, end) = (centre.saturating_sub(TEMPORAL_REACH), (centre + TEMPORAL_REACH + 1).min(playing.len()));
        for ts in &playing[start..end] {
            self.frames.get_mut(ts).expect("playing frames are in the ledger").stale = true;
        }
    }

    /// The next averaged field to send to the GPU. Waits until every raw field is in, so loading doesn't
    /// redo the same averages as each new field lands.
    pub fn next_publish_job(&self) -> Option<PublishJob> {
        if self.next_flow_job().is_some() {
            return None;
        }
        let playing = self.playing();
        let pairs: Vec<(&FlowField, f32)> = playing
            .windows(2)
            .map(|pair| {
                let [(from, a), (to, _)] = pair else { unreachable!() };
                (a.field.as_deref().expect("every pair has a field by now"), (to - from) as f32)
            })
            .collect();
        (0..pairs.len()).find_map(|i| {
            let (from, entry) = playing[i];
            let to = playing[i + 1].0;
            (entry.stale || entry.published_to != Some(to)).then(|| PublishJob {
                from,
                to,
                layer: entry.layer.expect("ready frames hold a layer"),
                field: flow::average_in_time(&pairs, i),
            })
        })
    }

    /// Marks a field as on the GPU.
    pub fn set_published(&mut self, from: i64, to: i64) {
        if let Some(entry) = self.frames.get_mut(&from) {
            entry.published_to = Some(to);
            entry.stale = false;
        }
    }

    pub fn counts(&self) -> Counts {
        let mut counts = Counts::default();
        for entry in self.frames.values() {
            match entry.state {
                FrameState::Queued => counts.queued += 1,
                FrameState::Fetching => counts.fetching += 1,
                FrameState::Decoded => counts.decoded += 1,
                FrameState::Ready => {
                    counts.ready += 1;
                    counts.repeats += u32::from(!entry.distinct);
                }
                FrameState::Failed => counts.failed += 1,
            }
            counts.flows += u32::from(entry.published_to.is_some());
        }
        counts
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn grid() -> Rc<Grid> {
        Rc::new(Grid { width: 1, height: 1, data: vec![0.0] })
    }

    fn field() -> FlowField {
        FlowField { width: 1, height: 1, u: vec![1.0], v: vec![0.0] }
    }

    /// Runs a frame through the whole pipeline.
    fn make_ready(inventory: &mut Inventory, ts: i64) -> u32 {
        make_ready_with(inventory, ts, grid())
    }

    fn make_ready_with(inventory: &mut Inventory, ts: i64, grid: Rc<Grid>) -> u32 {
        assert!(inventory.begin_fetch(ts));
        assert!(inventory.mark_decoded(ts, grid));
        inventory.claim_layer(ts).unwrap()
    }

    /// Computes and publishes every outstanding field.
    fn settle(inventory: &mut Inventory) {
        while let Some(job) = inventory.next_flow_job() {
            inventory.set_flow(job.from, job.to, field());
        }
        while let Some(job) = inventory.next_publish_job() {
            inventory.set_published(job.from, job.to);
        }
    }

    #[test]
    fn queues_newest_first_and_caps_to_capacity() {
        let mut inventory = Inventory::new(2);
        assert_eq!(inventory.reconcile(&[30, 20, 10]), vec![30, 20]);
    }

    #[test]
    fn layer_counts_stay_clear_of_the_webgl_cube_map_shapes() {
        for wanted in 1..100 {
            let mut inventory = Inventory::new(wanted);
            inventory.reconcile(&(0..200).collect::<Vec<i64>>());
            let capacity = inventory.capacity();
            assert!(capacity >= 2 && capacity % 6 != 0, "{wanted} frames made {capacity} layers");
        }
    }

    #[test]
    fn short_series_shrinks_the_layer_budget() {
        let mut inventory = Inventory::new(48);
        inventory.reconcile(&[30, 20, 10]);
        assert_eq!(inventory.capacity(), 5);
    }

    #[test]
    fn rolling_forward_recycles_the_oldest_layer_and_only_fetches_the_new_frame() {
        let mut inventory = Inventory::new(2);
        inventory.reconcile(&[20, 10]);
        let _newer = make_ready(&mut inventory, 20);
        let older = make_ready(&mut inventory, 10);

        assert_eq!(inventory.reconcile(&[30, 20]), vec![30]);
        assert_eq!(make_ready(&mut inventory, 30), older);
        let order: Vec<i64> = inventory.playable().iter().map(|f| f.ts).collect();
        assert_eq!(order, vec![20, 30]);
    }

    #[test]
    fn a_frame_dropped_mid_download_is_ignored_on_arrival() {
        let mut inventory = Inventory::new(2);
        inventory.reconcile(&[20, 10]);
        assert!(inventory.begin_fetch(10));
        inventory.reconcile(&[40, 30]);
        assert!(!inventory.mark_decoded(10, grid()));
    }

    #[test]
    fn failed_frames_retry_on_the_next_reconcile_and_are_not_played() {
        let mut inventory = Inventory::new(2);
        inventory.reconcile(&[20, 10]);
        inventory.begin_fetch(10);
        inventory.mark_failed(10);
        make_ready(&mut inventory, 20);
        assert_eq!(inventory.playable().len(), 1);
        assert_eq!(inventory.reconcile(&[20, 10]), vec![10]);
    }

    #[test]
    fn flow_jobs_follow_the_ready_frames_and_redo_after_a_gap_fills() {
        let mut inventory = Inventory::new(3);
        inventory.reconcile(&[30, 20, 10]);
        make_ready(&mut inventory, 30);
        make_ready(&mut inventory, 10);

        // 20 hasn't arrived, so 10 flows straight to 30.
        let job = inventory.next_flow_job().unwrap();
        assert_eq!((job.from, job.to), (10, 30));
        settle(&mut inventory);
        assert!(inventory.next_flow_job().is_none());
        assert!(inventory.playable()[0].has_flow);

        // Once 20 lands, 10's field points at the wrong frame and both pairs are due, newest first.
        make_ready(&mut inventory, 20);
        assert!(!inventory.playable()[0].has_flow);
        let job = inventory.next_flow_job().unwrap();
        assert_eq!((job.from, job.to), (20, 30));
        inventory.set_flow(20, 30, field());
        let job = inventory.next_flow_job().unwrap();
        assert_eq!((job.from, job.to), (10, 20));
    }

    #[test]
    fn averaged_fields_wait_for_every_raw_field_and_redo_when_a_neighbour_changes() {
        let mut inventory = Inventory::new(4);
        inventory.reconcile(&[40, 30, 20, 10]);
        for ts in [40, 30, 20, 10] {
            make_ready(&mut inventory, ts);
        }
        // Raw fields still outstanding: nothing to publish yet, and the pair stays a plain dissolve.
        let job = inventory.next_flow_job().unwrap();
        inventory.set_flow(job.from, job.to, field());
        assert!(inventory.next_publish_job().is_none());

        settle(&mut inventory);
        let played = inventory.playable();
        assert!(played[..3].iter().all(|frame| frame.has_flow));
        assert!(!played[3].has_flow);

        // A newly computed neighbour makes the published averages stale, but they still draw meanwhile.
        inventory.set_flow(20, 30, field());
        assert!(inventory.next_publish_job().is_some());
        assert!(inventory.playable()[1].has_flow);
    }

    #[test]
    fn repeated_frames_leave_the_loop_but_keep_their_timestamp() {
        let moved = |x: usize| {
            let mut data = vec![0.0; 64];
            data[x] = 1.0;
            Rc::new(Grid { width: 8, height: 8, data })
        };
        let mut inventory = Inventory::new(6);
        inventory.reconcile(&[60, 50, 40, 30, 20, 10]);
        // 30 repeats 20; the rest each differ.
        for (ts, x) in [(10, 0), (20, 10), (30, 10), (40, 20), (50, 30), (60, 40)] {
            make_ready_with(&mut inventory, ts, moved(x));
        }
        let played: Vec<i64> = inventory.playable().iter().map(|frame| frame.ts).collect();
        assert_eq!(played, vec![10, 20, 40, 50, 60]);
        assert_eq!(inventory.ready_timestamps(), vec![10, 20, 30, 40, 50, 60]);
        assert_eq!(inventory.counts().repeats, 1);

        // Motion runs straight across the repeat.
        let jobs: Vec<(i64, i64)> = std::iter::from_fn(|| {
            let job = inventory.next_flow_job()?;
            inventory.set_flow(job.from, job.to, field());
            Some((job.from, job.to))
        })
        .collect();
        assert!(jobs.contains(&(20, 40)));
    }
}
