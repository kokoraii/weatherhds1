import { config } from "../config.js";

// Layout switches resolved once from config.js plus URL overrides (?sidebar=false, ?topbar=true), shared by
// every module that has to know whether the sidebar and top bar exist. Each also sets a class on <html>
// for the layout CSS in base.css.
const params = new URLSearchParams(window.location.search);
const root = document.documentElement;

function flag(name, fallback) {
    const value = params.get(name);
    return value === null ? fallback : value.toLowerCase() === "true";
}

export const sidebarEnabled = flag("sidebar", config.presentationConfig.sidebar !== false);
root.classList.toggle("sidebar-off", !sidebarEnabled);

// "auto" shows the top bar only when the sidebar is off; true always shows it, false never does.
const topbarSetting = String(params.get("topbar") ?? config.presentationConfig.topbar ?? "auto").toLowerCase();
const topbarShown = topbarSetting === "auto" ? !sidebarEnabled : topbarSetting === "true";
root.classList.toggle("topbar-on", topbarShown);

// The top bar only takes the network logo, clock and affiliate logo when the sidebar isn't showing them;
// next to the sidebar it stays blank. topbar.js moves the content and turns the brand rail into a radar.
export const topbarContent = topbarShown && !sidebarEnabled;
root.classList.toggle("topbar-filled", topbarContent);
