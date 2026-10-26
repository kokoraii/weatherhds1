import { config, clock as clockConfig } from "../config.js";

// Clock and status line for the filled top bar (topbar.js starts both). The radar notch clock shows
// "11:39:52a CST"; the status line in the top bar cycles the date, the song playing and current conditions.

const timeZone = config.systemTimeZone || Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
const use24Hour = clockConfig?.["24Hour"] === true;
const showSeconds = clockConfig?.showSeconds !== false;

const HOLD_MS = 6000;
const FADE_MS = 500; // out, then in: a 1 second crossfade

const timeFormat = new Intl.DateTimeFormat("en-US", {
    timeZone,
    hourCycle: use24Hour ? "h23" : "h12",
    hour: "numeric",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "short",
});
const dateFormat = new Intl.DateTimeFormat("en-US", { timeZone, weekday: "short", month: "short", day: "numeric", year: "numeric" });
const monthFormat = new Intl.DateTimeFormat("en-US", { timeZone, month: "long" });

// A status line is a list of parts: plain text, { light } for light-weight text, { icon } for an image.
function render(el, parts) {
    el.replaceChildren(...parts.map(part => {
        if (typeof part === "string") return part;
        if (part.icon) return Object.assign(document.createElement("img"), { className: "topbar-status-icon", src: part.icon, alt: "" });
        return Object.assign(document.createElement("span"), { className: "light", textContent: part.light });
    }));
}

// Renders into el only when the parts changed, so the once-a-second tick doesn't rebuild an unchanged line.
function renderIfChanged(el, parts) {
    const signature = JSON.stringify(parts);
    if (el.dataset.signature === signature) return;
    el.dataset.signature = signature;
    render(el, parts);
}

// "11:39:52a CST": the last colon and seconds and the zone in light weight.
export function startNotchClock(el) {
    const tick = () => {
        const p = Object.fromEntries(timeFormat.formatToParts(new Date()).map(({ type, value }) => [type, value]));
        const hour = use24Hour ? p.hour.padStart(2, "0") : p.hour;
        renderIfChanged(el, [
            `${hour}:${p.minute}`,
            ...(showSeconds ? [{ light: `:${p.second}` }] : []),
            use24Hour ? "" : p.dayPeriod[0].toLowerCase(),
            { light: ` ${p.timeZoneName}` },
        ]);
    };
    tick();
    setInterval(tick, 1000);
}

// "Mon Oct. 26, 2026": abbreviated months get a period, ones already spelled out (May) don't.
function dateParts(now) {
    const p = Object.fromEntries(dateFormat.formatToParts(now).map(({ type, value }) => [type, value]));
    const month = p.month === monthFormat.format(now) ? p.month : `${p.month}.`;
    return [`${p.weekday} ${month} ${p.day}, ${p.year}`];
}

function formatDuration(seconds) {
    const s = Math.max(0, Math.floor(seconds));
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const ss = String(s % 60).padStart(2, "0");
    return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
}

// Cycles the date, the song playing (from music_toast.js's "music-state" events) and the current
// conditions (setConditions). Lines without data are skipped.
export class TopbarStatus {
    #el;
    #music = null;
    #conditions = null;
    #index = 0;

    constructor(el) {
        this.#el = el;
        document.addEventListener("music-state", event => { this.#music = event.detail; });
        this.#show();
        setInterval(() => this.#show(), 1000);
        setTimeout(() => this.#advance(), HOLD_MS);
    }

    // "Saskatoon [icon] 1° Sunny"
    setConditions(name, icon, temperature, phrase) {
        this.#conditions = [`${name} `, { icon }, ` ${temperature ?? "--"}° ${phrase ?? ""}`];
    }

    #lines() {
        return [() => dateParts(new Date()), () => this.#songParts(), () => this.#conditions];
    }

    // '"Title" by Artist 0:00/3:12', while a song is playing.
    #songParts() {
        const music = this.#music;
        if (!music?.playing || !music.title) return null;
        const elapsed = Math.min(music.duration, music.elapsed + (Date.now() - music.syncedAt) / 1000);
        return [
            `“${music.title}”${music.artist ? ` by ${music.artist}` : ""} `,
            { light: `${formatDuration(elapsed)}/${formatDuration(music.duration)}` },
        ];
    }

    #show() {
        const parts = this.#lines()[this.#index]();
        if (parts) renderIfChanged(this.#el, parts);
    }

    // Fades out, moves to the next line that has data, fades back in.
    #advance() {
        const lines = this.#lines();
        let next = this.#index;
        do next = (next + 1) % lines.length; while (!lines[next]());
        if (next === this.#index) {
            setTimeout(() => this.#advance(), HOLD_MS);
            return;
        }
        this.#el.classList.add("is-fading");
        setTimeout(() => {
            this.#index = next;
            this.#show();
            this.#el.classList.remove("is-fading");
            setTimeout(() => this.#advance(), HOLD_MS + FADE_MS);
        }, FADE_MS);
    }
}
