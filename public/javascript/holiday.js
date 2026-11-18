import { holidayMapping } from "../config.js";
import { onHealth } from "./health.js";

// Today's holiday, from holidayMapping, for the date the server sends (health holidaySync: as the page
// connects and at local midnight). It sets data-holiday and --holiday-color0/1 on <body> for styles to
// use; nothing uses them yet.
onHealth("holidaySync", ({ date }) => {
    const holiday = holidayMapping[date];
    const body = document.body;
    if (!holiday) {
        delete body.dataset.holiday;
        body.style.removeProperty("--holiday-color0");
        body.style.removeProperty("--holiday-color1");
        return;
    }
    body.dataset.holiday = holiday.name;
    body.style.setProperty("--holiday-color0", holiday.color0);
    body.style.setProperty("--holiday-color1", holiday.color1);
});
