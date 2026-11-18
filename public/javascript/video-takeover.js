import { on, report, unsupported } from "./events.js";
import { presentations } from "./presentation.js";

// Videos the server puts on screen (type video). playFile plays a file once and then hands back;
// startStream shows a live stream until stopStream. Two ways to show one (args.mode):
//   squeezeBack  (the default) the main slides shrink away and a frame at the video's shape grows in
//                their place, while the LDL and sidebar carry on around it
//   fullscreen   covers the whole picture
// audio says what the music does meanwhile: mute (the default) fades it out, duck lowers it, ignore leaves
// it playing as it is.
// presControl says what each presentation does meanwhile: pause, stop or continue. Whatever a video
// paused or stopped comes back when it ends; presentations it didn't touch are left alone.
//
// Commands are acked once the video is set up: starting can take longer than the server waits for an
// answer. A video that then fails to play is reported to the server log and taken down.

const DEFAULT_CONTROL = {
    fullscreen: { main: "pause", ldl: "pause", sidebar: "pause", cityticker: "pause" },
    squeezeBack: { main: "pause", ldl: "continue", sidebar: "continue", cityticker: "continue" },
};
const MUSIC_DUCK = { duck: {}, mute: { volume: 0 } }; // by audio; ignore leaves the music alone
const TRANSITION_MS = 600; // .video-takeover fades and grows over this long (base.css)
const START_TIMEOUT_MS = 20000;
const FALLBACK_ASPECT = 16 / 9;

const slidesBlock = document.querySelector(".slides-block");
let current = null; // { mode, frame, box, player, held }

on("video", async message => {
    const args = message.args ?? {};
    switch (message.command) {
        case "playFile":
            return show(message.target, args, false);
        case "startStream":
            return show(message.target, args, true);
        case "stopStream":
            if (!current) throw new Error("no video on screen");
            return finish(current);
        case "presControl":
            if (!current) throw new Error("no video on screen");
            return control(current, args);
        default:
            unsupported(message);
    }
});

async function show(url, args, live) {
    if (current) await finish(current); // one at a time
    const mode = args.mode ?? "squeezeBack";
    const takeover = { mode, audio: args.audio ?? "mute", held: new Map(), ...buildFrame(mode) };
    current = takeover;

    control(takeover, { ...DEFAULT_CONTROL[mode], ...args });
    const duck = MUSIC_DUCK[takeover.audio];
    if (duck) document.dispatchEvent(new CustomEvent("music-duck-start", { detail: duck }));

    takeover.player = videojs(takeover.video, {
        children: ["MediaLoader"], // just the picture: no controls, poster or spinner
        controls: false,
        preload: "auto",
    });
    takeover.player.src(type(url) ? { src: url, type: type(url) } : { src: url });
    takeover.player.on("loadedmetadata", () => fit(takeover));
    if (!live) takeover.player.one("ended", () => finish(takeover));

    started(takeover.player).then(
        () => {
            if (current !== takeover) return;
            requestAnimationFrame(() => {
                takeover.frame.classList.add("is-on");
                if (mode === "squeezeBack") slidesBlock.classList.add("video-squeezed");
            });
        },
        error => {
            report(`video ${url} failed: ${error.message}`);
            finish(takeover);
        },
    );
}

// The frame the video plays in: over the whole view, or over the main slides' area.
function buildFrame(mode) {
    const frame = document.createElement("div");
    frame.className = `video-takeover video-takeover-${mode}`;
    const box = document.createElement("div");
    box.className = "video-takeover-box";
    const video = document.createElement("video");
    video.playsInline = true;
    box.append(video);
    frame.append(box);

    if (mode === "squeezeBack") {
        // The slides' content box, in their positioned parent's layout coordinates (the view's scale
        // applies to both alike).
        const style = getComputedStyle(slidesBlock);
        const left = slidesBlock.offsetLeft + parseFloat(style.paddingLeft);
        const top = slidesBlock.offsetTop + parseFloat(style.paddingTop);
        frame.style.left = `${left}px`;
        frame.style.top = `${top}px`;
        frame.style.width = `${slidesBlock.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)}px`;
        frame.style.height = `${slidesBlock.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom)}px`;
        slidesBlock.offsetParent.append(frame);
    } else {
        document.querySelector(".view").append(frame);
    }
    const takeover = { frame, box, video };
    fit(takeover);
    return takeover;
}

// Sizes a squeezeBack box to the video's shape within the slides' area.
function fit(takeover) {
    if (takeover.frame.classList.contains("video-takeover-fullscreen")) return;
    const element = takeover.player?.el()?.querySelector("video");
    const aspect = element?.videoWidth ? element.videoWidth / element.videoHeight : FALLBACK_ASPECT;
    const { clientWidth: width, clientHeight: height } = takeover.frame;
    const boxWidth = Math.min(width, height * aspect);
    takeover.box.style.width = `${boxWidth}px`;
    takeover.box.style.height = `${boxWidth / aspect}px`;
}

function started(player) {
    return new Promise((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error(`didn't start within ${START_TIMEOUT_MS / 1000} s`)), START_TIMEOUT_MS);
        player.one("playing", () => {
            clearTimeout(timer);
            resolve();
        });
        player.one("error", () => {
            clearTimeout(timer);
            reject(new Error(player.error()?.message || "couldn't be loaded"));
        });
        player.play()?.catch(error => {
            clearTimeout(timer);
            reject(error);
        });
    });
}

// Applies what each named presentation should do now, giving back whatever this video held before.
function control(takeover, args) {
    for (const [name, presentation] of Object.entries(presentations)) {
        const action = args[name];
        if (!action) continue;
        release(takeover, name);
        if (action === "pause" && (presentation.state === "running" || presentation.state === "ending")) {
            presentation.pause();
            takeover.held.set(name, "pause");
        } else if (action === "stop" && presentation.state !== "stopped" && presentation.state !== "idle") {
            presentation.stop();
            takeover.held.set(name, "stop");
        }
    }
}

function release(takeover, name) {
    const presentation = presentations[name];
    const held = takeover.held.get(name);
    takeover.held.delete(name);
    if (held === "pause" && presentation.paused) presentation.resume();
    if (held === "stop" && presentation.state === "stopped") presentation.start();
}

// Takes a video down: the frame fades out and the slides come back, then what it held is given back.
async function finish(takeover) {
    if (takeover.finished) return;
    takeover.finished = true;
    if (current === takeover) current = null;
    takeover.frame.classList.remove("is-on");
    if (takeover.mode === "squeezeBack" && !current) slidesBlock.classList.remove("video-squeezed");
    await new Promise(resolve => setTimeout(resolve, TRANSITION_MS));
    takeover.player?.dispose();
    takeover.frame.remove();
    for (const name of [...takeover.held.keys()]) release(takeover, name);
    if (!current && MUSIC_DUCK[takeover.audio]) document.dispatchEvent(new CustomEvent("music-duck-end"));
}

function type(url) {
    const path = url.split("?")[0].toLowerCase();
    if (path.endsWith(".m3u8")) return "application/x-mpegURL";
    if (path.endsWith(".webm")) return "video/webm";
    if (path.endsWith(".mp4") || path.endsWith(".m4v")) return "video/mp4";
    if (path.endsWith(".mov")) return "video/quicktime";
    return null; // a live stream URL without an extension: video.js works it out
}
