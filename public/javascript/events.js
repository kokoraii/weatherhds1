import { versionID } from "../config.js";

// The page's realtime channel to the server: one WebSocket at /events carrying every message type
// (format: internal/events/message.go). Modules handle their type with on(). A message that carries an
// id is a command and gets an ack: ok if the handler returns, its error if it throws.

const RETRY_MIN_MS = 1000;
const RETRY_MAX_MS = 15000;

/** This page's feed name, from ?feed= ("sd", "hd", ...), so commands can target one feed. */
export const feed = new URLSearchParams(window.location.search).get("feed") || "default";

const handlers = new Map();
const connectListeners = new Set();
const disconnectListeners = new Set();
const stateReporters = new Map();
let socket = null;
let retryMs = RETRY_MIN_MS;

/** Handles every message of one type. A handler may be async; throwing fails the command. */
export function on(type, handler) {
    if (handlers.has(type)) throw new Error(`events: ${type} already has a handler`);
    handlers.set(type, handler);
}

/** Runs on every (re)connection, and now if already connected. */
export function onConnect(listener) {
    connectListeners.add(listener);
    if (isConnected()) listener();
}

/** Runs whenever the connection drops. */
export function onDisconnect(listener) {
    disconnectListeners.add(listener);
}

export function isConnected() {
    return socket?.readyState === WebSocket.OPEN;
}

/** Sends a message to the server. False if not connected; nothing is queued. */
export function send(type, command, args) {
    if (!isConnected()) return false;
    socket.send(JSON.stringify({ type, command, args }));
    return true;
}

/**
 * Adds a named part of the page state the server is told about in hello and on reportState(), such as
 * a presentation's "running" or "paused".
 */
export function reportsState(name, read) {
    stateReporters.set(name, read);
}

/** Puts a line in the server's log, for things that go wrong after a command was acked. */
export function report(text) {
    send("health", "log", { text });
}

export function reportState() {
    send("health", "state", { presentations: currentState() });
}

function currentState() {
    return Object.fromEntries([...stateReporters].map(([name, read]) => [name, read()]));
}

function connect() {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${protocol}//${window.location.host}/events`);
    socket = ws;
    ws.addEventListener("open", () => {
        retryMs = RETRY_MIN_MS;
        send("health", "hello", { feed, version: versionID, presentations: currentState() });
        for (const listener of connectListeners) listener();
    });
    ws.addEventListener("message", event => {
        let message;
        try {
            message = JSON.parse(event.data);
        } catch (error) {
            console.warn("[events.js] Ignoring a malformed message:", error);
            return;
        }
        dispatch(message);
    });
    ws.addEventListener("close", () => {
        if (socket !== ws) return;
        socket = null;
        for (const listener of disconnectListeners) listener();
        setTimeout(connect, retryMs);
        retryMs = Math.min(retryMs * 2, RETRY_MAX_MS);
    });
}

async function dispatch(message) {
    const handler = handlers.get(message.type);
    try {
        if (!handler) throw new Error(`this page doesn't handle ${message.type}`);
        await handler(message);
        if (message.id) socket?.send(JSON.stringify({ type: "ack", id: message.id, ok: true }));
    } catch (error) {
        console.warn(`[events.js] ${message.type} ${message.command ?? ""} failed:`, error);
        if (message.id) socket?.send(JSON.stringify({ type: "ack", id: message.id, error: String(error?.message ?? error) }));
    }
}

/** Throws for a command a handler doesn't know, so the sender hears about it. */
export function unsupported(message) {
    throw new Error(`unsupported ${message.type} command ${message.command}`);
}

connect();
