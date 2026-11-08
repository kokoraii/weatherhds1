//! Engine settings, read from `config.radar` in public/config.js. Everything is optional there.

use serde::Deserialize;
use std::collections::HashMap;

#[derive(Clone, Copy, Debug, Default, Deserialize, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum Backend {
    /// WebGPU where the browser offers it, otherwise WebGL2.
    #[default]
    Auto,
    Webgpu,
    Webgl,
}

#[derive(Clone, Copy, Debug, Default, Deserialize, PartialEq)]
#[serde(rename_all = "lowercase")]
pub enum Interpolation {
    /// Optical flow: echoes move between frames.
    #[default]
    Flow,
    /// Plain dissolve between frames.
    Crossfade,
    /// Hard cuts, like the old raster layers.
    None,
}

/// How one product's frames are cleaned up.
#[derive(Clone, Copy, Debug, Deserialize, PartialEq)]
#[serde(default, rename_all = "camelCase")]
pub struct Tuning {
    /// Median filter radius in source pixels: 0 (off), 1 (3x3) or 2 (5x5).
    pub median: u32,
    /// Gaussian sigma in source pixels; 0 is off.
    pub gaussian: f32,
    /// Unsharp mask strength, applied after the blur; 0 is off.
    pub sharpen: f32,
    /// Remaps blurred alpha with smoothstep(low, high) when drawing, which gives clean contours at any
    /// zoom. Only for products with on/off alpha; it would flatten satrad's translucent clouds.
    pub edge: Option<[f32; 2]>,
    /// How gradually frames hand over to each other: 0 is a dissolve between the nearest two, 1 is a
    /// smooth blend across four, which keeps uneven echo edges from popping.
    pub temporal: f32,
    /// A hard shadow: [offset in CSS pixels, opacity]. Drawn from this layer's outline.
    pub shadow: Option<[f32; 2]>,
}

impl Default for Tuning {
    fn default() -> Self {
        Self { median: 1, gaussian: 0.5, sharpen: 0.4, edge: Some([0.35, 0.65]), temporal: 1.0, shadow: None }
    }
}

#[derive(Clone, Debug, Deserialize)]
#[serde(default, rename_all = "camelCase")]
pub struct RadarConfig {
    pub backend: Backend,
    /// Render rate cap per surface. Motion is computed per drawn frame, so this is only a ceiling.
    pub fps: f64,
    /// Frames fetched at once.
    pub concurrency: usize,
    /// How often the product series is re-read for new frames.
    pub refresh_seconds: f64,
    pub interpolation: Interpolation,
    /// Longest side of the grid motion is estimated on.
    pub flow_resolution: u32,
    /// Per-product tuning. A product without an entry uses `default`.
    pub products: HashMap<String, Tuning>,
    /// What each on-screen radar draws. The engine only reads which products appear, to look them up.
    pub layers: HashMap<String, Vec<ConfiguredLayer>>,
}

#[derive(Clone, Debug, Deserialize)]
pub struct ConfiguredLayer {
    pub product: String,
}

impl Default for RadarConfig {
    fn default() -> Self {
        Self {
            backend: Backend::Auto,
            fps: 60.0,
            concurrency: 4,
            refresh_seconds: 120.0,
            interpolation: Interpolation::Flow,
            flow_resolution: 192,
            products: HashMap::new(),
            layers: HashMap::new(),
        }
    }
}

impl RadarConfig {
    /// Every product some radar draws, once each.
    pub fn layer_products(&self) -> Vec<String> {
        let mut products: Vec<String> =
            self.layers.values().flatten().map(|layer| layer.product.clone()).collect();
        products.sort();
        products.dedup();
        products
    }

    pub fn tuning(&self, product: &str) -> Tuning {
        self.products
            .get(product)
            .or_else(|| self.products.get("default"))
            .copied()
            .unwrap_or_default()
    }
}
