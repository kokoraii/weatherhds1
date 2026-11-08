// Poppins has no tabular figures ("1" is half the width of "8", and font-variant-numeric can't change that),
// so a ticking time changes width every second and shifts everything beside it. This puts each digit in a
// fixed-width cell (.digit in base.css) so the text keeps one width as the digits change.

/** Text as nodes, each digit wrapped in its own cell. Append or replaceChildren with the result. */
export function steadyDigits(text) {
    return [...text].map(char => /\d/.test(char)
        ? Object.assign(document.createElement("span"), { className: "digit", textContent: char })
        : char);
}

/** Sets an element's text with steady digits, skipping the DOM work when the text hasn't changed. */
export function setSteadyText(el, text) {
    if (el.dataset.text === text) return;
    el.dataset.text = text;
    el.replaceChildren(...steadyDigits(text));
}
