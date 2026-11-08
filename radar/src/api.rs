//! The JavaScript-facing surface of the engine. See public/javascript/radar.js for how it is driven.

use crate::config::RadarConfig;
use crate::engine::Engine;
use crate::gpu::Gpu;
use crate::interest::StackRequest;
use crate::surface::{Camera, Surface, SurfaceOptions};
use std::rc::Rc;
use wasm_bindgen::prelude::*;
use wasm_bindgen_futures::future_to_promise;
use web_sys::HtmlCanvasElement;

#[wasm_bindgen]
pub struct RadarEngine {
    inner: Rc<Engine>,
}

#[wasm_bindgen]
impl RadarEngine {
    /// Looks up the configured products. `config` is `config.radar` from config.js.
    pub async fn create(config: JsValue, api_key: String) -> Result<RadarEngine, JsValue> {
        console_error_panic_hook::set_once();
        let config: RadarConfig = if config.is_undefined() {
            RadarConfig::default()
        } else {
            serde_wasm_bindgen::from_value(config)?
        };
        Ok(Self { inner: Engine::new(config, api_key).await })
    }

    /// Sets up a canvas's GPU device. Resolves to a RadarSurface. The canvas needn't be in the page yet.
    #[wasm_bindgen(js_name = createSurface)]
    pub fn create_surface(&self, name: String, canvas: HtmlCanvasElement, options: JsValue) -> js_sys::Promise {
        let engine = self.inner.clone();
        future_to_promise(async move {
            let options: SurfaceOptions = serde_wasm_bindgen::from_value(options)?;
            let gpu = Rc::new(Gpu::new(canvas.clone(), engine.config.backend).await?);
            let surface = Surface::new(name, gpu, canvas, options, engine.config.interpolation, engine.config.fps);
            Ok(RadarSurface { inner: Rc::new(surface), engine }.into())
        })
    }

    /// One slice of background work. Call once per animation frame.
    pub fn pump(&self, now_ms: f64) {
        self.inner.pump(now_ms);
    }

    /// Every interest list and how far along its frames are.
    pub fn inventory(&self) -> Result<JsValue, JsValue> {
        self.inner.inventory()
    }
}

#[wasm_bindgen]
pub struct RadarSurface {
    inner: Rc<Surface>,
    engine: Rc<Engine>,
}

#[wasm_bindgen]
impl RadarSurface {
    /// Registers interest in a stack { layers: [{ product, opacity }], frames, lat, lon, zoom, width, height }
    /// and starts fetching its frames, ahead of other downloads if `urgent`. Layers go bottom first, and
    /// `frames` is the top layer's count. Safe to call while the radar is off screen.
    pub fn want(&self, request: JsValue, urgent: bool) -> Result<(), JsValue> {
        let request: StackRequest = serde_wasm_bindgen::from_value(request)?;
        let layers = self.engine.want_stack(&self.inner.name, &self.inner.gpu, &request);
        if urgent {
            self.engine.prioritize(&layers);
        }
        Ok(())
    }

    /// Plays a stack from its oldest frame, registering it first if needed.
    pub fn show(&self, request: JsValue) -> Result<(), JsValue> {
        let request: StackRequest = serde_wasm_bindgen::from_value(request)?;
        let layers = self.engine.want_stack(&self.inner.name, &self.inner.gpu, &request);
        self.engine.prioritize(&layers);
        self.inner.show(layers);
        Ok(())
    }

    /// Draws the current moment for this camera. Returns the on-screen frame's timestamp in seconds.
    pub fn render(
        &self,
        now_ms: f64,
        lat: f64,
        lon: f64,
        zoom: f64,
        width: f64,
        height: f64,
        pixel_ratio: f64,
    ) -> Option<f64> {
        self.inner.render(now_ms, Camera { lat, lon, zoom, width, height, pixel_ratio })
    }

    /// The graphics backend in use, for the startup log.
    #[wasm_bindgen(getter)]
    pub fn backend(&self) -> String {
        self.inner.gpu.backend_name.clone()
    }
}
