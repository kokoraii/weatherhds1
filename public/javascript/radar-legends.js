// Colour keys for each radar data source, for the main radar's info bubble. Colours run light to heavy and
// are sampled from each source's own legend (WMS GetLegendGraphic), so they match what the feed draws.

/** @typedef {{ label: string, colors: string[], ends?: [string, string] }} LegendSection */

/** @type {Record<string, LegendSection[]>} */
const LEGENDS = {
    twc: [
        { label: "Rain", colors: ["#63eb63", "#0e681a", "#fbeb02", "#ee6d02", "#a90503"] },
        { label: "Ice", colors: ["#bca5f0", "#866dbd", "#5d469b"] },
        { label: "Snow", colors: ["#8af8ff", "#529faa", "#36737f", "#0d3140"] },
        { label: "Sleet", colors: ["#ffa0cf", "#d1649a", "#a42665"] },
    ],
    // Environment Canada's precipitation-rate ramp: 0.5 to 125 mm/h (snow layers share the colours, in cm/h).
    eccc: [
        {
            label: "Precipitation",
            colors: ["#6cbcfe", "#00e092", "#00b000", "#fefe00", "#fe9800", "#fe0026", "#9d31c9"],
            ends: ["Light", "Heavy"],
        },
    ],
    // NWS base reflectivity: 10 to 65 dBZ.
    nws: [
        {
            label: "Precipitation",
            colors: ["#5188b9", "#57c3b7", "#0caf11", "#0a730c", "#f4ca17", "#d00808", "#f174fd"],
            ends: ["Light", "Heavy"],
        },
    ],
};

// TWC products whose colours are the TWC precipitation key. Its satellite products have no key.
const TWC_RADAR = new Set(["radar", "twcRadarMosaic", "twcRadarHcMosaic", "satrad"]);

/** The data source a product id comes from, or null for imagery without a colour key. */
function sourceOf(product) {
    const prefix = product.split(":", 1)[0];
    if (prefix !== product) return prefix;
    return TWC_RADAR.has(product) ? "twc" : null;
}

/**
 * Fills a legend container with the keys of every source in a layer stack (bottom first, as in config),
 * top layer's key first, separated by dividers.
 * @param {HTMLElement} container
 * @param {{ product: string }[]} layers
 */
export function renderRadarLegend(container, layers) {
    const sources = [...new Set(layers.map(layer => sourceOf(layer.product)).filter(Boolean))].reverse();
    const sections = sources.flatMap(source => LEGENDS[source]);

    container.replaceChildren(...sections.flatMap((section, index) => {
        const element = document.createElement("div");
        element.className = "radar-legend-section";

        const label = document.createElement("span");
        label.className = "radar-legend-label";
        label.textContent = section.label;

        const swatches = document.createElement("div");
        swatches.className = "radar-swatches";
        swatches.append(...section.colors.map(color => {
            const swatch = document.createElement("span");
            swatch.className = "radar-swatch";
            swatch.style.background = color;
            return swatch;
        }));

        element.append(label, swatches);
        if (section.ends) {
            const ends = document.createElement("div");
            ends.className = "radar-legend-ends";
            ends.append(...section.ends.map(text => Object.assign(document.createElement("span"), { textContent: text })));
            element.append(ends);
        }
        if (index === 0) return [element];
        const divider = document.createElement("div");
        divider.className = "radar-legend-divider";
        return [divider, element];
    }));
}
