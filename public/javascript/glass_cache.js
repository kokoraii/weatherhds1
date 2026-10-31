import { config } from "../config.js";

const perf = config.performance ?? {};

const GLASS_SELECTOR = [
    ".sidebar",
    ".slide-info-group",
    ".current-location-group",
    ".upnext-location-entry",
    ".main-upnext-carousel-entry",
    ".main-slide-panel",
    ".main-current-condtions-box",
    ".ldl-weather",
    ".topbar",
    ".ldl-date-time-container",
    ".provider-logo-container",
    ".cityticker",
    ".ldl-bulletin-crawl",
    ".ldl-carousel-container",
    ".ldl-carousel-current",
    ".ldl-carousel-location-entry",
    ".ldl-hourly-period",
    ".ldl-shortterm-period-summary",
    ".ldl-daily-period-summary",
    ".left-container-radar",
    ".right-container-locations",
    ".national-current-labels",
].join(",");

const root = document.documentElement;
const body = document.body;
const view = document.querySelector(".view");
const wallpaper = document.querySelector(".wallpaper");

let cachedGlassUrl = "";
let sourceUrl = "";
let renderToken = 0;
let layoutRaf = 0;
let rebuildTimer = 0;

// Last background offset written to each glass node, so unchanged nodes aren't restyled.
const lastGlassLayout = new WeakMap();
// Glass nodes appearing (display none -> shown) or resizing re-trigger the layout pass.
const glassResizeObserver = new ResizeObserver(() => scheduleGlassLayout());
const observedGlass = new WeakSet();

if (perf.disableLiveBackdropBlur !== false) {
    body.classList.add("performance-static-glass");
}

if (perf.disableLiveBackdropBlur !== false && perf.cachedBackdropGlass !== false && view && wallpaper) {
    body.classList.add("performance-cached-glass");
    observeWallpaper();
    scheduleGlassRebuild();
    scheduleGlassLayout();
}

function observeWallpaper() {
    const observer = new MutationObserver(() => scheduleGlassRebuild());
    observer.observe(wallpaper, { attributes: true, attributeFilter: ["style", "class"] });

    window.addEventListener("resize", () => {
        scheduleGlassRebuild();
        scheduleGlassLayout();
    }, { passive: true });

    const layoutObserver = new MutationObserver(() => scheduleGlassLayout());
    layoutObserver.observe(view, { attributes: true, attributeFilter: ["style", "class"] });

    // Panels only move when something moves them: slides and LDL entries animate or transition in,
    // so re-align once that motion settles (coalesced into one pass per frame).
    document.addEventListener("transitionend", scheduleGlassLayout, { passive: true });
    document.addEventListener("animationend", scheduleGlassLayout, { passive: true });
}

function scheduleGlassRebuild() {
    clearTimeout(rebuildTimer);
    rebuildTimer = setTimeout(() => {
        rebuildGlassCache().catch((err) => {
            console.warn("[glass_cache.js] Cached glass disabled:", err.message);
            body.classList.remove("performance-cached-glass");
        });
    }, 80);
}

function scheduleGlassLayout() {
    if (layoutRaf) return;
    layoutRaf = requestAnimationFrame(() => {
        layoutRaf = 0;
        syncGlassLayout();
    });
}

async function rebuildGlassCache() {
    const nextSourceUrl = extractCssUrl(wallpaper.style.backgroundImage || getComputedStyle(wallpaper).backgroundImage);
    if (!nextSourceUrl || nextSourceUrl === sourceUrl) {
        syncGlassLayout();
        return;
    }

    sourceUrl = nextSourceUrl;
    const token = ++renderToken;
    const image = await loadImage(nextSourceUrl);
    if (token !== renderToken) return;

    const rect = view.getBoundingClientRect();
    const layoutWidth = view.offsetWidth || rect.width;
    const layoutHeight = view.offsetHeight || rect.height;
    const scale = clampNumber(perf.glassCacheScale, 0.25, 1, 0.42);
    const width = Math.max(320, Math.round(layoutWidth * scale));
    const height = Math.max(240, Math.round(layoutHeight * scale));
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = height;

    const ctx = canvas.getContext("2d", { alpha: true });
    if (!ctx) throw new Error("canvas 2d context unavailable");

    const blurPx = clampNumber(perf.glassCacheBlurPx, 8, 80, 28);
    const overscan = Math.ceil(blurPx * 2);
    const cover = coverRect(image.naturalWidth, image.naturalHeight, width + overscan * 2, height + overscan * 2);

    ctx.filter = `blur(${blurPx}px) saturate(185%) brightness(70%)`;
    ctx.drawImage(
        image,
        cover.sx,
        cover.sy,
        cover.sw,
        cover.sh,
        -overscan,
        -overscan,
        width + overscan * 2,
        height + overscan * 2
    );
    ctx.filter = "none";
    ctx.fillStyle = "rgba(2, 8, 18, 0.46)";
    ctx.fillRect(0, 0, width, height);
    const highlight = ctx.createLinearGradient(0, 0, 0, height);
    highlight.addColorStop(0, "rgba(255, 255, 255, 0.10)");
    highlight.addColorStop(0.36, "rgba(255, 255, 255, 0.02)");
    highlight.addColorStop(1, "rgba(0, 0, 0, 0.16)");
    ctx.fillStyle = highlight;
    ctx.fillRect(0, 0, width, height);

    const blob = await canvasToBlob(canvas);
    if (token !== renderToken) return;

    const nextGlassUrl = URL.createObjectURL(blob);
    const previousGlassUrl = cachedGlassUrl;
    cachedGlassUrl = nextGlassUrl;
    root.style.setProperty("--cached-glass-bg", `url("${nextGlassUrl}")`);
    root.style.setProperty("--cached-glass-source-w", `${layoutWidth}px`);
    root.style.setProperty("--cached-glass-source-h", `${layoutHeight}px`);
    body.classList.add("performance-cached-glass");
    syncGlassLayout();

    if (previousGlassUrl) URL.revokeObjectURL(previousGlassUrl);
}

function syncGlassLayout() {
    if (!view) return;
    const viewRect = view.getBoundingClientRect();
    const scaleX = view.offsetWidth ? viewRect.width / view.offsetWidth : 1;
    const scaleY = view.offsetHeight ? viewRect.height / view.offsetHeight : 1;
    const layoutWidth = view.offsetWidth || viewRect.width;
    const layoutHeight = view.offsetHeight || viewRect.height;
    const glassNodes = document.querySelectorAll(GLASS_SELECTOR);

    for (const node of glassNodes) {
        if (!observedGlass.has(node)) {
            observedGlass.add(node);
            glassResizeObserver.observe(node);
        }

        const rect = node.getBoundingClientRect();
        if (rect.width < 1 || rect.height < 1) continue;

        const x = `${(viewRect.left - rect.left) / scaleX}px`;
        const y = `${(viewRect.top - rect.top) / scaleY}px`;
        const key = `${x} ${y} ${layoutWidth} ${layoutHeight}`;
        if (lastGlassLayout.get(node) === key) continue;
        lastGlassLayout.set(node, key);

        node.style.setProperty("--glass-bg-x", x);
        node.style.setProperty("--glass-bg-y", y);
        node.style.setProperty("--glass-bg-w", `${layoutWidth}px`);
        node.style.setProperty("--glass-bg-h", `${layoutHeight}px`);
    }
}

function extractCssUrl(value) {
    const match = String(value || "").match(/url\((["']?)(.*?)\1\)/);
    return match?.[2] || "";
}

function loadImage(url) {
    return new Promise((resolve, reject) => {
        const image = new Image();
        image.decoding = "async";
        if (isCrossOriginHttpUrl(url)) image.crossOrigin = "anonymous";
        image.onload = () => resolve(image);
        image.onerror = () => reject(new Error(`failed to load ${url}`));
        image.src = url;
    });
}

function isCrossOriginHttpUrl(url) {
    try {
        const parsed = new URL(url, window.location.href);
        return (parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.origin !== window.location.origin;
    } catch {
        return false;
    }
}

function canvasToBlob(canvas) {
    return new Promise((resolve, reject) => {
        canvas.toBlob((blob) => {
            if (blob) {
                resolve(blob);
            } else {
                reject(new Error("cached glass render produced no blob"));
            }
        }, "image/webp", 0.82);
    });
}

function coverRect(imageWidth, imageHeight, targetWidth, targetHeight) {
    const imageRatio = imageWidth / imageHeight;
    const targetRatio = targetWidth / targetHeight;
    if (imageRatio > targetRatio) {
        const sw = imageHeight * targetRatio;
        return { sx: (imageWidth - sw) / 2, sy: 0, sw, sh: imageHeight };
    }
    const sh = imageWidth / targetRatio;
    return { sx: 0, sy: (imageHeight - sh) / 2, sw: imageWidth, sh };
}

function clampNumber(value, min, max, fallback) {
    const numeric = Number(value);
    if (!Number.isFinite(numeric)) return fallback;
    return Math.min(max, Math.max(min, numeric));
}
