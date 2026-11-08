//! Where a product's frames come from, worked out from its id:
//!   "twcRadarHcMosaic"     The Weather Company tile server (no prefix)
//!   "eccc:RADAR_1KM_RRAI"  an Environment Canada GeoMet layer
//!   "nws:conus_bref_qcd"   an NWS GeoServer layer
//!   "nws:KDTX"             one NEXRAD station's super-resolution base reflectivity
//!
//! The WMS services draw any rectangle on request, so a frame is one image rather than a grid of tiles.

use crate::geo::TileRect;

const GEOMET_URL: &str = "https://geo.weather.gc.ca/geomet";
const NWS_URL: &str = "https://opengeo.ncep.noaa.gov/geoserver";
/// Half the width of the web-mercator world, in metres.
const MERCATOR_HALF_WORLD: f64 = 20_037_508.342_789_244;
/// GeoMet's composite is 1 km, about one pixel at z7 at Canadian latitudes.
const COMPOSITE_NATIVE_ZOOM: u8 = 7;
/// Station gates are 250 m, but past z9 the extra tiles add more cost than visible detail.
const STATION_NATIVE_ZOOM: u8 = 9;

#[derive(Clone, Debug, PartialEq)]
pub enum Source {
    Twc,
    Wms(WmsLayer),
}

#[derive(Clone, Debug, PartialEq)]
pub struct WmsLayer {
    pub base_url: String,
    pub layer: String,
    pub native_zoom: u8,
}

impl Source {
    pub fn parse(product: &str) -> Result<Self, String> {
        let Some((service, name)) = product.split_once(':') else { return Ok(Self::Twc) };
        match service {
            "eccc" if is_station_id(name, 5) => Err(format!(
                "{product}: GeoMet has no single-station layers; use eccc:RADAR_1KM_RRAI or eccc:RADAR_1KM_RSNO"
            )),
            "eccc" => Ok(Self::Wms(WmsLayer {
                base_url: GEOMET_URL.to_owned(),
                layer: name.to_owned(),
                native_zoom: COMPOSITE_NATIVE_ZOOM,
            })),
            "nws" if is_station_id(name, 4) => {
                let station = name.to_ascii_lowercase();
                Ok(Self::Wms(WmsLayer {
                    base_url: format!("{NWS_URL}/{station}/{station}_sr_bref/ows"),
                    layer: format!("{station}_sr_bref"),
                    native_zoom: STATION_NATIVE_ZOOM,
                }))
            }
            "nws" => {
                let (workspace, _) = name
                    .split_once('_')
                    .ok_or_else(|| format!("{product}: expected a station like KDTX or a layer like conus_bref_qcd"))?;
                Ok(Self::Wms(WmsLayer {
                    base_url: format!("{NWS_URL}/{workspace}/{name}/ows"),
                    layer: name.to_owned(),
                    native_zoom: COMPOSITE_NATIVE_ZOOM,
                }))
            }
            _ => Err(format!("{product}: unknown radar source {service}")),
        }
    }
}

/// Station ids are all capitals, which is what separates them from layer names.
fn is_station_id(name: &str, length: usize) -> bool {
    name.len() == length && name.bytes().all(|byte| byte.is_ascii_uppercase())
}

impl WmsLayer {
    pub fn capabilities_url(&self) -> String {
        // GeoMet's full document is 40 MB; LAYERS trims it to this one. GeoServer ignores the parameter.
        format!("{}?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetCapabilities&LAYERS={}", self.base_url, self.layer)
    }

    /// One frame covering `tiles`, at the same pixel size the tiles would have made.
    pub fn frame_url(&self, ts: i64, tiles: TileRect) -> String {
        let rect = tiles.world_rect();
        let metres_x = |x: f64| (x * 2.0 - 1.0) * MERCATOR_HALF_WORLD;
        let metres_y = |y: f64| (1.0 - y * 2.0) * MERCATOR_HALF_WORLD;
        let (width, height) = tiles.pixel_size();
        format!(
            "{}?SERVICE=WMS&VERSION=1.3.0&REQUEST=GetMap&LAYERS={}&STYLES=&CRS=EPSG:3857&BBOX={:.2},{:.2},{:.2},{:.2}\
             &WIDTH={width}&HEIGHT={height}&FORMAT=image/png&TRANSPARENT=TRUE&TIME={}",
            self.base_url,
            self.layer,
            metres_x(rect.min_x),
            metres_y(rect.min_y + rect.height),
            metres_x(rect.min_x + rect.width),
            metres_y(rect.min_y),
            format_time(ts),
        )
    }

    /// The layer's frame times from a capabilities document, newest first like the TWC series. Empty if
    /// the document doesn't list the layer.
    pub fn parse_times(&self, capabilities: &str) -> Vec<i64> {
        let Some(layer_at) = capabilities.find(&format!("<Name>{}</Name>", self.layer)) else { return Vec::new() };
        let after_layer = &capabilities[layer_at..];
        let Some(dimension_at) = after_layer.find("<Dimension name=\"time\"") else { return Vec::new() };
        let dimension = &after_layer[dimension_at..];
        let Some(open_end) = dimension.find('>') else { return Vec::new() };
        let value = &dimension[open_end + 1..];
        let Some(close) = value.find('<') else { return Vec::new() };

        let mut times: Vec<i64> = value[..close].trim().split(',').flat_map(expand_time_item).collect();
        times.sort_unstable_by(|a, b| b.cmp(a));
        times.dedup();
        times
    }
}

/// One entry of a WMS time dimension: a single time, or start/end/period.
fn expand_time_item(item: &str) -> Vec<i64> {
    let parts: Vec<&str> = item.trim().split('/').collect();
    match parts.as_slice() {
        [single] => parse_time(single).into_iter().collect(),
        [start, end, period] => match (parse_time(start), parse_time(end), parse_period(period)) {
            (Some(start), Some(end), Some(period)) if period > 0 => {
                (0..).map(|i| start + i * period).take_while(|&ts| ts <= end).collect()
            }
            _ => Vec::new(),
        },
        _ => Vec::new(),
    }
}

/// Seconds since the epoch from "2026-11-06T14:15:58.000Z" or "2026-11-06T14:15:58Z".
fn parse_time(text: &str) -> Option<i64> {
    let text = text.strip_suffix('Z')?;
    let (date, time) = text.split_once('T')?;
    let mut date = date.split('-').map(str::parse::<i64>);
    let (year, month, day) = (date.next()?.ok()?, date.next()?.ok()?, date.next()?.ok()?);
    let time = time.split('.').next()?;
    let mut time = time.split(':').map(str::parse::<i64>);
    let (hour, minute, second) = (time.next()?.ok()?, time.next()?.ok()?, time.next()?.ok()?);
    Some(days_from_civil(year, month, day) * 86_400 + hour * 3_600 + minute * 60 + second)
}

/// Seconds from an ISO 8601 period like "PT6M" or "PT1H30M".
fn parse_period(text: &str) -> Option<i64> {
    let mut number = 0i64;
    let mut seconds = 0i64;
    for character in text.strip_prefix("PT")?.chars() {
        match character {
            '0'..='9' => number = number * 10 + i64::from(character.to_digit(10)?),
            'H' | 'M' | 'S' => {
                seconds += number * match character { 'H' => 3_600, 'M' => 60, _ => 1 };
                number = 0;
            }
            _ => return None,
        }
    }
    Some(seconds)
}

fn format_time(ts: i64) -> String {
    let (days, seconds) = (ts.div_euclid(86_400), ts.rem_euclid(86_400));
    let (year, month, day) = civil_from_days(days);
    format!("{year:04}-{month:02}-{day:02}T{:02}:{:02}:{:02}Z", seconds / 3_600, seconds / 60 % 60, seconds % 60)
}

/// Days since 1970-01-01 for a proleptic Gregorian date (Howard Hinnant's algorithm).
fn days_from_civil(year: i64, month: i64, day: i64) -> i64 {
    let year = if month <= 2 { year - 1 } else { year };
    let era = year.div_euclid(400);
    let year_of_era = year - era * 400;
    let day_of_year = (153 * (month + if month > 2 { -3 } else { 9 }) + 2) / 5 + day - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    era * 146_097 + day_of_era - 719_468
}

/// The inverse of `days_from_civil`.
fn civil_from_days(days: i64) -> (i64, i64, i64) {
    let days = days + 719_468;
    let era = days.div_euclid(146_097);
    let day_of_era = days - era * 146_097;
    let year_of_era = (day_of_era - day_of_era / 1_460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let shifted_month = (5 * day_of_year + 2) / 153;
    let day = day_of_year - (153 * shifted_month + 2) / 5 + 1;
    let month = if shifted_month < 10 { shifted_month + 3 } else { shifted_month - 9 };
    (year_of_era + era * 400 + i64::from(month <= 2), month, day)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn wms(product: &str) -> WmsLayer {
        match Source::parse(product).unwrap() {
            Source::Wms(layer) => layer,
            Source::Twc => panic!("{product} should be a WMS layer"),
        }
    }

    #[test]
    fn product_ids_pick_their_source() {
        assert_eq!(Source::parse("twcRadarHcMosaic").unwrap(), Source::Twc);
        assert_eq!(wms("eccc:RADAR_1KM_RRAI").layer, "RADAR_1KM_RRAI");
        assert_eq!(wms("nws:conus_bref_qcd").base_url, format!("{NWS_URL}/conus/conus_bref_qcd/ows"));

        let station = wms("nws:KDTX");
        assert_eq!(station.layer, "kdtx_sr_bref");
        assert_eq!(station.base_url, format!("{NWS_URL}/kdtx/kdtx_sr_bref/ows"));
        assert_eq!(station.native_zoom, STATION_NATIVE_ZOOM);
    }

    #[test]
    fn bad_product_ids_explain_themselves() {
        assert!(Source::parse("eccc:CASRA").unwrap_err().contains("no single-station"));
        assert!(Source::parse("nws:nope").is_err());
        assert!(Source::parse("bom:IDR").unwrap_err().contains("unknown radar source"));
    }

    #[test]
    fn times_survive_a_round_trip() {
        let ts = parse_time("2026-11-06T14:15:58.000Z").unwrap();
        assert_eq!(format_time(ts), "2026-11-06T14:15:58Z");
        assert_eq!(parse_time("1970-01-01T00:00:00Z"), Some(0));
        assert_eq!(parse_time("2000-03-01T00:00:00Z"), Some(951_868_800));
        assert_eq!(parse_time("garbage"), None);
    }

    #[test]
    fn reads_geomets_start_end_period_form() {
        let document = r#"<Name>Other</Name><Dimension name="time" units="ISO8601">2020-01-01T00:00:00Z</Dimension>
            <Name>RADAR_1KM_RRAI</Name><Title>t</Title>
            <Dimension name="time" units="ISO8601" default="x" nearestValue="0">2026-11-06T13:12:00Z/2026-11-06T13:24:00Z/PT6M</Dimension>"#;
        let times = wms("eccc:RADAR_1KM_RRAI").parse_times(document);
        let start = parse_time("2026-11-06T13:12:00Z").unwrap();
        assert_eq!(times, vec![start + 720, start + 360, start]);
    }

    #[test]
    fn reads_geoservers_list_form_newest_first() {
        let document = r#"<Name>conus_bref_qcd</Name>
            <Dimension name="time" default="x" units="ISO8601" nearestValue="1">2026-11-06T14:15:58.000Z,2026-11-06T14:17:51.000Z</Dimension>"#;
        let times = wms("nws:conus_bref_qcd").parse_times(document);
        assert_eq!(times, vec![parse_time("2026-11-06T14:17:51Z").unwrap(), parse_time("2026-11-06T14:15:58Z").unwrap()]);
    }

    #[test]
    fn a_missing_layer_has_no_times() {
        assert!(wms("nws:KDTX").parse_times("<Name>something_else</Name>").is_empty());
    }

    #[test]
    fn frame_requests_cover_the_tiles_exactly() {
        let tiles = TileRect { z: 1, x0: 0, y0: 0, x1: 1, y1: 1 }; // the north-west quarter of the world
        let url = wms("eccc:RADAR_1KM_RRAI").frame_url(0, tiles);
        assert!(url.contains("BBOX=-20037508.34,0.00,0.00,20037508.34"), "{url}");
        assert!(url.contains("WIDTH=256&HEIGHT=256"), "{url}");
        assert!(url.contains("TIME=1970-01-01T00:00:00Z"), "{url}");
    }
}
