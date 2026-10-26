import { requestWxData } from './data.js';
import { weatherIcons, locationConfig, serverConfig, displayUnits, config } from '../config.js';
import { RadarMap } from './radar.js';
import { weatherIconUrl } from './icons.js';
import { sidebarEnabled } from './layout.js';

const perf = config.performance ?? {};
const iconDir = config.staticIcons ? "static" : "smil";

const radarInstance = new RadarMap('sidebar-radar-map', {
    totalFrames:  Number(perf.sidebarRadarFrames ?? 24),
    frameDelay:   Number(perf.radarFrameDelay ?? 150),
    loopGap:      2000,
    maxLoops:     Number(perf.sidebarRadarLoops ?? Infinity),
    labelTextSize: 22,
});

const SUMMARY_HOLD_MS = 5000;
const SUMMARY_SLIDE_MS = 220;
const VALUE_SLIDE_MS = 200;

const conditionsLocation = document.getElementById('sidebar-conditions-location');
const conditionsTemp     = document.getElementById('sidebar-conditions-temp');
const conditionsPhrase   = document.getElementById('sidebar-conditions-phrase');
const conditionsIcon     = document.getElementById('sidebar-conditions-icon');

const summaryContainer = document.getElementById('sidebar-summary-lines');
const lineEl = (() => {
    const el = document.createElement('div');
    el.className = 'sidebar-summary-line';
    summaryContainer?.appendChild(el);
    return el;
})();

let latestLines   = [];
let summaryRunning = false;
let lineIdx       = 0;
let summaryCycleTimer = null;
let summaryTransitionTimer = null;
const slideTextTimers = new WeakMap();

function formatTemp(value) {
    const displayValue = value ?? '--';
    return `${displayValue}<span class="small-degrees">${endingTemp}</span>`;
}

function formatSummaryLine(label, value) {
    return `<span class="sidebar-summary-label">${label}</span><span class="sidebar-summary-value">${value}</span>`;
}

function formatSidebarLocationName(value) {
    return String(value ?? '').replace(/,\s*[A-Z]{2}(?=\s*$|,)/, '');
}

function setSlidingText(el, value) {
    if (!el) return;

    const next = value == null ? '' : String(value);
    const isReady = el.dataset.sidebarTextReady === 'true';

    if (!isReady || el.innerHTML === next) {
        el.innerHTML = next;
        el.dataset.sidebarTextReady = 'true';
        el.classList.remove('sidebar-slide-out', 'sidebar-slide-in');
        return;
    }

    const oldTimer = slideTextTimers.get(el);
    if (oldTimer) clearTimeout(oldTimer);

    el.classList.remove('sidebar-slide-in');
    el.classList.add('sidebar-slide-out');

    const timer = setTimeout(() => {
        el.innerHTML = next;
        el.classList.remove('sidebar-slide-out');
        el.classList.add('sidebar-slide-in');
        slideTextTimers.delete(el);

        requestAnimationFrame(() => {
            requestAnimationFrame(() => {
                el.classList.remove('sidebar-slide-in');
            });
        });
    }, VALUE_SLIDE_MS);

    slideTextTimers.set(el, timer);
}

function updateSidebarSummary(lines) {
    latestLines = lines.filter(line => typeof line === 'string' && line.length > 0);

    if (!summaryContainer || !lineEl) return;

    if (!latestLines.length) {
        clearTimeout(summaryCycleTimer);
        clearTimeout(summaryTransitionTimer);
        summaryRunning = false;
        lineEl.textContent = '';
        return;
    }

    if (!summaryRunning) {
        summaryRunning = true;
        lineIdx = 0;
        showSummaryLine(true);
    }
}

function scheduleNextSummaryLine() {
    clearTimeout(summaryCycleTimer);
    summaryCycleTimer = setTimeout(() => showSummaryLine(false), SUMMARY_HOLD_MS);
}

function showSummaryLine(immediate = false) {
    if (!latestLines.length) {
        summaryRunning = false;
        return;
    }

    if (lineIdx >= latestLines.length) lineIdx = 0;
    const target = latestLines[lineIdx] ?? '';
    lineIdx = (lineIdx + 1) % latestLines.length;

    clearTimeout(summaryCycleTimer);
    clearTimeout(summaryTransitionTimer);

    if (immediate) {
        lineEl.classList.remove('is-sliding-out', 'is-sliding-in');
        lineEl.innerHTML = target;
        scheduleNextSummaryLine();
        return;
    }

    lineEl.classList.add('is-sliding-out');
    summaryTransitionTimer = setTimeout(() => {
        lineEl.innerHTML = target;
        lineEl.classList.remove('is-sliding-out');
        lineEl.classList.add('is-sliding-in');

        requestAnimationFrame(() => {
            requestAnimationFrame(() => {
                lineEl.classList.remove('is-sliding-in');
            });
        });

        scheduleNextSummaryLine();
    }, SUMMARY_SLIDE_MS);
}

const REFRESH_MS = 10 * 60 * 1000;

const primaryGroup = locationConfig.localLocations.find(
    g => g.playlist === 'primary' && g.index === 0
);
const primaryLocation = primaryGroup?.locations?.[0]?.name ?? null;

const selectedDisplayUnits = displayUnits[serverConfig.units] || displayUnits['m'];
const { endingTemp, endingWind, endingDistance, endingPressure, endingCeiling, endingMeasurement } = selectedDisplayUnits;

const MONTHS = ['January','February','March','April','May','June','July','August','September','October','November','December'];

async function refreshSidebar() {
    if (!primaryLocation) return;

    const wxData = await requestWxData(primaryLocation, 'primary');

    const lat = wxData?.metadata?.localeData?.lat ?? null;
    const lon = wxData?.metadata?.localeData?.lon ?? null;

    if (lat && lon) {
        await radarInstance.init(lat, lon, 6, 'satrad');
        requestAnimationFrame(() => radarInstance.resize());
    }

    const current = wxData?.weather?.['v3-wx-observations-current'] ?? null;
    if (!current) return;

    const displayName = formatSidebarLocationName(primaryGroup.locations[0].displayName ?? primaryLocation);
    const iconEntry   = weatherIcons[current.iconCode];
    const iconFile    = iconEntry ? iconEntry[current.dayorNight === 'D' ? 0 : 1] : 'not-available.svg';

    setSlidingText(conditionsLocation, displayName);
    setSlidingText(conditionsTemp, formatTemp(current.temperature));
    setSlidingText(conditionsPhrase, current.wxPhraseLong);
    if (conditionsIcon)     conditionsIcon.src              = weatherIconUrl(iconDir, iconFile);

    const ceiling   = current.cloudCeiling == null ? 'Unlimited' : `${current.cloudCeiling}${endingCeiling}`;
    const month     = MONTHS[new Date().getMonth()];
    const precip    = current.precipMonth ?? current.precip24Hour;
    const precipStr = precip != null ? `${precip} ${endingMeasurement}` : '--';

    updateSidebarSummary([
        formatSummaryLine('Pressure', `${current.pressureAltimeter} ${endingPressure}`),
        formatSummaryLine('Wind', `${current.windDirectionCardinal} ${current.windSpeed} ${endingWind}`),
        formatSummaryLine('Dewpoint', formatTemp(current.temperatureDewPoint)),
        formatSummaryLine('Humidity', `${current.relativeHumidity}%`),
        formatSummaryLine('Visibility', `${current.visibility} ${endingDistance}`),
        formatSummaryLine('Ceiling', ceiling),
        formatSummaryLine('UV Index', current.uvIndex),
        formatSummaryLine(`${month} Precip.`, precipStr),
    ]);
}

// With the sidebar off, nothing here runs: no data refresh, no sidebar radar map.
if (sidebarEnabled) {
    requestAnimationFrame(() => requestAnimationFrame(refreshSidebar));
    setInterval(refreshSidebar, REFRESH_MS);
}
