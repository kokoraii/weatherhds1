import { weatherIcons, locationConfig, serverConfig, config, displayUnits } from "../config.js";
import { requestWxData, requestAlertData } from "./data.js";
import { weatherIconUrl } from "./icons.js";

const SCROLL_SPEED_PX_PER_MS = 0.24;
const STATIC_MSG_DURATION_MS = 12000;
const FETCH_CONCURRENCY = Math.max(1, Number(config.performance?.cityTickerFetchConcurrency ?? 4));
const iconDir = config.staticIcons ? "static" : "smil";
const units = displayUnits[serverConfig.units] ?? displayUnits["m"];

function resolveIcon(iconCode, dayOrNight) {
    const arr = weatherIcons[String(iconCode ?? 44)] ?? weatherIcons["44"];
    return arr[dayOrNight === "D" ? 0 : 1] ?? "not-available.svg";
}

function buildAlertBadge(color) {
    const badge = document.createElement("span");
    badge.className = "cityticker-alert-badge";
    badge.style.color = color;
    badge.textContent = "\u26a0";
    return badge;
}

function buildCurrentEntry(locationStr, current, alertData = null) {
    const entry = document.createElement("div");
    entry.className = "cityticker-entry";

    const city = document.createElement("div");
    city.className = "cityticker-entry-city";
    city.textContent = locationStr.split(",")[0];
    if (alertData && config.cityTicker?.severeAlertIcon) city.prepend(buildAlertBadge(alertColorFor(alertData)));

    const temp = document.createElement("div");
    temp.className = "cityticker-entry-temp";
    temp.textContent = `${current.temperature}${units.endingTemp}`;

    const cond = document.createElement("div");
    cond.className = "cityticker-entry-condition";
    cond.textContent = current.wxPhraseMedium ?? current.wxPhraseLong ?? "";

    const icon = document.createElement("img");
    icon.className = "cityticker-entry-icon";
    icon.src = weatherIconUrl(iconDir, resolveIcon(current.iconCode, current.dayOrNight));

    entry.append(city, temp, cond, icon);
    return entry;
}

function buildForecastEntry(locationStr, forecast, dayIndex, hasAlert = false) {
    const dpOffset = dayIndex * 2;
    const iconCode = forecast.daypart[0].iconCode[dpOffset] ?? forecast.daypart[0].iconCode[dpOffset + 1];
    const cond = forecast.daypart[0].wxPhraseLong[dpOffset] ?? forecast.daypart[0].wxPhraseLong[dpOffset + 1] ?? "";
    const high = forecast.calendarDayTemperatureMax[dayIndex];
    const low = forecast.calendarDayTemperatureMin[dayIndex];

    const entry = document.createElement("div");
    entry.className = "cityticker-entry";

    const city = document.createElement("div");
    city.className = "cityticker-entry-city";
    city.textContent = locationStr.split(",")[0];
    if (hasAlert && config.cityTicker?.severeAlertIcon) city.prepend(buildAlertBadge());

    const temps = document.createElement("div");
    temps.className = "cityticker-entry-forecast-temps";

    const hiTemp = document.createElement("span");
    hiTemp.className = "cityticker-entry-temp";
    hiTemp.textContent = high != null ? `${high}${units.endingTemp}` : "--";

    const loTemp = document.createElement("span");
    loTemp.className = "cityticker-entry-temp-low";
    loTemp.textContent = low != null ? `${low}${units.endingTemp}` : "--";

    temps.append(hiTemp, document.createTextNode(" / "), loTemp);

    const condEl = document.createElement("div");
    condEl.className = "cityticker-entry-condition";
    condEl.textContent = cond;

    const icon = document.createElement("img");
    icon.className = "cityticker-entry-icon";
    icon.src = weatherIconUrl(iconDir, resolveIcon(iconCode, "D"));

    entry.append(city, temps, condEl, icon);
    return entry;
}

function parseInlineLatex(text) {
    const escapeHtml = s => s
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;");

    const handlers = {
        textcolor: ([color, content]) => {
            const safe = /^[a-zA-Z0-9#%, .]+$/.test(color) ? color : "inherit";
            return `<span style="color:${safe}">${escapeHtml(content)}</span>`;
        },
        textbf:  ([content]) => `<span style="font-weight:bold">${escapeHtml(content)}</span>`,
        textit:  ([content]) => `<span style="font-style:italic">${escapeHtml(content)}</span>`,
        emph:    ([content]) => `<span style="font-style:italic">${escapeHtml(content)}</span>`,
        underline: ([content]) => `<span style="text-decoration:underline">${escapeHtml(content)}</span>`,
    };

    const pattern = /@([a-zA-Z]+)(\{[^}]*\})+/g;
    let result = "";
    let lastIndex = 0;

    for (const match of text.matchAll(pattern)) {
        result += escapeHtml(text.slice(lastIndex, match.index));
        const cmd = match[1];
        const args = [...match[0].matchAll(/\{([^}]*)\}/g)].map(m => m[1]);
        result += handlers[cmd]?.(args) ?? escapeHtml(match[0]);
        lastIndex = match.index + match[0].length;
    }
    result += escapeHtml(text.slice(lastIndex));
    return result;
}

function buildMessageNodes(msg) {
    let header = null;

    if (msg.headerIcon || msg.messageHeader) {
        header = document.createElement("div");
        header.className = "cityticker-header cityticker-header--message";

        if (msg.headerIcon) {
            const icon = document.createElement("img");
            icon.className = "cityticker-message-icon";
            icon.src = msg.headerIcon;
            header.appendChild(icon);
        }

        if (msg.messageHeader) {
            const label = document.createElement("span");
            label.textContent = msg.messageHeader;
            header.appendChild(label);
        }
    }

    const body = document.createElement("div");
    body.className = "cityticker-entries cityticker-entries--message";

    if (msg.bodyIcon) {
        const icon = document.createElement("img");
        icon.className = "cityticker-message-icon";
        icon.src = msg.bodyIcon;
        body.appendChild(icon);
    }

    const text = document.createElement("div");
    text.className = "cityticker-message-body";
    text.innerHTML = parseInlineLatex(msg.messageBody);
    body.appendChild(text);

    return { header, body };
}

async function fetchSectionData(categories) {
    const allLocations = [...new Set(
        Object.values(categories).flat()
    )];

    const [wxResults, alertResults] = await Promise.all([
        mapSettledLimit(allLocations, FETCH_CONCURRENCY, loc => requestWxData(loc, "ldl")),
        mapSettledLimit(allLocations, FETCH_CONCURRENCY, loc => requestAlertData(loc)),
    ]);

    const dataMap = {};
    const alertMap = {};
    allLocations.forEach((loc, i) => {
        const r = wxResults[i];
        dataMap[loc] = r.status === "fulfilled" ? r.value?.weather ?? null : null;
        alertMap[loc] = alertResults[i].status === "fulfilled" ? alertResults[i].value : null;
    });
    return { dataMap, alertMap };
}

async function mapSettledLimit(items, limit, worker) {
    const results = new Array(items.length);
    let nextIndex = 0;
    async function run() {
        while (nextIndex < items.length) {
            const index = nextIndex++;
            try {
                results[index] = { status: "fulfilled", value: await worker(items[index], index) };
            } catch (reason) {
                results[index] = { status: "rejected", reason };
            }
        }
    }
    const workers = Array.from({ length: Math.min(limit, items.length) }, run);
    await Promise.all(workers);
    return results;
}

function buildAlertIntroEntry(alertInfo) {
    const entry = document.createElement("div");
    entry.className = "cityticker-entry cityticker-entry--alert-intro";
    entry.style.color = alertInfo.color;
    entry.style.fontWeight = "600";
    const article = /^[aeiou]/i.test(alertInfo.event) ? "An" : "A";
    entry.textContent = `${article} ${alertInfo.event} is in effect for the following indicated locations.`;
    return entry;
}

function buildSections(categories, dataMap, alertMap, products) {
    const byProduct = {};

    for (const product of products) {
        byProduct[product] = [];
        for (const [category, locations] of Object.entries(categories)) {
            const entries = [];
            let sectionDayName = null;
            const sectionAlertGroups = new Map();

            for (const loc of locations) {
                const wx = dataMap[loc];
                if (!wx) continue;

                const alertData = alertMap[loc] ?? null;
                if (product === "currentConditions") {
                    const current = wx["v3-wx-observations-current"];
                    if (current) {
                        entries.push(buildCurrentEntry(loc, current, alertData));
                        if (alertData && config.cityTicker?.severeAlertIcon) {
                            const a = alertData.headline.alerts[0];
                            const key = `${a.eventDescription}|${a.severityCode}|${a.sourceColorName ?? ""}`;
                            if (!sectionAlertGroups.has(key)) {
                                sectionAlertGroups.set(key, {
                                    event: a.eventDescription,
                                    color: alertColorFor(alertData),
                                });
                            }
                        }
                    }
                } else if (product === "dayOne") {
                    const forecast = wx["v3-wx-forecast-daily-7day"] ?? wx["v3-wx-forecast-daily-3day"];
                    if (forecast?.calendarDayTemperatureMax?.[0] != null) {
                        sectionDayName ??= forecast.dayOfWeek?.[0]?.toUpperCase() ?? null;
                        entries.push(buildForecastEntry(loc, forecast, 0));
                    }
                } else if (product === "dayTwo") {
                    const forecast = wx["v3-wx-forecast-daily-7day"] ?? wx["v3-wx-forecast-daily-3day"];
                    if (forecast?.calendarDayTemperatureMax?.[1] != null) {
                        sectionDayName ??= forecast.dayOfWeek?.[1]?.toUpperCase() ?? null;
                        entries.push(buildForecastEntry(loc, forecast, 1));
                    }
                }
            }
            if (entries.length > 0) {
                const alertIntros = [...sectionAlertGroups.values()].map(buildAlertIntroEntry);
                byProduct[product].push({
                    category,
                    entries: [...alertIntros, ...entries],
                    dayName: sectionDayName,
                });
            }
        }
    }
    return byProduct;
}

function alertColorFor(alertData) {
    const a = alertData.headline.alerts[0];
    if (a.countryCode === "CA") {
        return { Red: "#e05537", Orange: "#dd7322", Yellow: "#c9b820" }[a.sourceColorName] ?? "#57aa57";
    }
    return { W: "#e04428", Y: "#dd7322", A: "#c9b820" }[a.severityCode] ?? "#57aa57";
}

function buildQueue(byProduct, products, messages, cycleCount) {
    const queue = [];

    for (const product of products) {
        for (const msg of messages) {
            if (msg.showBeforeProduct === product && (cycleCount - 1) % (msg.displayInterval || 1) === 0) {
                if (msg.messageHeader || msg.messageBody || msg.headerIcon || msg.bodyIcon) {
                    queue.push({ type: "message", msg });
                }
            }
        }

        for (const sec of (byProduct[product] ?? [])) {
            queue.push({ type: "entries", ...sec, headerText: sec.dayName ?? sec.category });
        }

        for (const msg of messages) {
            if (msg.showAfterProduct === product && (cycleCount - 1) % (msg.displayInterval || 1) === 0) {
                if (msg.messageHeader || msg.messageBody || msg.headerIcon || msg.bodyIcon) {
                    queue.push({ type: "message", msg });
                }
            }
        }
    }

    return queue;
}

// Every scroll below is a constant-speed linear move, so it is handed to the browser as a Web Animation.
// Those run on the compositor at the display's refresh rate even when the main thread is busy, which a
// requestAnimationFrame loop cannot promise on weak hardware.
class CityTickerScroller {
    #queueIndex = 0;
    #cycleCount = 1;
    #queue = [];

    #running = false;
    #runId = 0;
    #animations = [];
    #wake = null;

    constructor(viewport, track, byProduct, products, messages) {
        this.viewport = viewport;
        this.track = track;
        this.byProduct = byProduct;
        this.products = products;
        this.messages = messages;
    }

    #rebuildQueue() {
        this.#queue = buildQueue(this.byProduct, this.products, this.messages, this.#cycleCount);
        this.#queueIndex = 0;
    }

    #advance() {
        this.#queueIndex++;
        if (this.#queueIndex >= this.#queue.length) {
            this.#cycleCount++;
            this.#rebuildQueue();
        }
    }

    #slide(el, from, to) {
        const animation = el.animate(
            [{ transform: `translate3d(${from}px, 0, 0)` }, { transform: `translate3d(${to}px, 0, 0)` }],
            { duration: Math.abs(from - to) / SCROLL_SPEED_PX_PER_MS, easing: "linear", fill: "forwards" }
        );
        this.#animations.push(animation);
        return animation.finished;
    }

    #sleep(ms) {
        return new Promise(resolve => {
            const timer = setTimeout(resolve, ms);
            this.#wake = () => { clearTimeout(timer); resolve(); };
        });
    }

    #cancelAnimations() {
        for (const animation of this.#animations) animation.cancel();
        this.#animations = [];
    }

    async #play(seg) {
        this.#cancelAnimations();
        this.track.innerHTML = "";

        // Hidden (no width): every slide would take 0ms and the loop would spin, so idle until it shows.
        if (!this.viewport.offsetWidth) {
            await this.#sleep(1000);
            return;
        }

        let header = null;
        let entries;
        if (seg.type === "entries") {
            header = document.createElement("div");
            header.className = "cityticker-header";
            header.textContent = seg.headerText;

            entries = document.createElement("div");
            entries.className = "cityticker-entries";
            for (const node of seg.entries) entries.appendChild(node.cloneNode(true));

            this.track.append(header, entries);
        } else {
            const nodes = buildMessageNodes(seg.msg);
            header = nodes.header;
            entries = nodes.body;
            if (header) this.track.append(header);
            this.track.append(entries);
        }
        const headerWidth = header ? header.offsetWidth : 0;
        const entriesWidth = entries.scrollWidth;

        if (seg.type === "message" && !seg.msg.scroll) {
            await this.#sleep(STATIC_MSG_DURATION_MS);
            return;
        }

        entries.style.marginLeft = "0px";
        // The whole track slides in from the right edge, the entries then scroll past the header, and
        // finally the header leaves on its own.
        await this.#slide(this.track, this.viewport.offsetWidth, 0);
        await this.#slide(entries, 0, -(headerWidth + entriesWidth));
        await this.#slide(this.track, 0, -headerWidth);
    }

    async #run(runId) {
        while (this.#running && runId === this.#runId && this.#queue[this.#queueIndex]) {
            try {
                await this.#play(this.#queue[this.#queueIndex]);
            } catch (error) {
                if (error?.name === "AbortError") return;
                throw error;
            }
            if (runId !== this.#runId) return;
            this.#advance();
        }
    }

    start() {
        if (this.#running) return;
        this.#running = true;
        this.#rebuildQueue();
        this.#run(++this.#runId);
    }

    stop() {
        this.#running = false;
        this.#runId++;
        this.#cancelAnimations();
        this.#wake?.();
    }
}

async function buildCityTicker() {
    if (!config.cityTicker?.enabled) return;

    const { categories } = locationConfig.cityTickerLocations;
    const products = config.cityTicker.products ?? ["currentConditions"];
    const messages = config.cityTicker.intervalMessages ?? [];

    const viewport = document.querySelector(".cityticker-body");
    const track = viewport.querySelector(".cityticker-track");

    const { dataMap, alertMap } = await fetchSectionData(categories);
    const byProduct = buildSections(categories, dataMap, alertMap, products);

    const hasContent = products.some(p => (byProduct[p] ?? []).some(s => s.entries.length > 0));
    if (!hasContent) return;

    new CityTickerScroller(viewport, track, byProduct, products, messages).start();
}

document.addEventListener("DOMContentLoaded", () => {
    buildCityTicker();
});
