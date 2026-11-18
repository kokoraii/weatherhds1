import { locationConfig } from "../config.js";

// Limits the radar maps' city and town labels to places the config mentions anywhere in locationConfig:
// local and secondary locations, the LDL, the city ticker, highway routes and the national regions.
// Province, state and country labels are left alone.

const SETTLEMENT_LAYERS = new Set(["settlement-major-label", "settlement-minor-label"]);
// The same text the label layers show.
const LABEL = ["coalesce", ["get", "name:en"], ["get", "name_en"], ["get", "name"]];
// "Montreal" in the config still matches "Montréal" on the map.
const LOOSE = ["collator", { "case-sensitive": false, "diacritic-sensitive": false }];

/** Every place name in locationConfig: "Prince Albert, SK, Canada" and "Prince Albert" both give "Prince Albert". */
function configPlaceNames() {
    const names = new Set();
    const visit = (value, key) => {
        if (typeof value === "string") {
            // "City, PR" anywhere, plus bare names where the key says it's a place.
            if (/^[^,]+,\s*[A-Z]{2}\b/.test(value) || key === "displayName" || key === "majorCities") {
                const name = value.split(",")[0].trim();
                if (name) names.add(name);
            }
        } else if (Array.isArray(value)) {
            for (const item of value) visit(item, key);
        } else if (value && typeof value === "object") {
            for (const [childKey, child] of Object.entries(value)) visit(child, childKey);
        }
    };
    visit(locationConfig);
    // Map data spells some apostrophes curly ("Fort Qu’Appelle").
    for (const name of [...names]) if (name.includes("'")) names.add(name.replaceAll("'", "’"));
    return [...names];
}

/** Narrows the settlement label layers of a style's layer list to the config's places. Mutates the layers. */
export function limitPlaceLabels(layers) {
    const named = ["any", ...configPlaceNames().map((name) => ["==", LABEL, name, LOOSE])];
    for (const layer of layers) {
        if (!SETTLEMENT_LAYERS.has(layer.id)) continue;
        // Config places can be villages, which the minor layer otherwise hides until zoom 8.
        const classes = layer.id === "settlement-major-label" ? ["city"] : ["town", "village", "hamlet"];
        layer.filter = ["all", ["match", ["get", "class"], classes, true, false], named];
    }
}
