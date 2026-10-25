import { config } from "../config.js";

// config.animatedIconFormat chooses how the animated weather icons load. "svg" uses the SMIL originals
// in /graphics/smil; "avif" uses the files scripts/svg2avif.py writes to /graphics/avif (same names).
const useAvif = String(config.animatedIconFormat ?? "svg").trim().replace(/^\./, "").toLowerCase() === "avif";

export function weatherIconUrl(dir, file) {
    if (dir !== "smil" || !useAvif) return `/graphics/${dir}/${file}`;
    return `/graphics/avif/${file.replace(/\.svg(?=$|[?#])/i, ".avif")}`;
}

if (useAvif) {
    // Icons written straight into index.html.
    const swapMarkup = () => {
        for (const img of document.querySelectorAll('img[src^="/graphics/smil/"]')) {
            img.setAttribute("src", img.getAttribute("src").replace(/^\/graphics\/smil\/([^/?#]+)\.svg/i, "/graphics/avif/$1.avif"));
        }
    };
    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", swapMarkup, { once: true });
    } else {
        swapMarkup();
    }

    // A partly converted folder still works: any icon missing its converted file falls back to the SVG.
    window.addEventListener("error", (event) => {
        const img = event.target;
        if (!(img instanceof HTMLImageElement)) return;
        const fallback = img.src.replace(/\/graphics\/avif\/([^/?#]+)\.avif(?=$|[?#])/i, "/graphics/smil/$1.svg");
        if (fallback !== img.src) img.src = fallback;
    }, true);
}
