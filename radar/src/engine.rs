//! The page-wide half of the radar: tracks every interest list, fetches their frames in the background,
//! and feeds finished frames to the GPU a little at a time so playback never stutters for it.

use crate::config::{Interpolation, RadarConfig, Tuning};
use crate::flow::{self, Grid};
use crate::gpu::Gpu;
use crate::gpu::frameset::FrameSet;
use crate::interest::{
    InterestList, LayerRequest, StackRequest, ViewRequest, frames_for_span, loop_span, series_step, thin_to_cadence,
};
use crate::inventory::{Counts, Inventory};
use crate::net::{self, Catalog, Product};
use crate::surface::Layer;
use serde::Serialize;
use std::cell::{Cell, RefCell};
use std::collections::VecDeque;
use std::rc::Rc;
use wasm_bindgen::JsValue;
use wasm_bindgen_futures::spawn_local;

pub struct TrackedList {
    pub interest: InterestList,
    pub tuning: Tuning,
    pub gpu: Rc<Gpu>,
    pub inventory: RefCell<Inventory>,
    /// Created when the first frame arrives, once the series has settled how many layers it needs.
    pub frames: RefCell<Option<FrameSet>>,
    grid_size: (usize, usize),
    /// For a layer under a slower one: the loop's cadence, which its series is thinned to.
    thin_to: Option<i64>,
}

struct FetchJob {
    list: Rc<TrackedList>,
    ts: i64,
}

/// A frame downloaded and assembled, waiting for its GPU pass.
struct DecodedFrame {
    list: Rc<TrackedList>,
    ts: i64,
    rgba: Vec<u8>,
}

#[derive(Serialize)]
pub struct ListStatus<'a> {
    #[serde(flatten)]
    interest: &'a InterestList,
    capacity: u32,
    /// Frame texture memory this list takes once loaded.
    memory_mb: u32,
    #[serde(flatten)]
    counts: Counts,
}

pub struct Engine {
    pub config: RadarConfig,
    api_key: String,
    catalog: RefCell<Catalog>,
    lists: RefCell<Vec<Rc<TrackedList>>>,
    jobs: RefCell<VecDeque<FetchJob>>,
    decoded: RefCell<VecDeque<DecodedFrame>>,
    active_workers: Cell<usize>,
    last_refresh_ms: Cell<Option<f64>>,
}

impl Engine {
    pub async fn new(config: RadarConfig, api_key: String) -> Rc<Self> {
        let catalog = net::fetch_catalog(&api_key, &config.layer_products()).await;
        Rc::new(Self {
            config,
            api_key,
            catalog: RefCell::new(catalog),
            lists: RefCell::new(Vec::new()),
            jobs: RefCell::new(VecDeque::new()),
            decoded: RefCell::new(VecDeque::new()),
            active_workers: Cell::new(0),
            last_refresh_ms: Cell::new(None),
        })
    }

    /// The lists for a stack, bottom layer first. The top layer's frame count sets how much time the loop
    /// covers, and every other layer gets however many of its own frames span the same time. A product
    /// that's unavailable right now is left out, and joins the next time this stack is asked for after a
    /// refresh finds it. Products must appear in `config.radar.layers` to be looked up at all.
    pub fn want_stack(self: &Rc<Self>, surface: &str, gpu: &Rc<Gpu>, request: &StackRequest) -> Vec<Layer> {
        let products: Vec<(&LayerRequest, Product)> = request
            .layers
            .iter()
            .filter_map(|layer| Some((layer, self.catalog.borrow().get(&layer.product)?.clone())))
            .collect();
        let Some((top, top_product)) = products.last() else { return Vec::new() };
        let span = loop_span(request.frames, &top_product.timestamps);
        let cadence = series_step(&top_product.timestamps).unwrap_or(1);

        products
            .iter()
            .map(|(layer, product)| {
                let (frames, thin_to) = if layer.product == top.product {
                    (request.frames, None)
                } else {
                    (frames_for_span(span, &thin_to_cadence(&product.timestamps, cadence)), Some(cadence))
                };
                let view = request.view_of(&layer.product, frames);
                let list = self.want(surface, gpu, &view, product, thin_to);
                Layer::new(list, layer.opacity)
            })
            .collect()
    }

    /// The list for a view: one this surface already has if it covers the view, else a new one, whose
    /// frames start downloading straight away.
    fn want(
        self: &Rc<Self>,
        surface: &str,
        gpu: &Rc<Gpu>,
        request: &ViewRequest,
        product: &Product,
        thin_to: Option<i64>,
    ) -> Rc<TrackedList> {
        let interest = InterestList::generate(surface, request, product.native_zoom);
        if let Some(existing) = self.lists.borrow().iter().find(|list| list.interest.satisfies(&interest)) {
            return existing.clone();
        }

        let (width, height) = interest.tiles.pixel_size();
        let list = Rc::new(TrackedList {
            tuning: self.config.tuning(&interest.product),
            gpu: gpu.clone(),
            inventory: RefCell::new(Inventory::new(interest.frames)),
            frames: RefCell::new(None),
            grid_size: flow::grid_size(width, height, self.config.flow_resolution),
            thin_to,
            interest,
        });
        self.lists.borrow_mut().push(list.clone());
        self.queue_frames(&list, &product.timestamps);
        list
    }

    /// Brings a list's frames in line with its product's series (newest first).
    fn queue_frames(self: &Rc<Self>, list: &Rc<TrackedList>, series: &[i64]) {
        let series = match list.thin_to {
            Some(cadence) => thin_to_cadence(series, cadence),
            None => series.to_vec(),
        };
        let fetch = list.inventory.borrow_mut().reconcile(&series);
        self.jobs.borrow_mut().extend(fetch.into_iter().map(|ts| FetchJob { list: list.clone(), ts }));
        self.start_workers();
    }

    /// Moves a stack's pending downloads to the front of the queue. Called when it goes on screen.
    pub fn prioritize(&self, layers: &[Layer]) {
        let on_stack = |job: &FetchJob| layers.iter().any(|layer| Rc::ptr_eq(&job.list, &layer.list));
        self.jobs.borrow_mut().make_contiguous().sort_by_key(|job| !on_stack(job));
    }

    fn start_workers(self: &Rc<Self>) {
        while self.active_workers.get() < self.config.concurrency.max(1) && !self.jobs.borrow().is_empty() {
            self.active_workers.set(self.active_workers.get() + 1);
            let engine = self.clone();
            spawn_local(async move {
                engine.run_worker().await;
                engine.active_workers.set(engine.active_workers.get() - 1);
            });
        }
    }

    async fn run_worker(&self) {
        loop {
            let Some(job) = self.jobs.borrow_mut().pop_front() else { return };
            if !job.list.inventory.borrow_mut().begin_fetch(job.ts) {
                continue;
            }
            let interest = &job.list.interest;
            match net::fetch_frame(&self.api_key, &interest.product, job.ts, interest.tiles).await {
                Ok(rgba) => self.accept_frame(job, rgba),
                Err(error) => {
                    web_sys::console::warn_2(&format!("[radar] {} frame {} failed:", interest.id, job.ts).into(), &error);
                    job.list.inventory.borrow_mut().mark_failed(job.ts);
                }
            }
        }
    }

    fn accept_frame(&self, job: FetchJob, rgba: Vec<u8>) {
        let (width, height) = job.list.interest.tiles.pixel_size();
        let (grid_width, grid_height) = job.list.grid_size;
        let grid: Grid = flow::intensity_grid(&rgba, width as usize, height as usize, grid_width, grid_height);
        if job.list.inventory.borrow_mut().mark_decoded(job.ts, Rc::new(grid)) {
            self.decoded.borrow_mut().push_back(DecodedFrame { list: job.list, ts: job.ts, rgba });
        }
    }

    /// One slice of background work. Called once per animation frame: at most one frame through the GPU
    /// passes and one motion field, which is what keeps the page's main thread smooth during loading.
    pub fn pump(self: &Rc<Self>, now_ms: f64) {
        self.refresh_if_due(now_ms);
        let decoded = self.decoded.borrow_mut().pop_front();
        if let Some(frame) = decoded {
            ingest(frame);
        }
        if self.config.interpolation == Interpolation::Flow {
            self.compute_one_flow();
        }
    }

    /// Advances motion work by one step: a raw field, or else one averaged field to the GPU.
    fn compute_one_flow(&self) {
        let lists = self.lists.borrow();
        for list in lists.iter() {
            let job = list.inventory.borrow().next_flow_job();
            if let Some(job) = job {
                let field = flow::estimate(&job.a, &job.b);
                list.inventory.borrow_mut().set_flow(job.from, job.to, field);
                return;
            }
            let job = list.inventory.borrow().next_publish_job();
            if let Some(job) = job {
                let frames = list.frames.borrow();
                let frames = frames.as_ref().expect("a list with ready frames has a frame set");
                frames.write_flow(&list.gpu.queue, job.layer, &job.field);
                list.inventory.borrow_mut().set_published(job.from, job.to);
                return;
            }
        }
    }

    fn refresh_if_due(self: &Rc<Self>, now_ms: f64) {
        let Some(last) = self.last_refresh_ms.get() else {
            self.last_refresh_ms.set(Some(now_ms));
            return;
        };
        if now_ms - last < self.config.refresh_seconds * 1000.0 {
            return;
        }
        self.last_refresh_ms.set(Some(now_ms));
        let engine = self.clone();
        spawn_local(async move {
            let catalog = net::fetch_catalog(&engine.api_key, &engine.config.layer_products()).await;
            engine.apply_catalog(catalog);
        });
    }

    /// Takes in fresh series. A source that failed this round keeps its last known series.
    fn apply_catalog(self: &Rc<Self>, catalog: Catalog) {
        let lists = self.lists.borrow().clone();
        for list in &lists {
            if let Some(product) = catalog.get(&list.interest.product) {
                self.queue_frames(list, &product.timestamps);
            }
        }
        self.catalog.borrow_mut().extend(catalog);
    }

    /// A snapshot of every list and how far along its frames are, for debugging from the console.
    pub fn inventory(&self) -> Result<JsValue, JsValue> {
        let lists = self.lists.borrow();
        let inventories: Vec<_> = lists.iter().map(|list| list.inventory.borrow()).collect();
        let status: Vec<ListStatus> = lists
            .iter()
            .zip(&inventories)
            .map(|(list, inventory)| {
                let (width, height) = list.interest.tiles.pixel_size();
                ListStatus {
                    interest: &list.interest,
                    capacity: inventory.capacity(),
                    memory_mb: width * height * 4 * inventory.capacity() / (1024 * 1024),
                    counts: inventory.counts(),
                }
            })
            .collect();
        // Flattened fields serialize as a map, which would otherwise arrive in JS as a Map.
        Ok(status.serialize(&serde_wasm_bindgen::Serializer::new().serialize_maps_as_objects(true))?)
    }
}

/// Runs a decoded frame through the clean-up passes into its layer.
fn ingest(frame: DecodedFrame) {
    let list = &frame.list;
    let Some(layer) = list.inventory.borrow_mut().claim_layer(frame.ts) else { return };
    let size = list.interest.tiles.pixel_size();

    let mut frames = list.frames.borrow_mut();
    let set = frames.get_or_insert_with(|| {
        let flow_size = (list.grid_size.0 as u32, list.grid_size.1 as u32);
        FrameSet::new(&list.gpu, size, flow_size, list.inventory.borrow().capacity())
    });
    let view = set.layer_view(layer);
    list.gpu.smoother.run(&list.gpu.device, &list.gpu.queue, &frame.rgba, size, &list.tuning, &view);
}
