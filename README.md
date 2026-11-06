![WeatherHDS logo](https://github.com/meowraii/weatherhds1/blob/master/public/images/hdslogo_2024.png)

# WeatherHDS (Weather HTML Display System)

![WeatherHDS screenshots](https://github.com/meowraii/weatherhds1/blob/master/public/images/hds_screenshots.jpg)

## Present continuous weather data on display screens, with easy deployment and free and open source software.

WeatherHDS is a Go app that simplifies the presentation of modern and updated weather graphics on digital displays or other types of broadcasts. Designed with modularity and infinite customizability in mind, thanks to WeatherHDS's simplified underlying system completely built on standard web technologies.

## Building

Requires Go 1.26.3 or newer. To build, run `go build -o weatherhds` in the project root. The executable will just simply appear.

The music streamer plays Ogg Opus directly. Other common audio formats and video containers with audio tracks are decoded and transcoded to Opus by FFmpeg. The standard Go build uses the `ffmpeg` executable from PATH for these files. The Windows portable build uses the native FFmpeg libraries by default and requires the MSYS2 CLANG64 FFmpeg development libraries; use `scripts/build-windows.ps1 -MediaBackend builtin` to build without those libraries. FFmpeg cannot play files with no audio stream, and supported codecs depend on the installed FFmpeg build.

### Downloads

- Download stable versions from the [Releases page](https://github.com/meowraii/weatherhds1/releases).
- Download development nightly builds from [GitHub Actions](https://github.com/meowraii/weatherhds1/actions).

## Configuration

Main presentation, LDL locations, and system settings can be edited in `public/config.js`. Always restart the server for config changes!

The system rotates between locations once the last slide of the presentation is reached, it should loop back on the next location.

`slides.js` manages how the slides work. Currently, it switches between the HTML containers every 8 to 10 seconds.

The alerts page will show an intro slide whenever there are no alerts in effect.

## Features

- Built and designed for developers to adapt and improve the system.
- Completely customizable locations, no automatic geolocation (`public/config.js`)
- Switching between 4:3 (VGA), 16:9 (HDTV), 16:10 (Tablet), and 3:2 (NTSC DV) aspect ratios. Rendered at canvas sizes of 640x480, 854x480, 768x480 and 720x480 respectively, scaled to fit the viewport. This is controlled by `videoType` in the config.js file.
- LDL only mode. Displays a black screen with only the LDL, which you could superimpose onto programming with OBS Studio. This is controlled by `presentationType` in the config.js file.
- Switching between metric and imperial. (`public/config.js`)
- Switching between static and animated weather icons. (`public/config.js`)

## Config

All configuration is now located in `public/config.js`.

### Global Configuration

- `networkName`: Network name used on the station ID card.
- `affiliateName`: Affiliate name used on the station ID card.
- `channelNumber`: Channel ID/number used on the station ID card.
- `videoBackgrounds`: Enables or disables the video backgrounds on the current conditions slide.
- `currentConditionsGradient`: Enables a background gradient on the current conditions slide based on time relative to sunrise or sunset.
- `staticIcons`: If true, uses non-animated weather icons.
- `animatedIconFormat`: Format of the animated weather icons: `svg` (default, SMIL-animated, from `public/graphics/smil`) or `avif` (pre-rendered animated AVIF from `public/graphics/avif`, see below). Icons without a converted file fall back to their SVG.
- `ldlClock`: In LDL-only mode, shows or hides the clock and network logo on the LDL.
- `presentationConfig`: Object containing slide and display mode toggles.
  - `main`: Toggle main presentation.
  - `ldl`: Toggle LDL.
  - `ldlClock`: Toggle LDL clock.
  - `ldlBack`: Toggle LDL background.
  - `backgrounds`: Toggle backgrounds.
  - `repeatMain`: Loop main presentation.
  - `squareLogo`: Toggle square logo.
  - `bannerLogo`: Toggle banner logo.
  - `autorunOnStartup`: Auto-start presentation. Similar to toggling MAIN off.
- `videoType`: Aspect ratio mode. (options: `vga`, `ntsc`, `hdtv`, `tablet`).
- `systemTimeZone`: Time zone in [tz_database](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones) format.
- `tickerContent`: Text for the scrolling crawl on the LDL; empty string hides the crawl.
- `overrideBackgroundImage`: URL to a custom background image; skips background rotation.
- `backgroundSource`: `local` for bundled backgrounds, `online` for Bing image API backgrounds.
- `verboseLogging`: Enables or disables extra console logging.
- `refreshInterval`: How often to refresh the browser tab, in minutes.

### Animated AVIF icons

The animated SVG icons are drawn by the browser's SMIL engine on the main thread, which can be heavy on weak hardware. `scripts/svg2avif.py` pre-renders the SVGs in `public/graphics/smil` to animated, transparent AVIF files in `public/graphics/avif`:

```bash
pip install pillow playwright
python scripts/svg2avif.py
```

Then set `animatedIconFormat` to `"avif"` in `public/config.js` and restart. It renders each frame in your installed Edge or Chrome (or `playwright install chromium`) and works out every icon's loop length from its SVG timing, so loops are seamless. Useful flags: `--size` (default 160), `--fps` (default 20), `--quality`, `--force`, `--jobs`. Run `python scripts/svg2avif.py --help` for all of them.

The animated weather backgrounds in `public/images/avif` are decoded on the CPU frame by frame too. `python scripts/shrink_backgrounds.py` (needs ffmpeg with libsvtav1) re-encodes them in place to 540 px tall at 30 fps, which matches the panels they fill; it skips files already that small, so new backgrounds can be dropped in and the script re-run.

### Server Configuration

- `units`: Measurement units (e.g., `"m"` for metric, `"e"` for imperial).
- `dataProvider`: Where weather data comes from. `twc` uses weather.com and needs `TWC_API_KEY` in `.env`. `openmeteo` uses the free [Open-Meteo](https://open-meteo.com) APIs with no key, but has no weather alerts, no cloud ceiling, and no pollen outside Europe. Left empty, it uses `twc` when a key is set and `openmeteo` otherwise. Can also be set with the `DATA_PROVIDER` environment variable. The radar still loads from weather.com and needs the key either way.
- `webPort`: Port for the web server (default 3000).
- The admin panel listens separately on port `1005`.
- `cacheValidTime`: Time in seconds to cache weather data.
- `musicLoudness`: Loudness the background music is normalised to: `ebu-r128` (-23 LUFS, -1 dBTP), `atsc-a85` (-24 LKFS, -2 dBTP), `streaming` (-14 LUFS, -1 dBTP) or `off`. Each track is measured once (cached in `persistentCache/music-loudness.json`) and played with a constant gain plus a true-peak limiter when needed. It re-encodes every track through the `ffmpeg` executable, which must be in PATH; without it the music plays unprocessed. Can also be set with the `MUSIC_LOUDNESS` environment variable.

### URL parameters

*Example: `http://localhost:3000/?videoType=hdtv&ldl=true&background=true`*

- `videoType`: Sets the video type. Please see entry #3 of features.
- `main`: Enables/disables main presentation mode.
- `ldl`: Enables/disables LDL mode.
- `backgrounds`: Enables/disables backgrounds in the presentation.
- `repeatMain`: When true, loops the main presentation.
- `ldlClock`: Toggles the LDL clock independently of global `ldlClock` setting.

## LLM Assistance Disclaimer
WeatherHDS was programmed with the assistance of a Large Language Model (LLM). If you do not like this, then do not use the program... or do. Whatever you want, because I don't really care.

## Credits

- Credit to ScentedOrange for making the air quality and 7 day high and low pages. | https://github.com/ScentedOrangeDEV
- Credit to LeWolfYT for providing solutions to issues regarding the 7 day highs and the LDL progress bars | https://github.com/LeWolfYT
- Weather icons (MIT): https://github.com/basmilius/weather-icons | Copy of license included in weather icon directory
- Google Material icons (APACHE 2.0): https://fonts.google.com/icons | https://www.apache.org/licenses/LICENSE-2.0
- Weather videos (Pexels): https://www.pexels.com/license/
