import { config } from "../config.js";
import { renderRadarLegend } from "./radar-legends.js";
import { setSteadyText } from "./steady-digits.js";

// On-screen radars. The picture is three stacked layers inside each container:
//   base map (land, water, roads) < radar canvas (the Rust engine in /radar) < label map (borders, place names)
// Both maps stay idle unless the camera moves, so a 60 fps radar doesn't redraw the map, and the browser
// composites the layers. This file only glues the engine to MapLibre and the DOM. Frames, smoothing and
// interpolation all live in the engine.

const perf = config.performance ?? {};

const DEFAULTS = {
    mapStyle: "/maps/radar-dark.json", // MapLibre style on OpenFreeMap tiles
    layers: config.radar.layers.main, // bottom first
    totalFrames: Number(perf.mainRadarFrames ?? 48), // of the top layer; the others cover the same time
    frameDelay: Number(perf.radarFrameDelay ?? 150),
    loopGap: 1000,
    maxLoops: Number(perf.mainRadarLoops ?? 12),
    maxTTL: 6,
    flyDuration: 2500,
    labelTextSize: 40,
    sizeHint: [1, 1], // expected share of the window, used to plan tiles while the container is hidden
    tileDetail: 1, // zoom levels above the map's to fetch: 1 draws tiles 1:1, 0 at 2x for a quarter of the memory
};

const log = () => `[radar.js] | ${new Date().toLocaleString()} |`;

const formatTime = (ts) => {
    const d = new Date(ts * 1000);
    let h = d.getHours();
    const m = d.getMinutes();
    const ampm = h >= 12 ? "PM" : "AM";
    h = h % 12 || 12;
    return `${h}:${m < 10 ? "0" + m : m} ${ampm}`;
};

// Radars with maps on the page, drawn from the one animation loop.
const activeRadars = new Set();

let enginePromise = null;

// Loads the wasm engine once. Imported dynamically so a missing or broken build costs the radar overlay,
// not every module that imports this file.
function getEngine() {
    enginePromise ??= (async () => {
        const [{ default: init, RadarEngine }, keys] = await Promise.all([
            import("/wasm/weatherhds_radar.js"),
            fetch("/config/keys").then((res) => {
                if (!res.ok) throw new Error(`Failed to fetch radar keys: ${res.status}`);
                return res.json();
            }),
        ]);
        await init();
        const backend = new URLSearchParams(window.location.search).get("radarBackend");
        const engine = await RadarEngine.create(
            { ...config.radar, ...(backend ? { backend } : {}) },
            keys.twcApiKey,
        );
        window.radarInventory = () => engine.inventory(); // for checking on loading from the console

        const loop = (now) => {
            engine.pump(now);
            for (const radar of activeRadars) radar.drawFrame(now);
            requestAnimationFrame(loop);
        };
        requestAnimationFrame(loop);
        return engine;
    })().catch((err) => {
        enginePromise = null;
        throw err;
    });
    return enginePromise;
}

// County-level borders, from our own tile archive (scripts/build-borders.sh): the map's tiles only carry
// them when zoomed in much further than the radars go.
const BORDERS_SOURCE = { type: "vector", url: `pmtiles://${window.location.origin}/maps/borders.pmtiles` };
const PMTILES_READER = "https://unpkg.com/pmtiles@4.5.0/dist/pmtiles.js";

let bordersPromise = null;

// Loads the archive reader once. Resolves to whether it worked: if it can't load, the maps go without
// county borders rather than taking down everything that imports this file.
function loadBordersReader() {
    bordersPromise ??= new Promise((resolve, reject) => {
        const script = document.createElement("script");
        script.src = PMTILES_READER;
        script.onload = resolve;
        script.onerror = () => reject(new Error(`could not load ${PMTILES_READER}`));
        document.head.append(script);
    }).then(() => {
        maplibregl.addProtocol("pmtiles", new pmtiles.Protocol().tile);
        return true;
    }).catch((err) => {
        console.warn(log(), "County borders unavailable:", err.message);
        return false;
    });
    return bordersPromise;
}

let stylePromise = null;
const loadStyle = (url) => (stylePromise ??= fetch(url).then((res) => res.json()));

// Cuts the map style where the old radar layers used to be inserted: borders and labels go above the radar.
function splitStyle(style, labelTextSize, hasBorders) {
    const split = style.layers.findIndex(
        (l) => l.type === "symbol" || l.id.includes("boundary") || l.id.includes("border"),
    );
    const labels = structuredClone(style.layers.slice(split)).filter((layer) => hasBorders || layer.source !== "borders");
    for (const layer of labels) {
        if (layer.type === "symbol" && layer.layout?.["text-size"]) {
            layer.layout["text-size"] = layer.id === "road-shield" ? 18 : labelTextSize;
        }
    }
    return {
        base: { ...style, layers: style.layers.slice(0, split) },
        labels: { ...style, sources: { ...style.sources, ...(hasBorders && { borders: BORDERS_SOURCE }) }, layers: labels },
    };
}

function coverElement(el, zIndex) {
    el.style.cssText = `position:absolute;inset:0;width:100%;height:100%;pointer-events:none;z-index:${zIndex}`;
    return el;
}

export class RadarMap {
    #baseMap = null;
    #labelMap = null;
    #labelsEl = null;
    #canvas = coverElement(document.createElement("canvas"), 1); // outlives destroy(), and its GPU context with it
    #surfacePromise = null;
    #surface = null;
    #size = { width: 0, height: 0 };
    #ttl = 0;
    #pendingInit = null;
    #resizeObserver = null;
    #frameTs = null;
    #containerId;
    #timeEl;
    #opts;

    constructor(containerId, options = {}) {
        this.#containerId = containerId;
        this.#opts = { ...DEFAULTS, ...options };
        this.#timeEl = options.timeElement ?? null;
    }

    get isActive() { return this.#baseMap !== null; }
    get ttl() { return this.#ttl; }

    #getContainer() {
        return document.getElementById(this.#containerId);
    }

    #measure() {
        const el = this.#getContainer();
        this.#size = { width: el.clientWidth, height: el.clientHeight };
    }

    #isVisible() {
        return this.#size.width > 0 && this.#size.height > 0;
    }

    #getSurface() {
        this.#surfacePromise ??= (async () => {
            const engine = await getEngine();
            const { frameDelay, loopGap, maxLoops } = this.#opts;
            this.#surface = await engine.createSurface(this.#containerId, this.#canvas, {
                frameDelay,
                loopGap,
                maxLoops: Number.isFinite(maxLoops) ? maxLoops : undefined,
            });
            console.log(log(), `Radar engine on ${this.#surface.backend} for [${this.#containerId}]`);
            return this.#surface;
        })().catch((err) => {
            // The map can still run without its radar; the next init tries again.
            console.error(log(), "Radar engine unavailable:", err);
            this.#surfacePromise = null;
            return null;
        });
        return this.#surfacePromise;
    }

    // The request the engine turns into an interest list per layer.
    #view(lat, lon, zoom) {
        const { layers, sizeHint, tileDetail, totalFrames } = this.#opts;
        const width = this.#size.width || window.innerWidth * sizeHint[0];
        const height = this.#size.height || window.innerHeight * sizeHint[1];
        return { layers, frames: totalFrames, detail: tileDetail, lat, lon, zoom, width, height };
    }

    #observeSize() {
        if (this.#resizeObserver) return;
        // Acts on the next frame: creating or resizing the map inside the callback changes the observed
        // layout again and raises "ResizeObserver loop completed with undelivered notifications".
        this.#resizeObserver = new ResizeObserver(() => requestAnimationFrame(() => {
            this.#measure();
            if (!this.#isVisible()) return;

            if (this.#pendingInit) {
                const { lat, lon, zoom } = this.#pendingInit;
                this.init(lat, lon, zoom);
                return;
            }
            this.resize();
        }));
        this.#resizeObserver.observe(this.#getContainer());
    }

    async #createMaps(center, zoom) {
        const container = this.#getContainer();
        const [style, hasBorders] = await Promise.all([loadStyle(this.#opts.mapStyle), loadBordersReader()]);
        const { base, labels } = splitStyle(style, this.#opts.labelTextSize, hasBorders);
        const mapOptions = {
            center,
            zoom,
            interactive: false,
            attributionControl: false,
            fadeDuration: 0,
            renderWorldCopies: false,
        };

        this.#baseMap = new maplibregl.Map({ ...mapOptions, container, style: base });
        container.append(this.#canvas);
        this.#labelsEl = coverElement(document.createElement("div"), 2);
        container.append(this.#labelsEl);
        this.#labelMap = new maplibregl.Map({ ...mapOptions, container: this.#labelsEl, style: labels });

        this.#baseMap.on("move", () => {
            this.#labelMap.jumpTo({ center: this.#baseMap.getCenter(), zoom: this.#baseMap.getZoom() });
        });
        activeRadars.add(this);
    }

    async init(lat, lon, zoom) {
        if (!lat || !lon) {
            console.warn(log(), "Invalid coordinates");
            return;
        }

        this.#observeSize();
        this.#measure();
        const surface = await this.#getSurface();

        if (!this.#isVisible()) {
            // Off screen for now: fetching starts anyway, so the loop is ready when it airs.
            surface?.want(this.#view(lat, lon, zoom), true);
            this.#pendingInit = { lat, lon, zoom };
            return;
        }
        this.#pendingInit = null;

        if (this.#baseMap && this.#ttl >= this.#opts.maxTTL) this.destroy();

        if (this.#baseMap) {
            this.resize();
            this.#baseMap.jumpTo({ center: [lon, lat], zoom });
            this.#ttl++;
        } else {
            await this.#createMaps([lon, lat], zoom);
        }
        surface?.show(this.#view(lat, lon, zoom));
    }

    // Starts fetching a view this radar will show later, behind anything on screen sooner.
    async prefetch(lat, lon, zoom) {
        const surface = await this.#getSurface();
        surface?.want(this.#view(lat, lon, zoom), false);
    }

    async flyTo(lat, lon, zoom) {
        if (!this.#baseMap) return;

        const surface = await this.#getSurface();
        surface?.show(this.#view(lat, lon, zoom));

        this.#baseMap.flyTo({
            center: [lon, lat],
            zoom,
            duration: this.#opts.flyDuration,
            essential: true,
        });

        await new Promise((resolve) => this.#baseMap.once("moveend", resolve));
        this.#ttl++;
    }

    resize() {
        this.#baseMap?.resize();
        this.#labelMap?.resize();
    }

    destroy() {
        if (!this.#baseMap) return;
        activeRadars.delete(this);
        this.#labelMap.remove();
        this.#labelsEl.remove();
        this.#canvas.remove();
        this.#baseMap.remove();
        this.#baseMap = null;
        this.#labelMap = null;
        this.#labelsEl = null;
        this.#ttl = 0;
        this.#pendingInit = null;
    }

    // Called from the animation loop.
    drawFrame(now) {
        if (!this.#surface || !this.#isVisible()) return;
        const { lat, lng } = this.#baseMap.getCenter();
        const { width, height } = this.#size;
        const ts = this.#surface.render(now, lat, lng, this.#baseMap.getZoom(), width, height, window.devicePixelRatio);
        if (ts !== undefined && ts !== this.#frameTs) {
            this.#frameTs = ts;
            if (this.#timeEl) setSteadyText(this.#timeEl, formatTime(ts));
        }
    }
}

const mainRadar = new RadarMap("radar-map", {
    timeElement: document.getElementById("main-radar-time"),
});
renderRadarLegend(document.getElementById("main-radar-legend"), DEFAULTS.layers);

export function resizeRadar() { mainRadar.resize(); }

export async function drawMap(lat, lon, zoom) {
    await mainRadar.init(lat, lon, zoom);
}
