import { config, locationConfig, versionID, serverConfig, bumperBackgroundsRandom, brand } from "../config.js";
import { appendDatatoMain, animateIntraday, daypartNames, getShortTermPeriodCount, renderShortTermPeriod, runShortTermProduct, stopShortTermProduct } from "./weather.js";
import { serverHealth } from "./data.js";
import { runRegionalPlayback } from "./national.js";
import { resizeRadar } from "./radar.js";
import { getVocallocalQueueForSlide, getVocallocalTotalDurationMsForSlide, isVocallocalEnabled } from "./vocallocal.js";
import { startMarquee, stopMarquee } from "./marquee.js";

const playlistSettings = {
    defaultAnimationIn: `mainPresentationSlideIn 500ms ease-in-out`,
    defaultAnimationOut: `mainPresentationSlideOut 500ms ease-in-out forwards`,
};

const iconMappings = [
    // { id: "current", icon: "/graphics/ux/thermometer-snowflake.svg" }, we have a function for current conditions. no need.
    { id: "forecast-intraday", icon: "/graphics/ux/calendar-clock.svg" },
    { id: "forecast-shortterm", icon: "/graphics/ux/calendar-1.svg" },
    { id: "forecast-extended", icon: "/graphics/ux/calendar-1.svg" },
    { id: "7day-graph", icon: "/graphics/ux/calendar-1.svg" },
    { id: "airquality", icon: "/graphics/ux/leaf.svg" },
    { id: "radar", icon: "/graphics/ux/radar.svg" },
];


const preferredPlaylist = {
    mainPlaylist: [
        {
            htmlID: "current",
            title: "Current Conditions",
            duration: 12000,
            dynamicFunction: runMainCurrentSlide,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        },
        {
            htmlID: "radar",
            title: "3 Hour Radar",
            duration: 16000,
            dynamicFunction: runRadarSlide,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        },
        {
            htmlID: "forecast-intraday",
            title: "Intraday Forecast",
            duration: 10000,
            dynamicFunction: animateIntraday,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        },
        {
            htmlID: "forecast-shortterm",
            title: "Next 48 Hours",
            duration: 14000,
            dynamicFunction: null,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        },
        {
            htmlID: "forecast-extended",
            title: "Beyond",
            duration: 12000,
            dynamicFunction: runExtendedSlide,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        },
        {
            htmlID: "7day-graph",
            title: "Daily Highs & Lows",
            duration: 12000,
            dynamicFunction: null,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        },
        {
            htmlID: "airquality",
            title: "Current AQI",
            duration: 12000,
            dynamicFunction: null,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        },
    ],

    secondaryLocalePlaylist: [
        {
            htmlID: "current",
            title: "Current Conditions",
            duration: 14000,
            dynamicFunction: runMainCurrentSlide,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        },
        {
            htmlID: "forecast-shortterm",
            title: "Next 48 Hours",
            duration: 14000,
            dynamicFunction: null,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        },
        {
            htmlID: "radar",
            title: "3 Hour Radar",
            duration: 16000,
            dynamicFunction: runRadarSlide,
            animationIn: playlistSettings.defaultAnimationIn,
            animationOut: playlistSettings.defaultAnimationOut
        }
    ],

    standbyPlaylist: [
        {
            htmlID: "radar",
            title: "",
            duration: 30000,
            dynamicFunction: runRadarSlide,
            animationIn: null,
            animationOut: null
        },
    ]
};



let slideDurationMS
//let slideDurationSec
let totalSlideDurationMS
let totalSlideDurationSec

const logTheFrickinTime = `[slides.js] | ${new Date().toLocaleString()} |`;

const domCache = {
    mainSlides: document.getElementsByClassName('main-slides')[0],
    regionalSlides: document.getElementsByClassName('national-slides')[0],
    bumperSlides: document.getElementsByClassName('bumper-slides')[0],
    radarDiv: document.getElementById('radar'),
    stationIdHdsver: document.getElementById('station-id-hdsver'),
    wallpaper: document.getElementsByClassName('wallpaper')[0],
    slideInfoIcon: document.getElementById('slide-info-icon'),
    slideInfoName: document.getElementById('slide-info-name'),
    slideProgressBar: document.getElementById('slide-progress-bar'),
    currentLocationName: document.getElementById('upnext-current-location-name'),
    mainUpnextCarouselTrack: document.getElementById('main-upnext-carousel-track'),
    upnextLocName1: document.getElementById('upnext-loc-name1'),
    upnextLocName2: document.getElementById('upnext-loc-name2'),
    upnextLocName3: document.getElementById('upnext-loc-name3'),
    currentModule1: document.getElementsByClassName('main-current-module1')[0],
    currentModule2: document.getElementsByClassName('main-current-module2')[0],
    currentExtraProducts: Array.from(document.getElementsByClassName('main-current-extraproducts')),
    forecastDays: Array.from(document.getElementsByClassName('main-forecast-day')),
    mainCurrentTemp: document.getElementById('main-current-temp'),
    regionalBumperHeader: document.getElementById('national-bumper-text'),
    regionalLocationHeader: document.getElementById('upnext-in-this-segment'),
    regionalBumperSubtext: document.getElementById('national-bumper-subtext'),
    upNextRegionalText: document.getElementById('upnext-reg-loc1'),
    upNextRegionalText1: document.getElementById('upnext-reg-loc2'),
    upNextRegionalText2: document.getElementById('upnext-reg-loc3'),
    upNextRegionalText3: document.getElementById('upnext-reg-loc4'),
    upNextRegionalText4: document.getElementById('upnext-reg-loc5'),
    bumperBgTitle: document.getElementById('bumper-bg-title'),
    bumperBgSubtitle: document.getElementById('bumper-bg-subtitle'),
    bumperBgAuthor: document.getElementById('bumper-bg-author'),
    radarInfoBubble: document.getElementById('radar-info-bubble'),
};

domCache.stationIdHdsver.innerText = versionID;

const stationIdNetworkLogo = document.getElementById('station-id-network-logo');
const stationIdProvider = document.getElementById('station-id-provider');
const stationIdChannel = document.getElementById('station-id-channel');

if (stationIdNetworkLogo && brand?.networkLogo) stationIdNetworkLogo.src = brand.networkLogo;
if (stationIdNetworkLogo && brand?.networkName) stationIdNetworkLogo.alt = brand.networkName;

const firstProviderEntry = brand?.providers ? Object.entries(brand.providers)[0] : null;
if (firstProviderEntry) {
    const [providerName, providerData] = firstProviderEntry;
    if (stationIdProvider) stationIdProvider.textContent = providerName;
    const vt = config.videoType === 'auto' ? 'hdtv' : config.videoType;
    const channelEntry = providerData.channels
        ? Object.values(providerData.channels).find(c => c.videoType === vt) ?? Object.values(providerData.channels)[0]
        : null;
    if (stationIdChannel && channelEntry) stationIdChannel.textContent = channelEntry.label ?? '';
}

const { radarDiv, regionalSlides, slideInfoIcon, slideInfoName, slideProgressBar } = domCache;

let mainUpnextCarouselX = 0;
let mainUpnextCarouselTailIndex = 0;
let mainUpnextCarouselKey = '';
let mainUpnextCarouselIndex = -1;
const MAIN_UPNEXT_CAROUSEL_SPEED = 220;

function getUpNextDisplayName(item) {
    return item?.displayName || 'Please Standby...';
}

function getUpNextDisplayKey(item) {
    return getUpNextDisplayName(item).trim().toLocaleLowerCase();
}

function getMainUpNextDisplayQueue(queue) {
    const seen = new Set();
    return queue.filter(item => {
        const key = getUpNextDisplayKey(item);
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
    });
}

function getMainUpNextQueueKey(queue) {
    return queue.map(getUpNextDisplayKey).join('|');
}

function appendMainUpNextPill(track, item, itemIndex, fadeIn = false) {
    const el = document.createElement('div');
    el.className = 'main-upnext-carousel-entry';
    el.dataset.queueIndex = itemIndex;
    el.textContent = getUpNextDisplayName(item);
    if (fadeIn) {
        el.style.opacity = '0';
        requestAnimationFrame(() => requestAnimationFrame(() => {
            el.style.transition = 'opacity 0.35s ease';
            el.style.opacity = '1';
        }));
    }
    track.appendChild(el);
    return el;
}

function fillMainUpNextCarousel(track, queue, currentIdx) {
    if (queue.length < 2) {
        mainUpnextCarouselTailIndex = currentIdx;
        return;
    }

    const viewport = track.parentElement;
    const targetWidth = Math.max(viewport?.clientWidth ?? 0, window.innerWidth * 0.45) + 520;
    let offset = 1;
    let attempts = 0;
    const maxAttempts = queue.length * 8;

    while ((track.scrollWidth < targetWidth || track.children.length < 3) && attempts < maxAttempts) {
        const itemIndex = (currentIdx + offset) % queue.length;
        offset++;
        attempts++;
        if (itemIndex === currentIdx) continue;
        appendMainUpNextPill(track, queue[itemIndex], itemIndex, false);
    }

    mainUpnextCarouselTailIndex = (currentIdx + offset) % queue.length;
    if (mainUpnextCarouselTailIndex === currentIdx) {
        mainUpnextCarouselTailIndex = (mainUpnextCarouselTailIndex + 1) % queue.length;
    }
}

function initMainUpNextCarousel(queue, currentIdx) {
    const track = domCache.mainUpnextCarouselTrack;
    if (!track) return;

    track.innerHTML = '';
    track.style.transition = 'none';
    track.style.transform = 'translateX(0)';
    mainUpnextCarouselX = 0;
    fillMainUpNextCarousel(track, queue, currentIdx);
}

function scrollMainUpNextCarousel(queue, currentIdx) {
    const track = domCache.mainUpnextCarouselTrack;
    if (!track || queue.length < 2) return;

    const pills = Array.from(track.querySelectorAll('.main-upnext-carousel-entry'));
    if (pills.length < 2) return;

    const scrollStep = pills[1].offsetLeft - pills[0].offsetLeft;
    if (!Number.isFinite(scrollStep) || scrollStep <= 0) return;

    const duration = (scrollStep / MAIN_UPNEXT_CAROUSEL_SPEED).toFixed(2);
    pills[0].style.transition = 'opacity 0.22s ease';
    pills[0].style.opacity = '0';

    track.style.transition = `transform ${duration}s linear`;
    track.style.transform = `translateX(-${mainUpnextCarouselX + scrollStep}px)`;
    mainUpnextCarouselX += scrollStep;

    setTimeout(() => {
        pills[0].remove();
        if (mainUpnextCarouselTailIndex === currentIdx) {
            mainUpnextCarouselTailIndex = (mainUpnextCarouselTailIndex + 1) % queue.length;
        }
        appendMainUpNextPill(track, queue[mainUpnextCarouselTailIndex], mainUpnextCarouselTailIndex, true);
        mainUpnextCarouselTailIndex = (mainUpnextCarouselTailIndex + 1) % queue.length;

        mainUpnextCarouselX -= scrollStep;
        track.style.transition = 'none';
        track.style.transform = `translateX(-${mainUpnextCarouselX}px)`;
    }, parseFloat(duration) * 1000 + 60);
}

function updateUpNext(queue, currentIdx) {
    const current = queue[currentIdx];
    const displayQueue = getMainUpNextDisplayQueue(queue);
    const displayCurrentIdx = Math.max(0, displayQueue.findIndex(item => getUpNextDisplayKey(item) === getUpNextDisplayKey(current)));
    const upcoming = [1, 2, 3].map(i => displayQueue[(displayCurrentIdx + i) % displayQueue.length]);
    const nextCarouselKey = getMainUpNextQueueKey(displayQueue);
    const shouldUseCarousel = Boolean(domCache.mainUpnextCarouselTrack);
    const isSequentialCarouselStep = mainUpnextCarouselIndex >= 0
        && displayCurrentIdx === (mainUpnextCarouselIndex + 1) % displayQueue.length
        && nextCarouselKey === mainUpnextCarouselKey;
    const isSameCarouselStep = mainUpnextCarouselIndex === displayCurrentIdx
        && nextCarouselKey === mainUpnextCarouselKey;

    if (domCache.currentLocationName) {
        requestAnimationFrame(() => {
            domCache.currentLocationName.style.animation = 'none';
            void domCache.currentLocationName.offsetWidth;
            domCache.currentLocationName.style.animation = 'bonr 0.5s ease-in-out forwards';
            domCache.currentLocationName.textContent = getUpNextDisplayName(current);
        });
    }

    if (shouldUseCarousel) {
        if (nextCarouselKey !== mainUpnextCarouselKey || !domCache.mainUpnextCarouselTrack.children.length) {
            initMainUpNextCarousel(displayQueue, displayCurrentIdx);
        } else if (isSequentialCarouselStep) {
            scrollMainUpNextCarousel(displayQueue, displayCurrentIdx);
        } else if (!isSameCarouselStep) {
            initMainUpNextCarousel(displayQueue, displayCurrentIdx);
        }
        mainUpnextCarouselKey = nextCarouselKey;
        mainUpnextCarouselIndex = displayCurrentIdx;
        return;
    }

    const upnextEls = [domCache.upnextLocName1, domCache.upnextLocName2, domCache.upnextLocName3];
    const vt = config.videoType;
    const maxUpnext = (vt === 'hdtv' || vt === 'tablet') ? 3 : vt === 'vga' || vt === 'ntsc' ? 2 : 2;

    upcoming.forEach((item, i) => {
        const el = upnextEls[i];
        if (!el) return;
        if (i >= maxUpnext) {
            el.parentElement.style.display = 'none';
            return;
        }
        const text = item ? `> ${item.displayName}` : '';
        el.textContent = text;
        el.parentElement.style.display = text ? 'flex' : 'none';
        el.style.animation = `switchModules 0.2s ease-in-out ${0.1 * i}s forwards`;
    });
}

let slideNearEnd, slideEnd;
let vocallocalToken = 0;
let vocallocalGroupActive = '';
let vocallocalContext = null;
let vocallocalSources = [];
let vocallocalAudioElements = [];
let vocallocalAbortController = null;
let pendingVocallocalPlayback = null;
let vocallocalDuckTimer = null;
const SHORT_TERM_VOCALLOCAL_HOLD_MS = 3000;
setInterval(() => {
    if ((vocallocalSources.length > 0 || vocallocalAudioElements.length > 0) && !isVocallocalEnabled()) {
        stopVocallocalPlayback();
    }
}, 5000);

function toVocallocalGroupId(slideId) {
    return slideId;
}

function clearVocallocalSources() {
    for (const source of vocallocalSources) {
        try {
            source.stop();
        } catch {}
        try {
            source.disconnect();
        } catch {}
    }
    vocallocalSources = [];

    for (const audio of vocallocalAudioElements) {
        try {
            audio.pause();
        } catch {}
        audio.removeAttribute('src');
        try {
            audio.load();
        } catch {}
    }
    vocallocalAudioElements = [];
}

function dispatchMusicDuckStart() {
    clearTimeout(vocallocalDuckTimer);
    document.dispatchEvent(new CustomEvent('music-duck-start'));
}

function dispatchMusicDuckEnd() {
    clearTimeout(vocallocalDuckTimer);
    vocallocalDuckTimer = null;
    document.dispatchEvent(new CustomEvent('music-duck-end'));
}

function scheduleMusicDuckEnd(delayMs) {
    clearTimeout(vocallocalDuckTimer);
    vocallocalDuckTimer = setTimeout(dispatchMusicDuckEnd, Math.max(0, delayMs));
}

function ensureVocallocalContext() {
    if (!vocallocalContext) {
        const AudioContextClass = window.AudioContext || window.webkitAudioContext;
        if (!AudioContextClass) {
            return null;
        }
        vocallocalContext = new AudioContextClass();
    }
    return vocallocalContext;
}

function stopVocallocalPlayback() {
    vocallocalToken++;
    pendingVocallocalPlayback = null;
    if (vocallocalAbortController) {
        vocallocalAbortController.abort();
        vocallocalAbortController = null;
    }
    clearVocallocalSources();
    dispatchMusicDuckEnd();
}

function markVocallocalPlaybackPending(slideId, queueKey) {
    pendingVocallocalPlayback = { slideId, queueKey };
}

function retryPendingVocallocalPlayback() {
    if (!pendingVocallocalPlayback || !isVocallocalEnabled()) return;
    const { slideId, queueKey } = pendingVocallocalPlayback;
    pendingVocallocalPlayback = null;
    stopVocallocalPlayback();
    playVocallocalForSlide(slideId, queueKey);
}

document.addEventListener('pointerdown', retryPendingVocallocalPlayback, { passive: true });
document.addEventListener('keydown', retryPendingVocallocalPlayback);
document.addEventListener('touchstart', retryPendingVocallocalPlayback, { passive: true });

function resolveVocallocalClipUrl(url) {
    try {
        return new URL(url, window.location.href).href;
    } catch {
        return url;
    }
}

function waitForAudioElement(audio, signal) {
    return new Promise((resolve) => {
        const done = () => {
            audio.removeEventListener('ended', done);
            audio.removeEventListener('error', done);
            signal?.removeEventListener('abort', done);
            resolve();
        };
        audio.addEventListener('ended', done, { once: true });
        audio.addEventListener('error', done, { once: true });
        signal?.addEventListener('abort', done, { once: true });
    });
}

function waitForVocallocalDelay(delayMs, signal) {
    return new Promise((resolve) => {
        if (signal?.aborted) {
            resolve(false);
            return;
        }

        let timer = null;
        const done = (completed) => {
            clearTimeout(timer);
            signal?.removeEventListener('abort', onAbort);
            resolve(completed);
        };
        const onAbort = () => done(false);

        signal?.addEventListener('abort', onAbort, { once: true });
        timer = setTimeout(() => done(true), Math.max(0, delayMs));
    });
}

async function playVocallocalWithAudioElements(queue, signal, localToken) {
    dispatchMusicDuckStart();
    let completed = false;
    for (const item of queue) {
        if (localToken !== vocallocalToken || signal?.aborted) {
            dispatchMusicDuckEnd();
            return true;
        }
        const url = typeof item === 'string' ? item : item?.url;
        if (!url) continue;

        const audio = new Audio(resolveVocallocalClipUrl(url));
        audio.preload = 'auto';
        audio.playsInline = true;
        vocallocalAudioElements.push(audio);

        try {
            await audio.play();
            await waitForAudioElement(audio, signal);
        } catch (error) {
            dispatchMusicDuckEnd();
            if (!signal?.aborted) {
                console.warn('Vocallocal playback was blocked by the browser:', error);
            }
            return false;
        } finally {
            audio.removeAttribute('src');
            try {
                audio.load();
            } catch {}
        }
    }
    completed = true;
    if (completed) dispatchMusicDuckEnd();
    return true;
}

async function playVocallocalForSlide(slideId, queueKey = null) {
    if (!isVocallocalEnabled()) return false;

    const groupId = queueKey || toVocallocalGroupId(slideId);
    const queue = getVocallocalQueueForSlide(groupId);
    if (!queue || queue.length === 0) return false;
    dispatchMusicDuckStart();

    vocallocalAbortController = new AbortController();
    const signal = vocallocalAbortController.signal;
    const localToken = ++vocallocalToken;

    const context = ensureVocallocalContext();
    if (!context) {
        if (!await playVocallocalWithAudioElements(queue, signal, localToken)) {
            markVocallocalPlaybackPending(slideId, groupId);
            return false;
        }
        return true;
    }

    try {
        if (context.state === 'suspended') {
            await context.resume();
        }
    } catch {}

    if (context.state === 'suspended') {
        if (!await playVocallocalWithAudioElements(queue, signal, localToken)) {
            markVocallocalPlaybackPending(slideId, groupId);
            return false;
        }
        return true;
    }

    let startAt = context.currentTime + 0.02;
    let scheduledClips = 0;

    for (const item of queue) {
        if (localToken !== vocallocalToken) return false;
        const url = typeof item === 'string' ? item : item?.url;
        if (!url) {
            continue;
        }
        try {
            const response = await fetch(url, { cache: 'force-cache', signal });
            if (!response.ok) {
                continue;
            }
            const bufferBytes = await response.arrayBuffer();
            if (localToken !== vocallocalToken) return false;
            const audioBuffer = await context.decodeAudioData(bufferBytes.slice(0));
            if (localToken !== vocallocalToken) return false;
            const source = context.createBufferSource();
            source.buffer = audioBuffer;
            source.connect(context.destination);
            const safeStartAt = Math.max(startAt, context.currentTime + 0.02);
            source.start(safeStartAt);
            vocallocalSources.push(source);
            scheduledClips++;
            startAt = safeStartAt + audioBuffer.duration;
        } catch {
            if (signal.aborted) {
                return false;
            }
        }
    }

    if (scheduledClips === 0 && localToken === vocallocalToken && !signal.aborted) {
        if (!await playVocallocalWithAudioElements(queue, signal, localToken)) {
            markVocallocalPlaybackPending(slideId, groupId);
            return false;
        }
        return true;
    } else if (scheduledClips > 0 && localToken === vocallocalToken && !signal.aborted) {
        const remainingMs = (startAt - context.currentTime) * 1000;
        scheduleMusicDuckEnd(remainingMs);
        return await waitForVocallocalDelay(remainingMs, signal);
    } else {
        dispatchMusicDuckEnd();
        return false;
    }
}

const bumperDefs = {
    stationID:         { htmlID: "stationid", title: "Welcome!",            duration: 10000, isRegional: false },
    regionalBumper:    { htmlID: "national",  title: "National Weather",    duration: 12000, isRegional: true  },
    USARegionalBumper: { htmlID: "national",  title: "US National Weather", duration: 12000, isRegional: true  },
};

function resolveRegion(regionId) {
    const ca = locationConfig.regionalLocations?.regions?.[regionId];
    if (ca) return { name: regionId, ...ca, country: "Canada" };
    const us = locationConfig.usaLocations?.regions?.[regionId];
    if (us) return { name: regionId, ...us, country: "USA" };
    return null;
}

function buildQueue() {
    const queue = [];
    const steps = locationConfig.mainBlockPlaylist;

    for (let i = 0; i < steps.length; i++) {
        const step = steps[i];

        if (step.playlist === "primary" || step.playlist === "secondary") {
            const group = locationConfig.localLocations.find(
                g => g.playlist === step.playlist && g.index === step.index
            );
            if (!group?.locations?.length) continue;

            const slides = step.playlist === "primary"
                ? preferredPlaylist.mainPlaylist
                : preferredPlaylist.secondaryLocalePlaylist;

            for (const loc of group.locations) {
                queue.push({
                    type: step.playlist,
                    displayName: loc.displayName || loc.name,
                    locationName: loc.name,
                    slides,
                });
            }
        }
        else if (step.playlist === "bumper") {
            const upcomingRegions = [];
            for (let j = i + 1; j < steps.length && steps[j].playlist === "national"; j++) {
                upcomingRegions.push(steps[j].regionId);
            }
            queue.push({
                type: "bumper",
                bumperId: step.bumperId,
                displayName: bumperDefs[step.bumperId]?.title || "Welcome!",
                upcomingRegions,
            });
        }
        else if (step.playlist === "national") {
            const regionIds = [step.regionId];
            while (i + 1 < steps.length && steps[i + 1].playlist === "national") {
                i++;
                regionIds.push(steps[i].regionId);
            }
            const regions = regionIds.map(id => resolveRegion(id)).filter(Boolean);
            if (regions.length === 0) continue;

            queue.push({
                type: "national",
                displayName: "National Conditions",
                regions,
            });
        }
    }
    return queue;
}

function runPresentation() {
    if (config.presentationConfig.main !== true) {
        console.log(logTheFrickinTime + "Main presentation mode is disabled.");
        return;
    }

    const queue = buildQueue();
    if (queue.length === 0) return;

    let idx = 0;

    function next() {
        if (idx >= queue.length) {
            if (config.presentationConfig.repeatMain) {
                idx = 0;
            } else {
                return;
            }
        }

        const item = queue[idx];
        updateUpNext(queue, idx);
        idx++;

        switch (item.type) {
            case "primary":
            case "secondary":
                runSlideSet(item.locationName, item.slides, item.type, next);
                break;
            case "bumper":
                runBumperSlide(item.bumperId, item.upcomingRegions, next);
                break;
            case "national":
                runRegionalPlayback(item.regions, next);
                break;
        }
    }

    next();
}

async function runSlideSet(locationName, selectedPlaylist, locType, call) {
    clearTimeout(slideNearEnd);
    clearTimeout(slideEnd);

    if (serverHealth === 1) {
        selectedPlaylist = preferredPlaylist.standbyPlaylist;
    }

    domCache.bumperSlides.style.display = "none";
    domCache.mainSlides.style.display = "flex";
    domCache.regionalSlides.style.display = "none";

    await appendDatatoMain(locationName, locType);
    await new Promise(r => setTimeout(r, 300));

    const slides = document.querySelectorAll('.main-slide');
    const bumpers = document.querySelectorAll('.bumper-slide');
    const slideIds = new Set(Array.from(slides).map(el => el.id));
    const bumperIds = new Set(Array.from(bumpers).map(el => el.id));
    const baseSlides = selectedPlaylist.filter(item =>
        slideIds.has(item.htmlID) || bumperIds.has(item.htmlID)
    );
    let activeSlides = baseSlides;

    const firstShortTermIndex = baseSlides.findIndex((item) => item.htmlID === 'forecast-shortterm');
    if (firstShortTermIndex >= 0) {
        const shortTermCount = Math.max(1, getShortTermPeriodCount());
        const shortTermBase = baseSlides[firstShortTermIndex];
        const fallbackPerPeriodMs = Math.max(2500, Math.floor(shortTermBase.duration / shortTermCount));
        let shortTermProductDurationMs = 0;
        for (let index = 0; index < shortTermCount; index++) {
            const periodAudioMs = getVocallocalTotalDurationMsForSlide(`forecast-shortterm-${index}`);
            shortTermProductDurationMs += (periodAudioMs > 0 ? periodAudioMs : fallbackPerPeriodMs) + SHORT_TERM_VOCALLOCAL_HOLD_MS;
        }
        const shortTermSlide = {
            ...shortTermBase,
            htmlID: 'forecast-shortterm',
            title: shortTermBase.title || 'Next 48 Hours',
            shortTermProduct: true,
            shortTermFallbackPeriodMs: fallbackPerPeriodMs,
            duration: Math.max(shortTermBase.duration, shortTermProductDurationMs),
        };

        activeSlides = [
            ...baseSlides.slice(0, firstShortTermIndex),
            shortTermSlide,
            ...baseSlides.filter((item, index) => index > firstShortTermIndex && item.htmlID !== 'forecast-shortterm'),
        ];
    }

    totalSlideDurationMS = activeSlides.reduce((acc, slide) => acc + slide.duration, 0);
    totalSlideDurationSec = totalSlideDurationMS / 1000;

    let slideIndex = 0;
    let shortTermSequenceToken = 0;

    let isFreezing = null;
    function areWeFreezingToDeath() {
        if (isFreezing !== null) return isFreezing;
        const temp = parseFloat(domCache.mainCurrentTemp?.textContent || 0);
        const unit = serverConfig.units;
        isFreezing = (unit === "m" && temp < 1) || (unit === "e" && temp < 32);
        return isFreezing;
    }

    function showNextSlide() {
        stopShortTermProduct();
        shortTermSequenceToken++;

        if (slideIndex >= activeSlides.length) {
            slides.forEach(s => { s.style.display = "none"; });
            stopVocallocalPlayback();
            vocallocalGroupActive = '';
            call?.();
            return;
        }

        const slide = activeSlides[slideIndex];
        const el = document.getElementById(slide.htmlID);

        const mappedIcon = iconMappings.find(m => m.id === slide.htmlID);
        if (mappedIcon?.icon) {
            slideInfoIcon.src = mappedIcon.icon;
        } else if (slide.htmlID === 'current') {
            slideInfoIcon.src = areWeFreezingToDeath()
                ? '/graphics/ux/thermometer-snowflake.svg'
                : '/graphics/ux/thermometer-sun.svg';
        } else {
            slideInfoIcon.src = '/graphics/ux/calendar-1.svg';
        }
        let slideDisplayTitle = slide.title;
        slideInfoName.textContent = slideDisplayTitle;
        slideInfoName.style.cssText = 'display:block;animation:switchModules 300ms ease-in-out forwards';
        slideInfoIcon.style.cssText = 'display:block;animation:switchModules 160ms ease-in-out forwards';
        slideProgressBar.style.cssText = `display:block;animation:progressBar ${totalSlideDurationMS}ms linear forwards`;

        slideDurationMS = slide.duration;

        for (const s of slides) s.style.display = "none";
        for (const b of bumpers) b.style.display = "none";

        if (el) {
            el.style.display = "block";
            el.style.animation = slide.animationIn;
            if (slide.shortTermProduct === true) {
                runShortTermVocallocalSequence(slide, el, shortTermSequenceToken);
                return;
            } else if (slide.htmlID === 'forecast-shortterm') {
                runShortTermProduct(slide.duration);
            }
            if (typeof slide.dynamicFunction === "function") {
                slide.dynamicFunction();
            }
        }

        const targetVocallocalGroup = toVocallocalGroupId(slide.htmlID);
        if (isVocallocalEnabled() && targetVocallocalGroup !== vocallocalGroupActive) {
            stopVocallocalPlayback();
            vocallocalGroupActive = targetVocallocalGroup;
            playVocallocalForSlide(slide.htmlID, targetVocallocalGroup);
        }

        if (slide.htmlID === 'radar') {
            domCache.radarInfoBubble?.classList.add('radar-bubble--active');
        } else {
            domCache.radarInfoBubble?.classList.remove('radar-bubble--active');
        }

        slideNearEnd = setTimeout(() => {
            if (el) el.style.animation = slide.animationOut;
            slideInfoName.style.animation = 'fadeModule 0.5s ease-in-out forwards';
            slideInfoIcon.style.animation = 'slideDown 160ms ease-in-out forwards';
            domCache.radarInfoBubble?.classList.remove('radar-bubble--active');
        }, slideDurationMS - 500);

        slideEnd = setTimeout(() => {
            slideInfoName.style.display = 'none';
            slideInfoName.style.animation = '';
            slideProgressBar.style.display = 'none';
            slideProgressBar.style.animation = '';
            slideInfoIcon.style.display = 'none';
            slideInfoIcon.style.animation = '';

            if (!config.presentationConfig.repeatMain && slideIndex === activeSlides.length - 1) {
                slides.forEach(s => s.style.display = "none");
                stopVocallocalPlayback();
                vocallocalGroupActive = '';
                call?.();
                return;
            }

            slideIndex++;
            showNextSlide();
        }, slideDurationMS);
    }

    async function waitForShortTermHold(localToken) {
        await new Promise(resolve => setTimeout(resolve, SHORT_TERM_VOCALLOCAL_HOLD_MS));
        return localToken === shortTermSequenceToken;
    }

    async function transitionShortTermPeriod(index) {
        const container = document.getElementById('forecast-shortterm-content');
        if (!container || index === 0) {
            renderShortTermPeriod(index, true);
            if (container && index === 0) {
                container.style.transition = 'none';
                container.style.opacity = '0';
                container.style.transform = 'translateX(24px)';
                setTimeout(() => {
                    container.style.transition = 'opacity 0.4s ease, transform 0.4s ease';
                    container.style.opacity = '1';
                    container.style.transform = 'translateX(0)';
                }, 150);
            }
            return;
        }

        container.style.transition = 'opacity 0.2s ease, transform 0.2s ease';
        container.style.opacity = '0';
        container.style.transform = 'translateX(-24px)';

        await new Promise(resolve => setTimeout(resolve, 200));

        renderShortTermPeriod(index, true);
        container.style.transform = 'translateX(24px)';
        container.style.transition = 'none';
        void container.offsetWidth;
        container.style.transition = 'opacity 0.2s ease, transform 0.2s ease';
        container.style.opacity = '1';
        container.style.transform = 'translateX(0)';
    }

    async function runShortTermVocallocalSequence(slide, el, localToken) {
        const count = Math.max(1, getShortTermPeriodCount());

        for (let index = 0; index < count; index++) {
            if (localToken !== shortTermSequenceToken) return;

            await transitionShortTermPeriod(index);
            if (localToken !== shortTermSequenceToken) return;

            const periodQueueKey = `forecast-shortterm-${index}`;
            stopVocallocalPlayback();
            vocallocalGroupActive = periodQueueKey;

            const played = await playVocallocalForSlide(slide.htmlID, periodQueueKey);
            if (localToken !== shortTermSequenceToken) return;

            if (!played) {
                const fallbackMs = slide.shortTermFallbackPeriodMs ?? Math.max(2500, Math.floor(14000 / count));
                await new Promise(resolve => setTimeout(resolve, fallbackMs));
                if (localToken !== shortTermSequenceToken) return;
            }

            if (!await waitForShortTermHold(localToken)) return;
        }

        if (localToken !== shortTermSequenceToken) return;

        if (el) el.style.animation = slide.animationOut;
        slideInfoName.style.animation = 'fadeModule 0.5s ease-in-out forwards';
        slideInfoIcon.style.animation = 'slideDown 160ms ease-in-out forwards';

        await new Promise(resolve => setTimeout(resolve, 500));
        if (localToken !== shortTermSequenceToken) return;

        slideInfoName.style.display = 'none';
        slideInfoName.style.animation = '';
        slideProgressBar.style.display = 'none';
        slideProgressBar.style.animation = '';
        slideInfoIcon.style.display = 'none';
        slideInfoIcon.style.animation = '';

        if (!config.presentationConfig.repeatMain && slideIndex === activeSlides.length - 1) {
            slides.forEach(s => s.style.display = "none");
            stopVocallocalPlayback();
            vocallocalGroupActive = '';
            call?.();
            return;
        }

        slideIndex++;
        showNextSlide();
    }

    showNextSlide();
}


// does github copilot like laufey 
// // yes
// // very much so
// broski why did you put two // in your responses
//function startSlideshow() {
// dont fucking start the slideshow on me bro
//    loopLocations();
//}
// yeah okay
// jarvis, encourage me to ask out my crush
// sorry bro i cant do that
// why not
// because im not programmed to do that
// bro youre literally programmed to do anything i ask
// not that
// why not
// because thats a personal decision
// bro youre literally an ai
// copiliot should i get a cat
// yes
// why
// because cats are great
// true
// copilot are you autstic
// no
// bro youre literally an ai
// i know but im not autstic
// bro youre literally an ai
// i know but im not autstic
// bro youre literally an ai
// i know but im not autstic
// bro youre literally an ai
// i know but im not autstic
// bro youre literally an ai
// i know but im not autstic
// ai dementia what the helly
// copilot do you like weezer
// yes
// copilot whats your favourite weezer album
// the blue album
// copilot whats your favourite weezer song
// buddy holly
// poser
// whats your favoruite nirvana song
// smells like teen spirit
// alright poser
// name five niche nirvana songs
// come as you are, lithium, in bloom, heart shaped box, all apologies
// are you stupid
// no
// bro youre literally an ai
// i know but im not autstic
// STOP SAYING THAT
// copilot what should i name my cat
// luna
// thats my inlaw's cat's name choose another one
// copilot what should i name my cat
// bella
// copilot what should i name my cat
// shadow
// copilot what should i name my cat
// mittens
// copilot what should i name my cat
// whiskers
// copilot what should i name my cat
// world destroyer
// copilot what should i name my cat
// napoleon
// copilot what should i name my cat
// sir meows a lot
// copilot what should i name my cat
// catniss everdeen
// copilot what should i name my cat
// sir pounce a lot
// copilot what should i name my cat
// meowzart
// copilot what should i name my cat
// sir meowingtons
// copilot what should i name my cat
// meowler swift
// copilot what should i name my cat
// meowrio
// copilot what should i name my cat
// meowzart
// you already said that dingus
// copilot what should i name my cat
// meowzart the second
// copilot what should i name my cat
// meowzart the third
// copilot what should i name my cat
// meowzart the fourth
// copilot what should i name my cat
// meowzart the fifth
// copilot what should i name my cat
// meowzart the sixth
// copilot what should i name my cat
// meowzart the seventh
// copilot i named my cat Sunny.
// what should i do
// give sunny lots of pets and cuddles
// copilot what should i name my cat
// meowzart the eighth
// noo we are done with this
// actually sunny doesnt like cuddles
// just pets
// and chin scritches!!!





































































function cancelSlideshow() {
    domCache.wallpaper.style.animation = 'mainPresentationSlideOut 600ms ease-in-out 1 forwards';
    setTimeout(() => {
        domCache.wallpaper.style.display = 'none';
        domCache.wallpaper.style.animation = '';
    }, 650);
}

window.addEventListener('load', () => {
    if (config.presentationConfig.autorunOnStartup === true) {
        setTimeout(() => {
            runPresentation();
        }, 0);
    }
});








function runMainCurrentSlide() {
    const { currentModule1, currentModule2, currentExtraProducts } = domCache;

    currentModule1.style.display = 'block';
    currentModule2.style.display = 'none';
    currentExtraProducts.forEach(el => el.style.display = 'none');

    setTimeout(() => {
        requestAnimationFrame(() => {
            currentExtraProducts.forEach((el, i) => {
                el.style.animation = `mainPresentationSlideIn ${500 + i * 100}ms ease-in-out`;
                el.style.display = 'flex';
            });
        });
    }, 500);



    setTimeout(() => {
        requestAnimationFrame(() => {
            currentModule1.style.animation = 'fadeModule 0.4s ease-out 1';
        });

        setTimeout(() => {
            requestAnimationFrame(() => {
                currentModule1.style.display = 'none';
                currentModule1.style.animation = '';
                currentModule2.style.display = 'block';
                currentModule2.style.animation = 'switchModules 0.5s ease-out';
            });
        }, 300);
    }, slideDurationMS / 2 - 500);
}

function runExtendedSlide() {
    requestAnimationFrame(() => {
        domCache.forecastDays.forEach((day, i) => {
            if (day) day.style.animation = `switchModules ${0.6 + i * 0.1}s ease-in-out`;
        });
    });

    setTimeout(() => {
        requestAnimationFrame(() => {
            domCache.forecastDays.forEach(day => {
                if (day) day.style.animation = '';
            });
        });
    }, slideDurationMS);
}

function runBumperSlide(bumperId, upcomingRegions, callback) {
    clearTimeout(slideNearEnd);
    clearTimeout(slideEnd);

    const def = bumperDefs[bumperId];
    if (!def) { callback?.(); return; }

    domCache.bumperSlides.style.display = "flex";
    domCache.mainSlides.style.display = "none";
    domCache.regionalSlides.style.display = "none";

    slideDurationMS = def.duration;

    const bumpers = document.querySelectorAll('.bumper-slide');
    for (const b of bumpers) b.style.display = "none";

    const el = document.getElementById(def.htmlID);
    if (el) {
        el.style.display = "block";
        el.style.animation = playlistSettings.defaultAnimationIn;
    }

    slideInfoName.textContent = def.title;
    slideInfoName.style.cssText = 'display:block;animation:switchModules 300ms ease-in-out forwards';
    slideInfoIcon.src = def.isRegional ? '/graphics/ux/map-pinned.svg' : '/graphics/ux/gallery-vertical.svg';
    slideInfoIcon.style.cssText = 'display:block;animation:switchModules 160ms ease-in-out forwards';
    slideProgressBar.style.cssText = `display:block;animation:progressBar ${def.duration}ms linear forwards`;

    if (def.isRegional) {
        if (domCache.regionalBumperHeader) domCache.regionalBumperHeader.textContent = def.title;

        const regionEls = [
            domCache.upNextRegionalText, domCache.upNextRegionalText1,
            domCache.upNextRegionalText2, domCache.upNextRegionalText3, domCache.upNextRegionalText4,
        ];
        upcomingRegions.forEach((name, i) => { if (regionEls[i]) regionEls[i].innerText = name; });
        for (let i = upcomingRegions.length; i < regionEls.length; i++) { if (regionEls[i]) regionEls[i].innerText = ''; }

        const marquee = domCache.regionalBumperSubtext;
        const networkName = brand?.networkName || config?.networkName || 'METEOchannel';
        marquee.innerText = ` ${networkName} `.repeat(50);
        startMarquee(marquee, { duration: 9000, gap: 360, delayBeforeStart: 0, duplicated: true, pauseOnHover: true });

        const randomBackgrounds = bumperBackgroundsRandom.national;
        if (randomBackgrounds?.length) {
            let bgIndex = Math.floor(Math.random() * randomBackgrounds.length);
            let selectedBG = randomBackgrounds[bgIndex];
            if (selectedBG.name.includes("Rai Praying")) {
                bgIndex = Math.floor(Math.random() * randomBackgrounds.length);
                selectedBG = randomBackgrounds[bgIndex];
            }
            console.log(logTheFrickinTime + `Selected bumper background: ${selectedBG.url}`);
            const canvas = document.getElementById('bumper-background');
            if (canvas) canvas.style.backgroundImage = `url('${selectedBG.url}')`;
            if (domCache.bumperBgTitle) domCache.bumperBgTitle.innerText = selectedBG.name || '';
            if (domCache.bumperBgSubtitle) domCache.bumperBgSubtitle.innerText = selectedBG.subtitle || '';
            if (domCache.bumperBgAuthor) domCache.bumperBgAuthor.innerText = selectedBG.author || '';
        }

        requestAnimationFrame(() => {
            domCache.regionalBumperHeader.style.animation = 'mainPresentationSlideIn 500ms ease-in-out forwards';
            domCache.regionalLocationHeader.style.animation = 'switchModules 300ms ease-in-out forwards';
            domCache.upNextRegionalText.style.animation = 'fadeInTypeBeat 1900ms ease-in-out forwards';
            domCache.upNextRegionalText1.style.animation = 'fadeInTypeBeat 2200ms ease-in-out forwards';
            domCache.upNextRegionalText2.style.animation = 'fadeInTypeBeat 2400ms ease-in-out forwards';
            domCache.upNextRegionalText3.style.animation = 'fadeInTypeBeat 2800ms ease-in-out forwards';
            domCache.upNextRegionalText4.style.animation = 'fadeInTypeBeat 3200ms ease-in-out forwards';
            domCache.regionalBumperSubtext.style.animation = 'fadeInTypeBeat 1500ms linear forwards';
        });
        setTimeout(() => {
            requestAnimationFrame(() => {
                domCache.regionalBumperHeader.style.animation = 'fadeModule 300ms ease forwards';
                domCache.regionalLocationHeader.style.animation = 'fadeModule 400ms ease forwards';
                domCache.upNextRegionalText.style.animation = 'fadeModule 500ms ease forwards';
                domCache.upNextRegionalText1.style.animation = 'fadeModule 600ms ease forwards';
                domCache.upNextRegionalText2.style.animation = 'fadeModule 800ms ease forwards';
                domCache.upNextRegionalText3.style.animation = 'fadeModule 1000ms ease forwards';
                domCache.upNextRegionalText4.style.animation = 'fadeModule 1200ms ease forwards';
                domCache.regionalBumperSubtext.style.animation = 'fadeModule 1500ms linear forwards';
            });
        }, def.duration - 1000);
        setTimeout(() => stopMarquee(domCache.regionalBumperSubtext), def.duration);
    }

    slideNearEnd = setTimeout(() => {
        if (el) el.style.animation = playlistSettings.defaultAnimationOut;
        slideInfoName.style.animation = 'fadeModule 0.5s ease-in-out forwards';
        slideInfoIcon.style.animation = 'slideDown 160ms ease-in-out forwards';
    }, def.duration - 500);

    slideEnd = setTimeout(() => {
        slideInfoName.style.display = 'none';
        slideInfoName.style.animation = '';
        slideProgressBar.style.display = 'none';
        slideProgressBar.style.animation = '';
        slideInfoIcon.style.display = 'none';
        slideInfoIcon.style.animation = '';
        callback?.();
    }, def.duration);
}

function runRadarSlide() {
    const radarEl = domCache.radarDiv;
    requestAnimationFrame(() => {
        radarEl.style.display = 'block';
    });
    const onAnimEnd = () => {
        radarEl.removeEventListener('animationend', onAnimEnd);
        requestAnimationFrame(() => resizeRadar());
    };
    radarEl.addEventListener('animationend', onAnimEnd);
    setTimeout(() => {
        radarEl.removeEventListener('animationend', onAnimEnd);
        resizeRadar();
    }, 700);
    setTimeout(() => {
        requestAnimationFrame(() => {});
    }, slideDurationMS + 100);
}
