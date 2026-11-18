// Highway shields and airport markers for the radar maps' label layer.
//
// OpenFreeMap's tiles only carry motorway route numbers at radar zooms (6-7); trunk highways and airports
// arrive one zoom deeper. So these layers read a source whose tiles are built from their children one zoom
// down (detail-tiles.js).
//
// Shields are drawn here on a canvas when MapLibre first asks for one ("styleimagemissing"), number
// included, so each design can place and colour its numerals its own way. OpenStreetMap tags every
// Canadian provincial route the same ("ca-provincial"), so the province is found from its outline
// (public/maps/provinces.json, built by scripts/build-provinces.py). Designs are simplified from each
// jurisdiction's real marker so they stay legible at their size on screen.

import { registerDetailTiles } from "./detail-tiles.js";

const SOURCE_ID = "openmaptiles-detail";
const TILEJSON = "https://tiles.openfreemap.org/planet";
const DETAIL_LAYERS = ["transportation_name", "aerodrome_label"];
// OpenFreeMap stops at zoom 14, so the deepest merged tile is 13.
const SOURCE = { type: "vector", tiles: ["detail://{z}/{x}/{y}"], minzoom: 0, maxzoom: 13 };
const PIXEL_RATIO = 2; // images are drawn at twice their on-screen size, for HiDPI output
const HEIGHT = 48; // CSS px
// Shield numerals use Interstate, the typeface of North American road signs.
const FONT = new FontFace("Interstate", "url(/fonts/Interstate-Regular.ttf)");
document.fonts.add(FONT);

/**
 * @typedef {object} Design
 * @property {"roundrect"|"shield"|"interstate"|"usroute"|"ellipse"} shape
 * @property {string} fill
 * @property {string} text numeral colour
 * @property {string} [stroke] outline colour
 * @property {number} [strokeWidth] outline width in CSS px at a 32 px tall shield (default 1.5); scales with HEIGHT
 * @property {string} [band] colour of a band across the top
 * @property {string} [backing] colour of a square behind the shape (US routes sit on black)
 * @property {"leaf"|"crown"} [emblem] a maple leaf above the number inside a taller sign, or a crown on top
 */

/** @type {Record<string, Design>} */
const DESIGNS = {
    generic: { shape: "roundrect", fill: "#ffffff", stroke: "#1b1b1b", text: "#1b1b1b" },
    "ca-generic": { shape: "shield", fill: "#ffffff", stroke: "#1b1b1b", text: "#1b1b1b" },
    tch: { shape: "roundrect", fill: "#00674e", stroke: "#ffffff", text: "#ffffff", emblem: "leaf" },
    "us-interstate": { shape: "interstate", fill: "#003f87", band: "#af1e2d", stroke: "#ffffff", text: "#ffffff" },
    "us-highway": { shape: "usroute", fill: "#ffffff", backing: "#000000", text: "#000000" },
    "us-state": { shape: "ellipse", fill: "#ffffff", stroke: "#1b1b1b", text: "#1b1b1b" },
    "CA-BC": { shape: "shield", fill: "#ffffff", stroke: "#1f4fa0", strokeWidth: 2, text: "#1f4fa0" },
    "CA-AB": { shape: "shield", fill: "#ffffff", stroke: "#000000", strokeWidth: 2, text: "#000000" },
    "CA-SK": { shape: "shield", fill: "#003882", stroke: "#ffffff", text: "#ffffff" },
    "CA-MB": { shape: "shield", fill: "#ffffff", stroke: "#000000", band: "#2e9a3e", text: "#000000" },
    "CA-ON": { shape: "shield", fill: "#ffffff", stroke: "#000000", text: "#000000", emblem: "crown" },
    "CA-QC": { shape: "roundrect", fill: "#14996b", stroke: "#ffffff", text: "#ffffff" },
    "qc-autoroute": { shape: "shield", fill: "#2a5db0", band: "#e4252c", stroke: "#ffffff", text: "#ffffff" },
    "CA-NB": { shape: "roundrect", fill: "#ffffff", stroke: "#0b6b3a", strokeWidth: 3, text: "#000000" },
    "CA-NS": { shape: "roundrect", fill: "#1f5bb5", stroke: "#ffffff", text: "#ffffff" },
    "CA-PE": { shape: "shield", fill: "#ffffff", stroke: "#118a4f", band: "#118a4f", text: "#000000" },
    "CA-NL": { shape: "roundrect", fill: "#ffffff", stroke: "#0b6b4f", strokeWidth: 3, text: "#000000" },
    "CA-YT": { shape: "roundrect", fill: "#ffffff", stroke: "#f39c12", strokeWidth: 2, text: "#f39c12" },
    "CA-NT": { shape: "roundrect", fill: "#0c2d6b", stroke: "#ffffff", text: "#ffffff" },
};

// Emblems in a 24-unit box, centred on (12, 12).
const LEAF = new Path2D("M12 1l1.5 3.5 2-.7-.5 4.2 3-2.5.8 2 2.7-.5-1 3 1.5 1-5 4 .6 2-4.8-.7V20h-1.6v-3.7l-4.8.7.6-2-5-4 1.5-1-1-3 2.7.5.8-2 3 2.5-.5-4.2 2 .7z");
const CROWN = new Path2D("M4 20h16v-3H4zM4 16h16l1-9-4.5 4L12 4 7.5 11 3 7z");
// Material Symbols "flight" (Apache 2.0).
const PLANE = new Path2D("M21 16v-2l-8-5V3.5c0-.83-.67-1.5-1.5-1.5S10 2.67 10 3.5V9l-8 5v2l8-2.5V19l-2 1.5V22l3.5-1 3.5 1v-1.5L13 19v-5.5l8 2.5z");

/** The outline of a shape in a w x h box at the origin. */
function outline(shape, w, h) {
    const path = new Path2D();
    switch (shape) {
        case "roundrect":
            path.roundRect(0, 0, w, h, h * 0.18);
            break;
        case "ellipse":
            path.ellipse(w / 2, h / 2, w / 2, h / 2, 0, 0, Math.PI * 2);
            break;
        case "shield": // flat top, straight sides, curving to a point
            path.moveTo(0, 0);
            path.lineTo(w, 0);
            path.lineTo(w, h * 0.5);
            path.quadraticCurveTo(w, h * 0.86, w / 2, h);
            path.quadraticCurveTo(0, h * 0.86, 0, h * 0.5);
            path.closePath();
            break;
        case "interstate": // dipped top, bulging sides
            path.moveTo(0, h * 0.12);
            path.quadraticCurveTo(w / 4, -h * 0.02, w / 2, h * 0.08);
            path.quadraticCurveTo(w * 0.75, -h * 0.02, w, h * 0.12);
            path.quadraticCurveTo(w * 1.04, h * 0.62, w / 2, h);
            path.quadraticCurveTo(-w * 0.04, h * 0.62, 0, h * 0.12);
            path.closePath();
            break;
        case "usroute": // dipped top, straight sides, pointed bottom
            path.moveTo(0, h * 0.12);
            path.quadraticCurveTo(w / 4, 0, w / 2, h * 0.1);
            path.quadraticCurveTo(w * 0.75, 0, w, h * 0.12);
            path.lineTo(w, h * 0.55);
            path.quadraticCurveTo(w, h * 0.88, w / 2, h);
            path.quadraticCurveTo(0, h * 0.88, 0, h * 0.55);
            path.closePath();
            break;
    }
    return path;
}

/** Draws one shield (design and route number) as image data for map.addImage. */
function drawShield(design, ref) {
    const r = PIXEL_RATIO;
    const leaf = design.emblem === "leaf";
    const emblemHeight = design.emblem === "crown" ? HEIGHT * 0.36 : 0;
    const bodyHeight = HEIGHT * r * (leaf ? 1.4 : 1);
    const pointed = ["shield", "interstate", "usroute"].includes(design.shape);
    // Pointed shapes lose width towards the bottom, so their numerals sit higher and get more room.
    const fontSize = (ref.length >= 3 ? 0.7 : 0.82) * HEIGHT * r * (leaf ? 0.8 : 1);
    const scratch = new OffscreenCanvas(1, 1).getContext("2d");
    scratch.font = `${fontSize}px ${FONT.family}`;
    const textWidth = scratch.measureText(ref).width;
    const width = Math.ceil(Math.max(HEIGHT * r * (design.shape === "ellipse" ? 1.15 : 0.95), textWidth * (pointed ? 1.25 : 1.12) + 4 * r));
    const pad = 2 * r * (HEIGHT / 32); // room for the outline
    const backing = design.backing ? 2 * r : 0;
    const canvas = new OffscreenCanvas(width + 2 * (pad + backing), Math.ceil(bodyHeight + emblemHeight * r) + 2 * (pad + backing));
    const ctx = canvas.getContext("2d");

    ctx.translate(pad + backing, pad + backing + emblemHeight * r);
    if (design.backing) {
        ctx.fillStyle = design.backing;
        ctx.beginPath();
        ctx.roundRect(-backing, -backing, width + 2 * backing, bodyHeight + 2 * backing, 3 * r);
        ctx.fill();
    }

    const shape = outline(design.shape, width, bodyHeight);
    ctx.fillStyle = design.fill;
    ctx.fill(shape);
    if (design.band) {
        ctx.save();
        ctx.clip(shape);
        ctx.fillStyle = design.band;
        ctx.fillRect(0, 0, width, bodyHeight * 0.2);
        ctx.restore();
    }
    if (design.stroke) {
        ctx.lineWidth = (design.strokeWidth ?? 1.5) * r * (HEIGHT / 32);
        ctx.strokeStyle = design.stroke;
        ctx.lineJoin = "round";
        ctx.stroke(shape);
    }

    if (leaf) {
        // The Trans-Canada leaf, in the top of the sign.
        const size = bodyHeight * 0.47;
        ctx.save();
        ctx.translate(width / 2 - size / 2, bodyHeight * 0.04);
        ctx.scale(size / 24, size / 24);
        ctx.fillStyle = design.text;
        ctx.fill(LEAF);
        ctx.restore();
    } else if (design.emblem === "crown") {
        // Ontario's crown, standing on top of the shield.
        const size = emblemHeight * r * 1.15;
        ctx.save();
        ctx.translate(width / 2 - size / 2, -size * 0.92);
        ctx.scale(size / 24, size / 24);
        ctx.fillStyle = "#000000";
        ctx.strokeStyle = "#ffffff";
        ctx.lineWidth = 3;
        ctx.stroke(CROWN);
        ctx.fill(CROWN);
        ctx.restore();
    }

    ctx.fillStyle = design.text;
    ctx.font = `${fontSize}px ${FONT.family}`;
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    const textY = leaf ? bodyHeight * 0.75 : bodyHeight * (pointed ? 0.47 : 0.54) + (design.band ? bodyHeight * 0.1 : 0);
    ctx.fillText(ref, width / 2, textY);
    return ctx.getImageData(0, 0, canvas.width, canvas.height);
}

/** A white plane on a dark disc, for major airports. */
function drawAirport() {
    const size = HEIGHT * 0.65 * PIXEL_RATIO;
    const canvas = new OffscreenCanvas(size, size);
    const ctx = canvas.getContext("2d");
    ctx.fillStyle = "rgba(10, 24, 44, 0.85)";
    ctx.strokeStyle = "rgba(255, 255, 255, 0.85)";
    ctx.lineWidth = size * 0.05;
    ctx.beginPath();
    ctx.arc(size / 2, size / 2, size / 2 - ctx.lineWidth, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();
    ctx.translate(size / 2, size / 2);
    ctx.rotate(Math.PI / 4);
    ctx.scale(size / 34, size / 34);
    ctx.translate(-11.5, -12);
    ctx.fillStyle = "#ffffff";
    ctx.fill(PLANE);
    return ctx.getImageData(0, 0, size, size);
}

/** The design for a route, as a MapLibre expression, picking provincial designs by outline. */
function designExpression(provinces) {
    const within = (code) => ["within", { type: "MultiPolygon", coordinates: provinces[code] }];
    const provincial = ["case"];
    for (const code of Object.keys(provinces)) {
        if (code === "CA-QC") {
            provincial.push(["all", ["==", ["get", "network"], "ca-provincial-arterial"], within(code)], "qc-autoroute");
        }
        provincial.push(within(code), code);
    }
    provincial.push("ca-generic");
    return [
        "match", ["get", "network"],
        "ca-transcanada", "tch",
        "us-interstate", "us-interstate",
        "us-highway", "us-highway",
        "us-state", "us-state",
        ["ca-provincial", "ca-provincial-arterial"], provincial,
        "generic",
    ];
}

/** Loads what the symbol layers need (the province outlines and the shield font) and registers their
 * tile source. Call once. */
export async function loadMapSymbols() {
    const [provinces, tilejson] = await Promise.all([
        fetch("/maps/provinces.json").then((res) => res.json()),
        fetch(TILEJSON).then((res) => res.json()),
        FONT.load(),
    ]);
    registerDetailTiles(tilejson.tiles[0], DETAIL_LAYERS);
    return { provinces };
}

/** Adds the shield and airport layers to a label style, below the place names. */
export function addMapSymbols(style, { provinces }) {
    style.sources[SOURCE_ID] = SOURCE;
    const layers = [
        {
            id: "road-shield",
            type: "symbol",
            source: SOURCE_ID,
            "source-layer": "transportation_name",
            minzoom: 5,
            filter: [
                "all",
                ["has", "ref"],
                ["<=", ["get", "ref_length"], 4],
                ["!", ["in", ";", ["get", "ref"]]], // concurrent routes ("11;16") are signed separately nearby
                ["match", ["get", "class"], ["motorway", "trunk", "primary"], true, false],
            ],
            layout: {
                "symbol-placement": "line",
                "symbol-spacing": 500,
                "symbol-avoid-edges": true,
                "icon-image": ["concat", "shield|", designExpression(provinces), "|", ["get", "ref"]],
                "icon-rotation-alignment": "viewport",
                "icon-padding": 6,
            },
        },
        {
            id: "airport",
            type: "symbol",
            source: SOURCE_ID,
            "source-layer": "aerodrome_label",
            minzoom: 5,
            filter: ["all", ["==", ["get", "class"], "international"], ["has", "iata"]],
            layout: {
                "icon-image": "symbol|airport",
                // Major airports sit right beside their city, whose name would otherwise always win the
                // space. The plane may overlap it; the code drops out when there's no room.
                "icon-allow-overlap": true,
                "icon-ignore-placement": true,
                "text-field": ["get", "iata"],
                "text-font": ["Poppins Bold"],
                "text-size": 15,
                "text-anchor": "top",
                "text-offset": [0, 1.1],
                "text-optional": true,
            },
            paint: {
                "text-color": "#ffffff",
                "text-halo-color": "rgba(0, 0, 0, 0.75)",
                "text-halo-width": 1.5,
            },
        },
    ];
    const placeLabels = style.layers.findIndex((layer) => layer.source === "openmaptiles" && layer["source-layer"] === "place");
    style.layers.splice(placeLabels, 0, ...layers);
}

/** Draws shields and the airport marker on demand, the first time a map asks for each. */
export function provideMapSymbols(map) {
    map.on("styleimagemissing", ({ id }) => {
        let image;
        if (id === "symbol|airport") {
            image = drawAirport();
        } else if (id.startsWith("shield|")) {
            const [, design, ref] = id.split("|");
            image = drawShield(DESIGNS[design], ref);
        } else {
            return;
        }
        map.addImage(id, image, { pixelRatio: PIXEL_RATIO });
    });
}
