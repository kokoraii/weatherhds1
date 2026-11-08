//! Talking to the radar data sources: what frames exist, and assembling a frame's pixels.

use crate::geo::{SOURCE_TILE_PX, TileRect};
use crate::source::{Source, WmsLayer};
use futures::future::join_all;
use serde::Deserialize;
use std::collections::HashMap;
use wasm_bindgen::{JsCast, JsValue};
use wasm_bindgen_futures::JsFuture;
use web_sys::{ImageBitmap, OffscreenCanvas, OffscreenCanvasRenderingContext2d, Response};

const TWC_SERIES_URL: &str = "https://api.weather.com/v3/TileServer/series/productSet/PPAcore";
const TWC_TILE_URL: &str = "https://api.weather.com/v3/TileServer/tile";

#[derive(Clone, Debug)]
pub struct Product {
    pub native_zoom: u8,
    /// Available frame timestamps, newest first.
    pub timestamps: Vec<i64>,
}

/// What's currently available, by product id. A source that's down just leaves its products out.
pub type Catalog = HashMap<String, Product>;

#[derive(Debug, Deserialize)]
struct TwcProductSet {
    #[serde(rename = "seriesInfo")]
    products: HashMap<String, TwcProduct>,
}

#[derive(Debug, Deserialize)]
struct TwcProduct {
    #[serde(rename = "nativeZoom", default)]
    native_zoom: u8,
    #[serde(default)]
    series: Vec<TwcSeriesEntry>,
}

#[derive(Debug, Deserialize)]
struct TwcSeriesEntry {
    #[serde(default)]
    ts: i64,
}

async fn fetch_ok(url: &str) -> Result<Response, JsValue> {
    let window = web_sys::window().ok_or("no window")?;
    let response: Response = JsFuture::from(window.fetch_with_str(url)).await?.dyn_into()?;
    if response.ok() {
        Ok(response)
    } else {
        // A TWC URL carries the API key, so it stays out of the error.
        Err(JsValue::from_str(&format!("HTTP {}", response.status())))
    }
}

/// Looks up every configured product. Each TWC product publishes its own series: the mosaics lag the
/// "radar" series by a few minutes, so its newest timestamps would be "Future tile" (HTTP 400) for them.
pub async fn fetch_catalog(api_key: &str, products: &[String]) -> Catalog {
    let mut catalog = Catalog::new();
    let mut wms_lookups = Vec::new();
    let mut wants_twc = false;
    for product in products {
        match Source::parse(product) {
            Ok(Source::Twc) => wants_twc = true,
            Ok(Source::Wms(layer)) => wms_lookups.push(fetch_wms_product(product, layer)),
            Err(reason) => web_sys::console::error_1(&format!("[radar] {reason}").into()),
        }
    }

    for found in join_all(wms_lookups).await.into_iter().flatten() {
        catalog.insert(found.0, found.1);
    }
    if wants_twc {
        match fetch_twc_products(api_key).await {
            Ok(twc) => {
                for product in products {
                    if let Some(found) = twc.products.get(product) {
                        let timestamps = found.series.iter().map(|entry| entry.ts).collect();
                        catalog.insert(product.clone(), Product { native_zoom: found.native_zoom, timestamps });
                    }
                }
            }
            Err(error) => web_sys::console::warn_2(&"[radar] TWC series lookup failed:".into(), &error),
        }
    }
    catalog
}

async fn fetch_twc_products(api_key: &str) -> Result<TwcProductSet, JsValue> {
    let response = fetch_ok(&format!("{TWC_SERIES_URL}?apiKey={api_key}")).await?;
    let json = JsFuture::from(response.json()?).await?;
    serde_wasm_bindgen::from_value(json).map_err(Into::into)
}

async fn fetch_wms_product(product: &str, layer: WmsLayer) -> Option<(String, Product)> {
    let lookup = async {
        let response = fetch_ok(&layer.capabilities_url()).await?;
        let text = JsFuture::from(response.text()?).await?;
        Ok::<String, JsValue>(text.as_string().unwrap_or_default())
    };
    match lookup.await {
        Ok(document) => {
            let timestamps = layer.parse_times(&document);
            if timestamps.is_empty() {
                web_sys::console::warn_1(&format!("[radar] {product} lists no frames").into());
                return None;
            }
            Some((product.to_owned(), Product { native_zoom: layer.native_zoom, timestamps }))
        }
        Err(error) => {
            web_sys::console::warn_2(&format!("[radar] {product} lookup failed:").into(), &error);
            None
        }
    }
}

async fn fetch_bitmap(url: String) -> Result<ImageBitmap, JsValue> {
    let response = fetch_ok(&url).await?;
    let blob: web_sys::Blob = JsFuture::from(response.blob()?).await?.dyn_into()?;
    let window = web_sys::window().ok_or("no window")?;
    JsFuture::from(window.create_image_bitmap_with_blob(&blob)?).await?.dyn_into()
}

/// Downloads one frame and returns it as unpremultiplied RGBA rows.
pub async fn fetch_frame(api_key: &str, product: &str, ts: i64, tiles: TileRect) -> Result<Vec<u8>, JsValue> {
    let source = Source::parse(product).map_err(|reason| JsValue::from_str(&reason))?;
    let placed = match source {
        Source::Twc => {
            let downloads = tiles.tiles().map(|(x, y)| {
                fetch_bitmap(format!("{TWC_TILE_URL}/{product}?ts={ts}&xyz={x}:{y}:{z}&apiKey={api_key}", z = tiles.z))
            });
            let bitmaps = join_all(downloads).await;
            let origins = tiles.tiles().map(|(x, y)| ((x - tiles.x0) * SOURCE_TILE_PX, (y - tiles.y0) * SOURCE_TILE_PX));
            origins.zip(bitmaps).map(|(origin, bitmap)| Ok((origin, bitmap?))).collect::<Result<Vec<_>, JsValue>>()?
        }
        Source::Wms(layer) => vec![((0, 0), fetch_bitmap(layer.frame_url(ts, tiles)).await?)],
    };

    let (width, height) = tiles.pixel_size();
    let canvas = OffscreenCanvas::new(width, height)?;
    let options = js_sys::Object::new();
    js_sys::Reflect::set(&options, &"willReadFrequently".into(), &JsValue::TRUE)?;
    let context: OffscreenCanvasRenderingContext2d = canvas
        .get_context_with_context_options("2d", &options)?
        .ok_or("no 2d context")?
        .dyn_into()?;

    for ((left, top), bitmap) in placed {
        context.draw_image_with_image_bitmap(&bitmap, f64::from(left), f64::from(top))?;
        bitmap.close();
    }
    Ok(context.get_image_data(0.0, 0.0, f64::from(width), f64::from(height))?.data().0)
}
