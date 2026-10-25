<img src="https://github.com/meowraii/weatherhds1/blob/master/public/images/hdslogo_2024.png">
<body>
  <h1>WeatherHDS (Weather HTML Display System)</h1>

  <img src="https://github.com/meowraii/weatherhds1/blob/master/public/images/hds_screenshots.jpg">

  <h2>Present continuous weather data on display screens, with easy deployment and free and open source software.</h2>
  <p>WeatherHDS is a Go app that simplifies the presentation of modern and updated weather graphics on digital displays or other types of broadcasts. Designed with modularity and infinite customizability in mind, thanks to WeatherHDS's simplified underlying system completely built on standard web technologies.</p>

  <h1>Building</h1>
  <p>Requires Go 1.26.3 or newer. To build, run <code>go build -o weatherhds</code> in the project root. The executable will just simply appear.</p>
  <p>The music streamer plays Ogg Opus directly. Other common audio formats and video containers with audio tracks are decoded and transcoded to Opus by FFmpeg. The standard Go build uses the <code>ffmpeg</code> executable from PATH for these files. The Windows portable build uses the native FFmpeg libraries by default and requires the MSYS2 CLANG64 FFmpeg development libraries; use <code>scripts/build-windows.ps1 -MediaBackend builtin</code> to build without those libraries. FFmpeg cannot play files with no audio stream, and supported codecs depend on the installed FFmpeg build.</p>
  <h3>Downloads</h3>
  <ul>
    <li>Download stable versions from the <a href="https://github.com/meowraii/weatherhds1/releases">Releases page</a>.</li>
    <li>Download development nightly builds from <a href="https://github.com/meowraii/weatherhds1/actions">GitHub Actions</a>.</li>
  </ul>

  <h1>Configuration</h1>
  <p>Main presentation, LDL locations, and system settings can be edited in <code>public/config.js</code>. Always restart the server for config changes!</p>
  <p>The system rotates between locations once the last slide of the presentation is reached, it should loop back on the next location.</p>
  <p>slides.js manages how the slides work. Currently, it switches between the HTML containers every 8 to 10 seconds.</p>
  <p>The alerts page will show an intro slide whenever there are no alerts in effect.</p>

  <h1>Features</h1>
  <ul>
    <li>Built and designed for developers to adapt and improve the system.</li>
    <li>Completely customizable locations, no automatic geolocation (public/config.js)</li>
    <li>Switching between 4:3 (VGA), 16:9 (HDTV), 16:10 (Tablet), and 3:2 (NTSC DV) aspect ratios. Rendered at canvas sizes of 640x480, 854x480, 768x480 and 720x480 respectively, scaled to fit the viewport. This is controlled by `videoType` in the config.js file.</li>
    <li>LDL only mode. Displays a black screen with only the LDL, which you could superimpose onto programming with OBS Studio. This is controlled by `presentationType` in the config.js file.</li>
    <li>Switching between metric and imperial. (public/config.js)</li>
    <li>Switching between static and animated weather icons. (public/config.js)</li>
  </ul>

<h1>Config</h1>
<p>All configuration is now located in <code>public/config.js</code>.</p>

<h3>Global Configuration</h3>
<ul>
  <li><code>networkName</code>: Network name used on the station ID card.</li>
  <li><code>affiliateName</code>: Affiliate name used on the station ID card.</li>
  <li><code>channelNumber</code>: Channel ID/number used on the station ID card.</li>
  <li><code>videoBackgrounds</code>: Enables or disables the video backgrounds on the current conditions slide.</li>
  <li><code>currentConditionsGradient</code>: Enables a background gradient on the current conditions slide based on time relative to sunrise or sunset.</li>
  <li><code>staticIcons</code>: If true, uses non-animated weather icons.</li>
  <li><code>animatedIconFormat</code>: Format of the animated weather icons: <code>svg</code> (default, SMIL-animated, from <code>public/graphics/smil</code>) or <code>avif</code> (pre-rendered animated AVIF from <code>public/graphics/avif</code>, see below). Icons without a converted file fall back to their SVG.</li>
  <li><code>ldlClock</code>: In LDL-only mode, shows or hides the clock and network logo on the LDL.</li>
  <li><code>presentationConfig</code>: Object containing slide and display mode toggles.
    <ul>
      <li><code>main</code>: Toggle main presentation.</li>
      <li><code>ldl</code>: Toggle LDL.</li>
      <li><code>ldlClock</code>: Toggle LDL clock.</li>
      <li><code>ldlBack</code>: Toggle LDL background.</li>
      <li><code>backgrounds</code>: Toggle backgrounds.</li>
      <li><code>repeatMain</code>: Loop main presentation.</li>
      <li><code>squareLogo</code>: Toggle square logo.</li>
      <li><code>bannerLogo</code>: Toggle banner logo.</li>
      <li><code>autorunOnStartup</code>: Auto-start presentation. Similar to toggling MAIN off.</li>
    </ul>
  </li>
  <li><code>videoType</code>: Aspect ratio mode. (options: <code>vga</code>, <code>ntsc</code>, <code>hdtv</code>, <code>tablet</code>).</li>
  <li><code>systemTimeZone</code>: Time zone in <a href="https://en.wikipedia.org/wiki/List_of_tz_database_time_zones">tz_database</a> format.</li>
  <li><code>tickerContent</code>: Text for the scrolling crawl on the LDL; empty string hides the crawl.</li>
  <li><code>overrideBackgroundImage</code>: URL to a custom background image; skips background rotation.</li>
  <li><code>backgroundSource</code>: <code>local</code> for bundled backgrounds, <code>online</code> for Bing image API backgrounds.</li>
  <li><code>verboseLogging</code>: Enables or disables extra console logging.</li>
  <li><code>refreshInterval</code>: How often to refresh the browser tab, in minutes.</li>
</ul>

<h3>Animated AVIF icons</h3>
<p>The animated SVG icons are drawn by the browser's SMIL engine on the main thread, which can be heavy on weak hardware. <code>scripts/svg2avif.py</code> pre-renders the SVGs in <code>public/graphics/smil</code> to animated, transparent AVIF files in <code>public/graphics/avif</code>:</p>
<pre>pip install pillow playwright
python scripts/svg2avif.py</pre>
<p>Then set <code>animatedIconFormat</code> to <code>"avif"</code> in <code>public/config.js</code> and restart. It renders each frame in your installed Edge or Chrome (or <code>playwright install chromium</code>) and works out every icon's loop length from its SVG timing, so loops are seamless. Useful flags: <code>--size</code> (default 256), <code>--fps</code> (default 30), <code>--quality</code>, <code>--force</code>, <code>--jobs</code>. Run <code>python scripts/svg2avif.py --help</code> for all of them.</p>

<h3>Server Configuration</h3>
<ul>
  <li><code>units</code>: Measurement units (e.g., "m" for metric, "e" for imperial).</li>
  <li><code>webPort</code>: Port for the web server (default 3000).</li>
  <li>The admin panel listens separately on port <code>1005</code>.</li>
  <li><code>cacheValidTime</code>: Time in seconds to cache weather data.</li>
</ul>

<h3>URL parameters </h3>
<i>Example: <code>http://localhost:3000/?videoType=hdtv&ldl=true&background=true</code></i>
<ul>
  <li><code>videoType</code>: Sets the video type. Please see entry #3 of features.</li>
  <li><code>main</code>: Enables/disables main presentation mode.</li>
  <li><code>ldl</code>: Enables/disables LDL mode.</li>
  <li><code>backgrounds</code>: Enables/disables backgrounds in the presentation.</li>
  <li><code>repeatMain</code>: When true, loops the main presentation.</li>
  <li><code>ldlClock</code>: Toggles the LDL clock independently of global <code>ldlClock</code> setting.</li>
</ul>
<p></p>
  <h1>Credits</h1>
    <p>Credit to ScentedOrange for making the air quality and 7 day high and low pages. | https://github.com/ScentedOrangeDEV</p>
    <p>Credit to LeWolfYT for providing solutions to issues regarding the 7 day highs and the LDL progress bars | https://github.com/LeWolfYT</p>
  <p>Weather icons (MIT): https://github.com/basmilius/weather-icons | Copy of license included in weather icon directory</p>
  <p>Google Material icons (APACHE 2.0): https://fonts.google.com/icons | https://www.apache.org/licenses/LICENSE-2.0</p>
  <p>weather videos (Pexels): https://www.pexels.com/license/</p>
</body>
