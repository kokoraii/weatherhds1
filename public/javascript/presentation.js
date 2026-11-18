import * as liveConfig from "../config.js";
import { on, reportsState, reportState } from "./events.js";

// The page's presentations (main, ldl, sidebar, cityticker) and the commands the server can give them:
// start, stop, kill, rebuild, pause, resume, pauseResume and letEnd.
//
// Each presentation's module describes it with define() and runs its timing through its controller:
//   ctl.after(fn, ms)   a timeout that pauses with the presentation
//   ctl.every(fn, ms)   an interval, likewise
//   await ctl.wait(ms)  a pausable delay
//   await ctl.guard(p)  any other await (a fetch, say); also holds while paused
//   ctl.frame(fn)       requestAnimationFrame, held while paused
// All of these belong to the run they were made in: once the presentation stops or restarts, that run's
// timers never fire and its waits never resolve, so an old async chain can't carry on beside a new one.
// Modules call ctl.boundary() where their loop wraps; it returns false when the presentation was asked
// to end there (letEnd), and stops it.

const PAUSED_CLASS = "presentation-paused";
const STOPPED_CLASS = "presentation-stopped";
const EXIT_CLASS = "presentation-exiting";
const EXIT_MS = 500; // the .presentation-exiting fade in base.css

class Presentation {
    state = "idle"; // idle, running, paused, ending or stopped
    #run = null; // the current run; its timers are the live ones
    #ending = false;
    #roots = () => [];
    #start = null;
    #pauseHooks = [];
    #resumeHooks = [];
    #stopHooks = [];
    #afterStop = [];

    constructor(name) {
        this.name = name;
    }

    /**
     * Describes the presentation. roots: its top elements, which pause, fade and hide. start: begins its
     * loop from the top, and may be async.
     */
    define({ roots, start }) {
        this.#roots = roots;
        this.#start = start;
    }

    /** Media and other work that CSS can't pause: hooks run on pause, resume and stop. */
    onPause(hook) { this.#pauseHooks.push(hook); }
    onResume(hook) { this.#resumeHooks.push(hook); }
    onStop(hook) { this.#stopHooks.push(hook); }

    /** Runs once, the next time the presentation stops (after a letEnd, say). */
    afterStop(fn) { this.#afterStop.push(fn); }

    get paused() {
        return this.state === "paused";
    }

    // ---- timing ----

    after(fn, ms) {
        const run = this.#run;
        if (!run) return null;
        const timer = { fn, remaining: Math.max(0, ms), handle: null, started: 0 };
        run.timers.add(timer);
        if (!this.paused) this.#arm(run, timer);
        return timer;
    }

    every(fn, ms) {
        const tick = () => {
            fn();
            timer.next = this.after(tick, ms);
        };
        const timer = { next: this.after(tick, ms) };
        return timer;
    }

    cancel(timer) {
        if (!timer) return;
        if (timer.next !== undefined) {
            this.cancel(timer.next);
            timer.next = null;
            return;
        }
        clearTimeout(timer.handle);
        this.#run?.timers.delete(timer);
    }

    wait(ms) {
        return new Promise(resolve => this.after(resolve, ms));
    }

    async guard(promise) {
        const run = this.#run;
        const value = await promise;
        if (run !== this.#run) await new Promise(() => {}); // that run is over: never carry on
        if (this.paused) await new Promise(resolve => run.resumeWaiters.push(resolve));
        return value;
    }

    frame(fn) {
        const run = this.#run;
        if (!run) return;
        requestAnimationFrame(time => {
            if (run !== this.#run) return;
            if (this.paused) run.heldFrames.push(fn);
            else fn(time);
        });
    }

    #arm(run, timer) {
        timer.started = performance.now();
        timer.handle = setTimeout(() => {
            run.timers.delete(timer);
            if (run === this.#run) timer.fn();
        }, timer.remaining);
    }

    /**
     * Where the loop wraps. False means stop here: the presentation was asked to end (letEnd) and has
     * now stopped.
     */
    boundary() {
        if (!this.#ending) return true;
        this.stop();
        return false;
    }

    // ---- commands ----

    start() {
        if (this.state === "running" || this.state === "paused" || this.state === "ending") {
            throw new Error(`${this.name} is already running`);
        }
        for (const root of this.#roots()) root.classList.remove(STOPPED_CLASS, PAUSED_CLASS, EXIT_CLASS);
        this.#run = { timers: new Set(), heldFrames: [], resumeWaiters: [] };
        this.#ending = false;
        this.#setState("running");
        Promise.resolve(this.#start?.()).catch(error => console.error(`[presentation.js] ${this.name} failed:`, error));
    }

    /** Fades out, then stops. */
    stop() {
        if (!this.#isOn()) return;
        this.#end();
        for (const root of this.#roots()) root.classList.add(EXIT_CLASS);
        setTimeout(() => {
            if (this.#run === null) this.#hide(); // unless it was started again meanwhile
        }, EXIT_MS);
    }

    /** Stops at once, without an exit. */
    kill() {
        if (!this.#isOn()) return;
        this.#end();
        this.#hide();
    }

    /** Re-reads the config and fresh data, and starts over from the top. */
    async rebuild() {
        await refreshConfig();
        for (const hook of rebuildHooks) hook();
        if (this.#isOn()) {
            this.#end();
            this.#hide();
        }
        this.start();
    }

    pause() {
        if (this.state !== "running" && this.state !== "ending") throw new Error(`${this.name} isn't running`);
        const now = performance.now();
        for (const timer of this.#run.timers) {
            clearTimeout(timer.handle);
            timer.remaining = Math.max(0, timer.remaining - (now - timer.started));
        }
        for (const root of this.#roots()) root.classList.add(PAUSED_CLASS);
        for (const hook of this.#pauseHooks) hook();
        this.#setState("paused");
    }

    resume() {
        if (this.state !== "paused") throw new Error(`${this.name} isn't paused`);
        this.#setState(this.#ending ? "ending" : "running");
        for (const root of this.#roots()) root.classList.remove(PAUSED_CLASS);
        for (const timer of this.#run.timers) this.#arm(this.#run, timer);
        const held = this.#run.heldFrames.splice(0);
        for (const fn of held) requestAnimationFrame(fn);
        for (const resolve of this.#run.resumeWaiters.splice(0)) resolve();
        for (const hook of this.#resumeHooks) hook();
    }

    pauseResume() {
        if (this.paused) this.resume();
        else this.pause();
    }

    /** Finishes the current loop, then stops. */
    letEnd() {
        if (!this.#isOn()) throw new Error(`${this.name} isn't running`);
        this.#ending = true;
        if (this.state === "running") this.#setState("ending");
    }

    #isOn() {
        return this.#run !== null;
    }

    // Ends the current run: its timers die and its stop hooks run.
    #end() {
        const run = this.#run;
        for (const timer of run.timers) clearTimeout(timer.handle);
        run.timers.clear();
        this.#run = null;
        this.#ending = false;
        for (const root of this.#roots()) root.classList.remove(PAUSED_CLASS);
        for (const hook of this.#stopHooks) hook();
        this.#setState("stopped");
        for (const fn of this.#afterStop.splice(0)) fn();
    }

    #hide() {
        for (const root of this.#roots()) {
            root.classList.remove(EXIT_CLASS);
            root.classList.add(STOPPED_CLASS);
        }
    }

    #setState(state) {
        this.state = state;
        reportState();
    }
}

/** The page's presentations, by the name commands target. */
export const presentations = {
    main: new Presentation("main"),
    ldl: new Presentation("ldl"),
    sidebar: new Presentation("sidebar"),
    cityticker: new Presentation("cityticker"),
};

for (const [name, presentation] of Object.entries(presentations)) {
    reportsState(name, () => presentation.state);
}

const COMMANDS = new Set(["start", "stop", "kill", "rebuild", "pause", "resume", "pauseResume", "letEnd"]);

on("presentation", async message => {
    const presentation = presentations[message.target];
    if (!presentation) throw new Error(`no presentation called ${message.target}`);
    if (!COMMANDS.has(message.command)) throw new Error(`unsupported presentation command ${message.command}`);
    await presentation[message.command]();
});

const rebuildHooks = [];

/** Work every rebuild does first, such as dropping cached weather data. */
export function beforeRebuild(hook) {
    rebuildHooks.push(hook);
}

/**
 * Re-reads config.js and copies its values into the config objects every module imported, so a rebuild
 * sees edits without reloading the page. Values a module copied out at load keep their old value.
 */
export async function refreshConfig() {
    const fresh = await import(`/config.js?v=${Date.now()}`);
    for (const [name, value] of Object.entries(fresh)) {
        const live = liveConfig[name];
        if (live && typeof live === "object" && value && typeof value === "object") {
            for (const key of Object.keys(live)) {
                if (!(key in value)) delete live[key];
            }
            Object.assign(live, value);
        }
    }
}
