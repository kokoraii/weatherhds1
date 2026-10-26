import { brand, config, locationConfig, weatherIcons } from "../config.js";
import { requestWxData } from "./data.js";
import { RadarMap } from "./radar.js";
import { weatherIconUrl } from "./icons.js";
import { topbarContent } from "./layout.js";
import { TopbarStatus, startNotchClock } from "./topbar_status.js";

// Top bar content (presentationConfig.topbar; layout.js decides). Puts the network logo and a status line
// (date / song / conditions) in the top bar, moves the affiliate logo(s) up beside them, and turns the LDL
// brand rail into a 3 hour radar loop with the clock in a notch at its bottom right.
// Nothing here runs when the top bar is off or blank. ldl.js keeps cycling the moved affiliate logos.

const perf = config.performance ?? {};
const iconDir = config.staticIcons ? "static" : "smil";
const REFRESH_MS = 10 * 60 * 1000;

if (topbarContent) start();

function start() {
    document.getElementById("topbar-network-logo").src = brand.networkLogo;
    document.querySelector(".topbar-right").append(document.querySelector(".provider-logo-container"));
    startNotchClock(document.getElementById("ldl-rail-radar-clock"));
    const status = new TopbarStatus(document.getElementById("topbar-status"));

    const primaryGroup = locationConfig.localLocations.find(g => g.playlist === "primary" && g.index === 0);
    const primaryLocation = primaryGroup?.locations?.[0]?.name ?? null;
    if (!primaryLocation) return;
    const displayName = String(primaryGroup.locations[0].displayName ?? primaryLocation).replace(/,\s*[A-Z]{2}(?=\s*$|,)/, "");

    const radar = new RadarMap("ldl-rail-radar-map", {
        totalFrames: Number(perf.sidebarRadarFrames ?? 24),
        frameDelay: Number(perf.radarFrameDelay ?? 150),
        loopGap: 2000,
        maxLoops: Number(perf.sidebarRadarLoops ?? Infinity),
        labelTextSize: 22,
    });

    const refresh = async () => {
        const wx = await requestWxData(primaryLocation, "primary");
        const current = wx?.weather?.["v3-wx-observations-current"];
        if (current) {
            const iconEntry = weatherIcons[current.iconCode];
            const iconFile = iconEntry ? iconEntry[current.dayorNight === "D" ? 0 : 1] : "not-available.svg";
            status.setConditions(displayName, weatherIconUrl(iconDir, iconFile), current.temperature, current.wxPhraseLong);
        }
        const { lat, lon } = wx?.metadata?.localeData ?? {};
        if (lat && lon) await radar.init(lat, lon, 6, "satrad");
    };

    requestAnimationFrame(() => requestAnimationFrame(refresh));
    setInterval(refresh, REFRESH_MS);
}
