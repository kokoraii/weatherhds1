// Line-by-line scrolling for text that can outgrow its box (e.g. long forecast narratives on the LDL).

const MIN_STEP_MS = 900;
const MAX_STEP_MS = 2500;

/**
 * Fits el to the whole lines that can be seen inside `bounds` (at its natural size) and every clipping
 * ancestor, and, when its text runs longer, scrolls it up one line at a time. Steps are spread across
 * durationMs with a pause before the first and after the last, so every line is held on screen.
 * el must be a content-box block with `overflow: hidden`, laid out (not display:none); a little bottom
 * padding keeps the last visible line's descenders from being clipped. Returns a function that cancels
 * the remaining steps.
 */
export function scrollOverflowByLine(el, bounds, durationMs) {
    const style = getComputedStyle(el);
    const lineHeight = parseFloat(style.lineHeight);
    const padding = parseFloat(style.paddingBottom);

    // Collapse first: long text can stretch `bounds`, and scrollHeight still reports the full text.
    el.style.maxHeight = '0px';
    el.scrollTop = 0;
    const limit = visibleBottom(el, bounds);
    // Whole lines only: glyphs can overhang their line boxes by a pixel or two.
    const totalLines = Math.round((el.scrollHeight - padding) / lineHeight);

    let visibleLines = totalLines;
    for (; visibleLines > 1; visibleLines--) {
        el.style.maxHeight = `${visibleLines * lineHeight}px`;
        if (el.getBoundingClientRect().bottom <= limit + 0.5) break;
    }
    el.style.maxHeight = `${visibleLines * lineHeight}px`;

    const extraLines = totalLines - visibleLines;
    if (extraLines <= 0) return () => {};

    const stepMs = Math.min(MAX_STEP_MS, Math.max(MIN_STEP_MS, durationMs / (extraLines + 2)));
    const timers = [];
    for (let line = 1; line <= extraLines; line++) {
        timers.push(setTimeout(() => el.scrollTo({ top: line * lineHeight, behavior: 'smooth' }), stepMs * line));
    }
    return () => timers.forEach(clearTimeout);
}

// The lowest point (viewport px) where el can still be seen: the bottom of `bounds` or of the nearest
// clipping ancestors, whichever is highest.
function visibleBottom(el, bounds) {
    let bottom = bounds.getBoundingClientRect().bottom;
    for (let ancestor = el.parentElement; ancestor; ancestor = ancestor.parentElement) {
        if (getComputedStyle(ancestor).overflowY !== 'visible') {
            bottom = Math.min(bottom, ancestor.getBoundingClientRect().bottom);
        }
    }
    return bottom;
}
