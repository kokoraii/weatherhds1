import { config } from "../config.js";
import { fetchOnlineBackground } from "./data.js";
import "./glass_cache.js";
import { sidebarEnabled } from "./layout.js";

const performanceConfig = config.performance ?? {};
const liveBlurParam = new URLSearchParams(window.location.search).get('liveBlur');
if (performanceConfig.disableLiveBackdropBlur !== false && liveBlurParam !== 'true') {
    document.body.classList.add('performance-static-glass');
}
if (performanceConfig.lightweightMotion !== false) {
    document.body.classList.add('performance-lightweight-motion');
}

const domElements = {
    viewport: document.getElementsByClassName("view")[0],
    mainSlides: document.getElementsByClassName("main-slides")[0],
    slidesBlock: document.getElementsByClassName("slides-block")[0],
    regionalSlides: document.getElementsByClassName("national-slides")[0],
    bumperSlides: document.getElementsByClassName("bumper-slides")[0],
    cityTicker: document.getElementsByClassName("cityticker")[0],
    contentArea: document.getElementsByClassName("content-area")[0],
    wallpaper: document.getElementsByClassName("wallpaper")[0],
    ldlContainer: document.getElementsByClassName("ldl-weather")[0],
    ldlBrandingArea: document.getElementsByClassName("ldl-branding-area")[0],
    ldlMainContent: document.getElementsByClassName("ldl-main-content")[0],
    ldlProviderLogo: document.getElementsByClassName("provider-logo-container")[0],
    date: document.getElementById("date"),
    time: document.getElementById("time"),
    i2SidebarBuffer: document.getElementsByClassName("sidebar")[0],
    upnextLocation2: document.getElementById('upnext-location2'),
    upnextLocation3: document.getElementById('upnext-location3'),
    ldlMarquee: document.getElementsByClassName('ldl-marquee')[0],
    marqueeTicker: document.getElementById('marquee-ticker')
};

const { viewport, mainSlides, regionalSlides, bumperSlides, wallpaper, ldlContainer, ldlBrandingArea, ldlMainContent, ldlProviderLogo, contentArea, cityTicker, slidesBlock } = domElements;
const date = domElements.date;
const time = domElements.time;

let broadcastState = 0; // zero is good weather, one is bad weather.
let localBackgroundObjectUrl = null;

const DAYS = ['SUN', 'MON', 'TUE', 'WED', 'THU', 'FRI', 'SAT'];
const MONTHS = ['JAN', 'FEB', 'MAR', 'APR', 'MAY', 'JUN', 'JUL', 'AUG', 'SEP', 'OCT', 'NOV', 'DEC'];

const dateFormatter = new Intl.DateTimeFormat('en-US', {
    timeZone: config.systemTimeZone,
    hour12: true,
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
    timeZoneName: 'short'
});

function debounce(fn, delay) {
    let timeout;
    return (...args) => {
        clearTimeout(timeout);
        timeout = setTimeout(() => fn(...args), delay);
    };
}

const logTheFrickinTime = `[main.js] | ${new Date().toLocaleString()} |`;

function initBackgrounds() {
        if (config.backgroundSource === "online") {
            async function onlineBg() {
                const url = await fetchOnlineBackground();
                console.log(logTheFrickinTime, "Fetched new online background:", url);
                wallpaper.style.backgroundImage = `url(${url})`;
            }
            setInterval(onlineBg, 8 * 36000000);
            onlineBg();
        } 
        if (config.backgroundSource === "local") {
            async function initLocalBackground() {
                const weatherType = broadcastState === 0 ? "wxgood" : "wxbad";
                const post = await fetch('/backgrounds/init', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'text/plain'
                    },
                    body: `[${weatherType}]`
                });
                const result = await post.text();
                console.log(logTheFrickinTime, result);
            }

            async function fetchLocalBackground() {
                const response = await fetch('/backgrounds/image');
                if (response.ok) {
                    const contentType = response.headers.get('content-type') || '';

                    if (contentType.startsWith('image/')) {
                        const imageBlob = await response.blob();
                        const objectUrl = URL.createObjectURL(imageBlob);

                        if (localBackgroundObjectUrl) {
                            URL.revokeObjectURL(localBackgroundObjectUrl);
                        }

                        localBackgroundObjectUrl = objectUrl;
                        wallpaper.style.backgroundImage = `url(${objectUrl})`;
                        console.log(logTheFrickinTime, "Applied streamed local background image", contentType);
                    } else {
                        const imageUrl = await response.text();
                        wallpaper.style.backgroundImage = `url(${imageUrl})`;
                        console.log(logTheFrickinTime, "Applied legacy local background image URL:", imageUrl);
                    }
                } else {
                    console.warn(logTheFrickinTime, "Failed to fetch background image");
                }
            }
            
            initLocalBackground();

            setTimeout(() => {
                fetchLocalBackground();
            }, 1000);
            
            setInterval(() => {
                initLocalBackground();
                setTimeout(() => {
                    fetchLocalBackground();
                }, 500);
            }, 8 * 36000000);
        }
}

const VIDEO_MODES = Object.freeze({
    vga:       { width: 1920, height: 1440, barWidth: "100%" },  // 4:3 - VGA
    ntsc:      { width: 2160, height: 1440, barWidth: "100%" },  // 3:2 - NTSC
    pal:       { width: 720,  height: 576,  barWidth: "100%" },  // 5:4 - PAL
    tablet:    { width: 2304, height: 1440, barWidth: "100%" },  // 16:10 - tablet
    hdtv:      { width: 2560, height: 1440, barWidth: "100%" },  // 16:9 - HDTV
});

const videoTypeParam = new URLSearchParams(window.location.search).get('videoType');
if (videoTypeParam !== null) {
    config.videoType = String(videoTypeParam).toLowerCase();
}

function resolveVideoMode() {
    if (config.videoType !== 'auto') return config.videoType;
    const windowRatio = window.innerWidth / window.innerHeight;
    return Object.entries(VIDEO_MODES).reduce((best, [key, mode]) => {
        const bestRatio = VIDEO_MODES[best].width / VIDEO_MODES[best].height;
        const thisRatio = mode.width / mode.height;
        return Math.abs(thisRatio - windowRatio) < Math.abs(bestRatio - windowRatio) ? key : best;
    }, 'vga');
}

function viewportAspect() {
    const containerWidth = window.innerWidth;
    const containerHeight = window.innerHeight;
    const resolvedType = resolveVideoMode();
    const mode = VIDEO_MODES[resolvedType] || VIDEO_MODES.vga;
    const { width, barWidth } = mode;

    const scaleRatio = Math.min(containerWidth / width, containerHeight / mode.height);
    const centeredLeft = (containerWidth - width * scaleRatio) / 2;
    const centeredTop = (containerHeight - mode.height * scaleRatio) / 2;

    viewport.style.cssText = `width:${width}px;transform-origin:top left;left:${centeredLeft}px;top:${centeredTop}px;transform:scale(${scaleRatio})`;
    
    contentArea.style.width = barWidth;

    const mainVideoBlock = [
        regionalSlides,
        bumperSlides,
        mainSlides
    ]

    if (resolvedType !== "hdtv" && resolvedType !== "tablet" && domElements.upnextLocation3) {
        domElements.upnextLocation3.style.display = 'none';
        mainVideoBlock.forEach(el => {
            if (el) el.style.paddingBottom = '2%'; el.style.paddingTop = '2%';
        });
    }

    if (resolvedType !== "hdtv" && resolvedType !== "tablet" && resolvedType && domElements.upnextLocation2) {
        domElements.upnextLocation2.style.display = 'none';
    }
}

window.addEventListener('resize', debounce(viewportAspect, 100));

function presentationType() {

    const mainPres = new URLSearchParams(window.location.search).get('main');
    const ldlPres = new URLSearchParams(window.location.search).get('ldl');
    const backgrounds = new URLSearchParams(window.location.search).get('backgrounds');
    const repeatMain = new URLSearchParams(window.location.search).get('repeatMain');
    const ldlBack = new URLSearchParams(window.location.search).get('ldlBack')
    const providerLogo = new URLSearchParams(window.location.search).get('providerLogo')

    if (mainPres !== null) {
        const parsed = mainPres.toLowerCase() === 'true';
        config.presentationConfig.main = parsed;
    }

    if (ldlPres !== null) {
        const parsed = ldlPres.toLowerCase() === 'true';
        config.presentationConfig.ldl = parsed;
    }
    
    if (backgrounds !== null) {
        const parsed = backgrounds.toLowerCase() === 'true';
        config.presentationConfig.backgrounds = parsed;
    }
    if (repeatMain !== null) {
        const parsed = repeatMain.toLowerCase() === 'true';
        config.presentationConfig.repeatMain = parsed;
    }

    if (ldlBack !== null) {
        const parsed = ldlBack.toLowerCase() === 'true';
        config.presentationConfig.ldlBack = parsed;
    }

    if (providerLogo !== null) {
        const parsed = providerLogo.toLowerCase() === 'true';
        config.presentationConfig.providerLogo = parsed;
    }

    // sidebar.js starts nothing when the sidebar is off; this just takes it out of the layout.
    if (!sidebarEnabled) {
        document.querySelector('.sidebar').style.display = 'none';
    }

    if (config.presentationConfig.providerLogo != true) {
        ldlProviderLogo.style.display = 'none';
    }

    if (config.presentationConfig.main != true) {
        wallpaper.style.display = `none`
        mainSlides.style.display = `none`
    }

    if (config.presentationConfig.backgrounds != true) {
        wallpaper.style.display = `none`
    } else {
        wallpaper.style.display = `block`
    }

    if (config.presentationConfig.ldl != true) {
        ldlMainContent.style.display = `none`;
        ldlBrandingArea.style.borderRight = `none`;
        ldlBrandingArea.style.boxShadow = `none`;
        ldlBrandingArea.style.background = `none`;
    }
    if (config.presentationConfig.ldlBack === false) {
        ldlMainContent.style.display = `none`;
        ldlContainer.style.display = `none`
        ldlContainer.style.opacity = '0';
    }
}

window.onload = () => {
    viewportAspect();
    presentationType();
    initBackgrounds();
};

window.addEventListener('beforeunload', () => {
    if (localBackgroundObjectUrl) {
        URL.revokeObjectURL(localBackgroundObjectUrl);
        localBackgroundObjectUrl = null;
    }
});

const refreshInterval = config.refreshInterval * 60000;
if (refreshInterval > 0) {
    setTimeout(() => {
        window.location.reload(true);
    }, refreshInterval);
}
