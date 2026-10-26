import { config, clock as clockConfig } from "../config.js";
import { topbarContent } from "./layout.js";

const ldlDateEl = document.getElementById("ldl-date");
const ldlTimeEl = document.getElementById("ldl-time");

// With the filled top bar, the LDL clock is hidden and topbar_status.js shows the time and date instead.
if (ldlDateEl && ldlTimeEl && !topbarContent) {
	const fallbackZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
	const systemZone = config.systemTimeZone || fallbackZone;
	const showSeconds = clockConfig?.showSeconds !== false;
	const use24Hour = clockConfig?.["24Hour"] === true;
	const rotateIntervalSec = Number(clockConfig?.rotateInterval ?? 0);
	const hideDuplicateZones = clockConfig?.hideDuplicateZones !== false;

	const configuredZones = Array.isArray(clockConfig?.zones) && clockConfig.zones.length > 0
		? clockConfig.zones
		: ["system"];

	const resolveZone = (zone) => zone === "system" ? systemZone : zone;

	const compareByClock = (date, timeZone) => new Intl.DateTimeFormat("en-CA", {
		timeZone,
		year: "numeric",
		month: "2-digit",
		day: "2-digit",
		hour: "2-digit",
		minute: "2-digit",
		hour12: false
	}).format(date);

	const getRenderableZones = () => {
		const now = new Date();
		const zones = configuredZones.map(resolveZone);

		if (!hideDuplicateZones) {
			return zones;
		}

		const filtered = [];
		let previousTime = "";

		for (const zone of zones) {
			let key = "";
			try {
				key = compareByClock(now, zone);
			} catch {
				key = "";
			}

			if (key !== previousTime || filtered.length === 0) {
				filtered.push(zone);
				previousTime = key;
			}
		}

		return filtered.length > 0 ? filtered : [systemZone];
	};

	let zones = getRenderableZones();
	let zoneIndex = 0;

	const formatDate = (date, timeZone) => new Intl.DateTimeFormat("en-US", {
		timeZone,
		weekday: "long",
		month: "long",
		day: "numeric",
		year: "numeric"
	}).format(date);

	const formatTime = (date, timeZone) => new Intl.DateTimeFormat("en-US", {
		timeZone,
		hour12: !use24Hour,
		hour: "numeric",
		minute: "2-digit",
		...(showSeconds ? { second: "2-digit" } : {}),
		timeZoneName: "short"
	}).format(date);

	const render = () => {
		if (zones.length === 0) {
			zones = [systemZone];
			zoneIndex = 0;
		}

		const now = new Date();
		const activeZone = zones[zoneIndex] || systemZone;

		ldlDateEl.textContent = formatDate(now, activeZone);
		ldlTimeEl.textContent = formatTime(now, activeZone);
	};

	render();
	setInterval(render, 1000);

	if (rotateIntervalSec > 0) {
		setInterval(() => {
			zones = getRenderableZones();
			zoneIndex = (zoneIndex + 1) % zones.length;
			render();
		}, rotateIntervalSec * 1000);
	}
}
