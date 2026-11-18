import { on, unsupported } from "./events.js";
import { presentations } from "./presentation.js";

// What the page does when a change set goes live (changeSet provisioned): each presentation's presControl
// action, or a reload if none was set or any asked for one. rebuild re-reads config.js and fresh data
// without reloading; scripts and styles the page has already loaded only change on a reload.
on("changeSet", message => {
    if (message.command !== "provisioned") unsupported(message);
    const { files, ...actions } = message.args ?? {};
    const named = Object.entries(actions).filter(([name]) => name in presentations);
    if (named.length === 0 || named.some(([, action]) => action === "reload")) {
        console.log("[changeset.js] Change set live; reloading:", files);
        setTimeout(() => window.location.reload(), 100); // after the ack goes out
        return;
    }
    for (const [name, action] of named) {
        const presentation = presentations[name];
        const on = ["running", "paused", "ending"].includes(presentation.state);
        if (action === "rebuild" || (action === "rebuildAfter" && !on)) {
            presentation.rebuild();
        } else if (action === "rebuildAfter") {
            presentation.afterStop(() => presentation.rebuild());
            presentation.letEnd();
        }
    }
});
