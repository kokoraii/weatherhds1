//! WeatherHDS radar engine. Fetches and inventories frames for each on-screen radar's interest list,
//! cleans them up, estimates motion between them, and draws the interpolated loop over the map.
//!
//! The modules declared first are pure logic and run under `cargo test` natively. The rest needs a
//! browser and only builds for wasm. public/javascript/radar.js is the only caller.

pub mod config;
pub mod flow;
pub mod geo;
pub mod interest;
pub mod inventory;
pub mod playback;
pub mod source;

#[cfg(target_arch = "wasm32")]
mod api;
#[cfg(target_arch = "wasm32")]
mod engine;
#[cfg(target_arch = "wasm32")]
mod gpu;
#[cfg(target_arch = "wasm32")]
mod net;
#[cfg(target_arch = "wasm32")]
mod surface;
