import { versionID } from "../config.js";
import { setServerHealth } from "./data.js";
import { on, onDisconnect, unsupported } from "./events.js";

// The server's lifecycle as the page sees it, over the event channel. The page is healthy while the
// server's heartbeat keeps arriving; it falls back to its standby slides (data.js serverHealth) when the
// channel drops, the server says it's shutting down, or the heartbeat goes quiet.

const SILENCE_LIMIT_MS = 45_000; // three missed heartbeats
const log = (...args) => console.log(`[health.js] | ${new Date().toLocaleString()} |`, ...args);

let watchdog = null;
let firstBoot = null;
const others = new Map();

/** Handles a health command that belongs to another module: backgroundSync, holidaySync. */
export function onHealth(command, handler) {
    others.set(command, handler);
}

function heard() {
    setServerHealth(0);
    clearTimeout(watchdog);
    watchdog = setTimeout(() => {
        log("No heartbeat from the server; showing standby.");
        setServerHealth(1);
    }, SILENCE_LIMIT_MS);
}

function lost() {
    clearTimeout(watchdog);
    setServerHealth(1);
}

on("health", message => {
    switch (message.command) {
        case "startUp": {
            heard();
            const { boot, version } = message.args;
            firstBoot ??= boot;
            // Only after the server restarts: a page loaded with an edited config.js shouldn't reload forever
            // against a server still running the old one.
            if (boot !== firstBoot && version !== versionID) {
                log(`Server restarted with version ${version} (this page is ${versionID}); reloading.`);
                window.location.reload();
            }
            break;
        }
        case "heartbeat":
            heard();
            break;
        case "shutDown":
            log("Server shutting down:", message.args?.reason);
            lost();
            break;
        case "error":
            console.warn("[health.js] Server says:", message.error);
            break;
        default: {
            const handler = others.get(message.command);
            if (!handler) unsupported(message);
            return handler(message.args ?? {});
        }
    }
});

on("reload", message => {
    if (message.command !== "refresh") unsupported(message);
    log("Reload requested by the server.");
    // After this handler returns, so the ack goes out first.
    setTimeout(() => window.location.reload(), 100);
});

onDisconnect(lost);
