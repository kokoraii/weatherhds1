import { config } from "../config.js";

const musicConfig = config.music ?? {};
const duckingConfig = musicConfig.ducking ?? {};

const dom = {
    display:  document.getElementsByClassName("sidebar-music-toast")[0],
    art:      document.getElementById("sidebar-music-albumart"),
    title:    document.getElementById("sidebar-music-title"),
    artist:   document.getElementById("sidebar-music-artist"),
    album:    document.getElementById("sidebar-music-album"),
    elapsed:  document.getElementsByClassName("sidebar-music-current-time")[0],
    duration: document.getElementsByClassName("sidebar-music-duration")[0],
    progress: document.getElementById("sidebar-music-progress"),
};

const audio = new Audio();
audio.autoplay = true;
audio.playsInline = true;
audio.preload = "auto";
audio.volume = 1;
audio.style.position = "fixed";
audio.style.width = "1px";
audio.style.height = "1px";
audio.style.opacity = "0";
audio.style.pointerEvents = "none";
audio.style.left = "-9999px";
document.body?.appendChild(audio);

let socket = null;
let peer = null;
let reconnectTimer = null;
let progressTimer = null;
let volumeAnimation = null;
let serverElapsed = 0;
let lastSyncTime = null;
let trackDuration = 0;
let currentTrackId = "";
let targetVolume = 1;
let peerReconnectTimer = null;

if (!musicConfig.enabled) {
    hideToast();
} else {
    connect();
}

function hideToast() {
    if (dom.display) dom.display.style.opacity = "0";
}

function showToast() {
    if (dom.display) dom.display.style.opacity = "";
}

function formatTime(seconds) {
    if (!seconds || Number.isNaN(seconds) || seconds < 0) return "0:00";
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = Math.floor(seconds % 60);
    if (h > 0) return `${h}:${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
    return `${m}:${s.toString().padStart(2, "0")}`;
}

function updateProgressUI(current, total) {
    if (!dom.elapsed || !dom.progress) return;
    dom.elapsed.textContent = formatTime(current);
    const progress = total > 0 ? Math.min(1, Math.max(0, current / total)) : 0;
    dom.progress.style.transform = `scaleX(${progress})`;
}

function startProgress() {
    stopProgress();
    progressTimer = setInterval(() => {
        const current = serverElapsed + (Date.now() - lastSyncTime) / 1000;
        updateProgressUI(current, trackDuration);
    }, 200);
}

function stopProgress() {
    if (progressTimer) {
        clearInterval(progressTimer);
        progressTimer = null;
    }
}

const SCROLL_HOLD_START = 2000;
const SCROLL_HOLD_END = 4000;
const SCROLL_FADE_MS = 300;
const SCROLL_PX_PER_SEC = 30;

function easeInOut(t) {
    return t < 0.5 ? 2 * t * t : -1 + (4 - 2 * t) * t;
}

function resetScroll(el) {
    if (!el) return;
    if (el._scrollCancel) {
        el._scrollCancel();
        el._scrollCancel = null;
    }
    el.style.transform = "";
    el.style.display = "";
    el.style.width = "";
    el.style.maxWidth = "";
    el.style.overflow = "";
    el.style.textOverflow = "";
    el.style.willChange = "";
}

function applyScroll(el) {
    resetScroll(el);
    if (!el?.textContent?.trim()) return;

    const wrapper = el.parentElement;
    if (!wrapper) return;

    const wrapperWidth = wrapper.clientWidth;
    const contentWidth = el.scrollWidth;
    const overflow = contentWidth - wrapperWidth;
    if (wrapperWidth <= 0 || overflow <= 2) return;

    const distance = overflow + 18;
    const scrollMs = Math.max(1400, (distance / SCROLL_PX_PER_SEC) * 1000);
    const cycleMs = SCROLL_HOLD_START + scrollMs + SCROLL_HOLD_END + SCROLL_FADE_MS;
    let rafId = 0;
    let startTime = 0;

    el.style.display = "inline-block";
    el.style.width = "max-content";
    el.style.maxWidth = "none";
    el.style.overflow = "visible";
    el.style.textOverflow = "clip";
    el.style.willChange = "transform";

    const tick = (now) => {
        if (!startTime) startTime = now;
        const elapsed = (now - startTime) % cycleMs;

        if (elapsed < SCROLL_HOLD_START) {
            el.style.transform = "translateX(0)";
        } else if (elapsed < SCROLL_HOLD_START + scrollMs) {
            const t = (elapsed - SCROLL_HOLD_START) / scrollMs;
            el.style.transform = `translateX(${-distance * easeInOut(t)}px)`;
        } else {
            el.style.transform = `translateX(${-distance}px)`;
        }

        rafId = requestAnimationFrame(tick);
    };

    rafId = requestAnimationFrame(tick);
    el._scrollCancel = () => {
        if (rafId) cancelAnimationFrame(rafId);
        rafId = 0;
    };
}

function applyState(statePayload) {
    const state = statePayload?.state ?? "EMPTY";
    const track = statePayload?.track ?? {};
    const isPlaying = state === "PLAYING";

    if (!musicConfig.showWhenStopped && !isPlaying) {
        hideToast();
    } else {
        showToast();
    }

    serverElapsed = statePayload?.elapsed || 0;
    lastSyncTime = Date.now();
    trackDuration = track.duration || 0;
    currentTrackId = track.id || "";

    if (dom.title) dom.title.textContent = track.title || statePayload?.message || "";
    if (dom.artist) dom.artist.textContent = track.artist || "";
    if (dom.album) dom.album.textContent = track.album ? `${track.album}${track.year ? ` (${track.year})` : ""}` : "";
    if (dom.duration) dom.duration.textContent = formatTime(trackDuration);

    requestAnimationFrame(() => {
        applyScroll(dom.title);
        applyScroll(dom.artist);
        applyScroll(dom.album);
    });

    if (dom.art && track.has_art && currentTrackId) {
        dom.art.src = `${musicConfig.artPath || "/music/art"}/${encodeURIComponent(currentTrackId)}?t=${Date.now()}`;
        dom.art.style.display = "block";
        if (dom.art.parentElement) dom.art.parentElement.style.display = "";
    } else if (dom.art) {
        dom.art.removeAttribute("src");
        dom.art.style.display = "none";
        if (dom.art.parentElement) dom.art.parentElement.style.display = "none";
    }

    dom.display?.classList.toggle("music-playing", isPlaying);

    // For other now-playing displays (the top bar's status line); elapsed was measured at syncedAt.
    document.dispatchEvent(new CustomEvent("music-state", {
        detail: { playing: isPlaying, title: track.title || "", artist: track.artist || "", elapsed: serverElapsed, duration: trackDuration, syncedAt: lastSyncTime },
    }));

    stopProgress();
    if (isPlaying) {
        startProgress();
    } else {
        updateProgressUI(serverElapsed, trackDuration);
    }
}

function crossfade(statePayload) {
    dom.title?.classList.add("music-fade");
    dom.artist?.classList.add("music-fade");
    dom.album?.classList.add("music-fade");
    setTimeout(() => {
        applyState(statePayload);
        dom.title?.classList.remove("music-fade");
        dom.artist?.classList.remove("music-fade");
        dom.album?.classList.remove("music-fade");
    }, 250);
}

function websocketURL(path) {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    return `${protocol}//${window.location.host}${path || "/music/ws"}`;
}

function send(message) {
    if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify(message));
    }
}

function connect() {
    clearTimeout(reconnectTimer);
    socket = new WebSocket(websocketURL(musicConfig.websocketPath));
    socket.addEventListener("open", () => {
        send({ type: "hello" });
        createPeer().catch(error => {
            console.warn("[music_toast.js] WebRTC startup failed:", error);
        });
    });
    socket.addEventListener("message", event => {
        try {
            handleMessage(JSON.parse(event.data));
        } catch (error) {
            console.warn("[music_toast.js] Ignoring malformed music message:", error);
        }
    });
    socket.addEventListener("close", reconnect);
    socket.addEventListener("error", reconnect);
}

function reconnect() {
    closePeer();
    clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(connect, 3000);
}

async function createPeer() {
    closePeer();
    const pc = new RTCPeerConnection({ iceServers: [] });
    peer = pc;
    pc.addTransceiver("audio", { direction: "recvonly" });
    pc.ontrack = event => {
        audio.srcObject = event.streams[0];
        audio.play().catch(error => {
            console.warn("[music_toast.js] Music playback was blocked:", error);
        });
    };
    pc.onicecandidate = event => {
        if (event.candidate) {
            send({ type: "webrtc.ice", candidate: event.candidate.toJSON() });
        }
    };
    pc.onconnectionstatechange = () => {
        if (pc.connectionState === "connected") {
            clearTimeout(peerReconnectTimer);
            peerReconnectTimer = null;
            return;
        }
        if (["failed", "closed"].includes(pc.connectionState)) {
            if (peer === pc) {
                closePeer();
            }
            if (socket?.readyState === WebSocket.OPEN) {
                setTimeout(() => {
                    createPeer().catch(error => {
                        console.warn("[music_toast.js] WebRTC reconnect failed:", error);
                    });
                }, 1000);
            }
            return;
        }
        if (pc.connectionState === "disconnected" && !peerReconnectTimer) {
            peerReconnectTimer = setTimeout(() => {
                peerReconnectTimer = null;
                if (peer === pc && pc.connectionState === "disconnected") {
                    closePeer();
                    if (socket?.readyState === WebSocket.OPEN) {
                        createPeer().catch(error => {
                            console.warn("[music_toast.js] WebRTC reconnect failed:", error);
                        });
                    }
                }
            }, 5000);
        }
    };
    const offer = enableOpusStereo(await pc.createOffer());
    await pc.setLocalDescription(offer);
    send({ type: "webrtc.offer", sdp: pc.localDescription });
}

function enableOpusStereo(description) {
    if (!description?.sdp) return description;

    const lines = description.sdp.split(/\r?\n/);
    const opusPayloads = new Set();
    for (const line of lines) {
        const match = line.match(/^a=rtpmap:(\d+)\s+opus\/48000\/2$/i);
        if (match) opusPayloads.add(match[1]);
    }
    if (opusPayloads.size === 0) return description;

    const updatedPayloads = new Set();
    for (let index = 0; index < lines.length; index++) {
        const match = lines[index].match(/^a=fmtp:(\d+)\s*(.*)$/i);
        if (!match || !opusPayloads.has(match[1])) continue;
        lines[index] = `a=fmtp:${match[1]} ${forceOpusStereoParameters(match[2])}`;
        updatedPayloads.add(match[1]);
    }

    for (const payload of opusPayloads) {
        if (updatedPayloads.has(payload)) continue;
        const rtpmapIndex = lines.findIndex(line => new RegExp(`^a=rtpmap:${payload}\\s`, "i").test(line));
        if (rtpmapIndex >= 0) {
            lines.splice(rtpmapIndex + 1, 0, `a=fmtp:${payload} stereo=1;sprop-stereo=1`);
        }
    }

    return { type: description.type, sdp: lines.join("\r\n") };
}

function forceOpusStereoParameters(parameters) {
    const values = parameters
        .split(";")
        .map(parameter => parameter.trim())
        .filter(Boolean)
        .filter(parameter => {
            const key = parameter.split("=", 1)[0].trim().toLowerCase();
            return key !== "stereo" && key !== "sprop-stereo";
        });
    values.push("stereo=1", "sprop-stereo=1");
    return values.join(";");
}

async function handleMessage(message) {
    switch (message.type) {
        case "state":
            applyState(message.state);
            break;
        case "track_change":
            crossfade(message.state);
            break;
        case "webrtc.answer":
            if (peer && message.sdp) {
                await peer.setRemoteDescription(message.sdp);
            }
            break;
        case "webrtc.ice":
            if (peer && message.candidate) {
                await peer.addIceCandidate(message.candidate);
            }
            break;
        case "error":
            console.warn("[music_toast.js]", message.message);
            break;
    }
}

function closePeer() {
    clearTimeout(peerReconnectTimer);
    peerReconnectTimer = null;
    if (peer) {
        peer.close();
        peer = null;
    }
}

function fadeVolume(to, durationMs) {
    if (volumeAnimation) {
        cancelAnimationFrame(volumeAnimation);
        volumeAnimation = null;
    }
    const duration = Number.isFinite(durationMs) ? Math.max(0, durationMs) : 0;
    targetVolume = Number.isFinite(to) ? Math.min(1, Math.max(0, to)) : 1;
    const from = Number.isFinite(audio.volume) ? audio.volume : 1;
    const start = performance.now();
    function step(now) {
        const progress = duration <= 0 ? 1 : Math.min(1, Math.max(0, (now - start) / duration));
        audio.volume = Math.min(1, Math.max(0, from + (targetVolume - from) * progress));
        if (progress < 1) {
            volumeAnimation = requestAnimationFrame(step);
        } else {
            volumeAnimation = null;
        }
    }
    volumeAnimation = requestAnimationFrame(step);
}

document.addEventListener("music-duck-start", () => {
    if (duckingConfig.enabled === false) return;
    fadeVolume(Number(duckingConfig.volume ?? 0.25), Number(duckingConfig.fadeMs ?? 350));
});

document.addEventListener("music-duck-end", () => {
    if (duckingConfig.enabled === false) return;
    fadeVolume(1, Number(duckingConfig.releaseMs ?? 700));
});
