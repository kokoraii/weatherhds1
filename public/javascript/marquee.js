// Native replacement for jquery.marquee. The scrolling is a CSS animation (css/marquee.css); this only
// measures the text and sets the timing variables, using the same maths the plugin used so existing
// speeds and durations carry over:
//   speed     px/s. Internally "time to cross one container width", so it stays constant whatever the text length.
//   duration  ms to cross one container width (ignored when speed is set).
//   gap       px between the two copies when duplicated (otherwise 0).
// Only right-to-left scrolling is supported.

const TRACK = "marquee-track";
const ITEM = "marquee-item";

export function startMarquee(el, { speed = 0, duration = 5000, gap = 0, duplicated = false, startVisible = false, delayBeforeStart = 0, pauseOnHover = false } = {}) {
    if (!el) return;
    stopMarquee(el);

    const item = document.createElement("div");
    item.className = ITEM;
    item.append(...el.childNodes);
    item.style.marginRight = `${duplicated ? gap : 0}px`;

    const track = document.createElement("div");
    track.className = TRACK;
    track.append(item);
    if (duplicated) track.append(item.cloneNode(true));
    el.append(track);

    const containerWidth = el.clientWidth;
    const itemWidth = item.offsetWidth + (duplicated ? gap : 0);
    if (!containerWidth || !itemWidth) {
        stopMarquee(el);
        return;
    }

    const crossMs = speed ? (containerWidth / speed) * 1000 : duration;
    // Enter from the right edge and leave past the left one.
    const fullPassMs = ((itemWidth + containerWidth) / containerWidth) * crossMs;

    let first = null;
    let loop;
    if (duplicated) {
        const loopMs = fullPassMs / 2;
        loop = { from: 0, ms: loopMs };
        // Entering from the right takes the extra distance at the same speed, then the two copies loop seamlessly.
        if (!startVisible) first = { from: containerWidth, ms: loopMs * (1 + containerWidth / itemWidth) };
    } else if (startVisible) {
        // Start with the text in place and scroll it out once, then come back in from the right forever.
        first = { from: 0, ms: (itemWidth / containerWidth) * crossMs };
        loop = { from: containerWidth, ms: fullPassMs };
    } else {
        loop = { from: containerWidth, ms: fullPassMs };
    }

    const set = (name, value) => track.style.setProperty(name, value);
    set("--mq-end", `${-itemWidth}px`);
    set("--mq-delay", `${delayBeforeStart}ms`);
    set("--mq-loop-from", `${loop.from}px`);
    set("--mq-loop-ms", `${loop.ms}ms`);
    if (first) {
        set("--mq-first-from", `${first.from}px`);
        set("--mq-first-ms", `${first.ms}ms`);
        track.classList.add(`${TRACK}--first`);
    }
    el.classList.toggle("marquee--pause-on-hover", pauseOnHover);
}

export function stopMarquee(el) {
    const track = el?.querySelector(`:scope > .${TRACK}`);
    if (!track) return;
    el.replaceChildren(...track.firstElementChild.childNodes);
    el.classList.remove("marquee--pause-on-hover");
}
