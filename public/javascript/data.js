export let imageIndex;
export let locationsList;
export let units;
export let serverHealth = 0; // if set to one then that means the heartbeat failed and the client SHOULD fallback to client-side slides. keyword 'SHOULD' because im a horrible coder.

let onlineBg;
let bgUrl;
const logTheFrickinTime = `[data.js] | ${new Date().toLocaleString()} |`;
const REQUEST_CACHE_MS = 2 * 60 * 1000;
const requestCache = new Map();
const inFlightRequests = new Map();

import { runInitialLDL, requestBulletinCrawl, cancelBulletinCrawl } from "./ldl.js";
import { config, serverConfig, locationConfig } from "../config.js";

async function cachedJSONRequest(key, url, options = {}) {
  const now = Date.now();
  const cached = requestCache.get(key);
  if (cached && now - cached.time < REQUEST_CACHE_MS) {
    return cached.value;
  }
  if (inFlightRequests.has(key)) {
    return inFlightRequests.get(key);
  }

  const request = fetch(url, options)
    .then(async response => {
      if (response.status === 204) return null;
      if (!response.ok) throw new Error(`request failed (${response.status})`);
      return response.json();
    })
    .then(value => {
      requestCache.set(key, { time: Date.now(), value });
      return value;
    })
    .finally(() => {
      inFlightRequests.delete(key);
    });
  inFlightRequests.set(key, request);
  return request;
}

export async function requestWxData(location, locType) {
  let wxData = {
    metadata: null,
    weather: null
  };

  try {
    wxData = await cachedJSONRequest(
      `wx:${locType}:${location}`,
      `/data/${encodeURIComponent(location)}?locType=${locType}`
    );
  } catch (error) {
    console.error(logTheFrickinTime, "Error fetching weather data from backend:", error);

    try {
      const wxResponseLocData = await fetch(
        `https://api.weather.com/v3/location/search?query=${location}&locationType=city&language=en-CA&format=json&apiKey=${serverConfig.twcApiKey}`
      );

      const wxDataLoc = await wxResponseLocData.json();

      wxData = {
        metadata: {
          localeData: {
            localeName: wxDataLoc.location.city[0],
            adminDistrict: wxDataLoc.location.adminDistrict[0],
            country: wxDataLoc.location.country[0],
            countryCode: wxDataLoc.location.countryCode[0],
            lat: wxDataLoc.location.latitude[0],
            lon: wxDataLoc.location.longitude[0],
            postalKey: wxDataLoc.location.postalKey[0],
          },
          units: serverConfig.units,
          hdsLocType: "national",
        },
        weather: null,
      };
    } catch (fallbackError) {
      console.error(logTheFrickinTime, "Fallback weather fetch also failed:", fallbackError);
    }
  }

  return wxData;
}

export async function requestAlertData(location) {
  try {
    const rawAlertData = await cachedJSONRequest(
      `alert:${location}`,
      `/data/alerts/${encodeURIComponent(location)}`
    );

    if (rawAlertData === null || typeof rawAlertData === 'string') { // null = 204, no active alerts
      return null;
    }

    if (!rawAlertData.headline?.alerts || rawAlertData.headline.alerts.length === 0) {
      return null;
    }
    
    return rawAlertData;
    
  } catch (error) {
    console.error(logTheFrickinTime, "Error fetching alert data:", error);
    return null;
  }
}

export async function fetchOnlineBackground() {
  try {
    const response = await fetch('/bing-background');
    onlineBg = await response.json();

    bgUrl = `https://www.bing.com${onlineBg.images[0].url}`;

    return bgUrl;

  } catch (error) {
    console.error(logTheFrickinTime, "Error fetching online background:", error);
  }
}

export async function areWeDead() {
  try {
    const response = await fetch('/heartbeat');
    const data = await response.json();

    serverHealth = 0;
    return serverHealth;
  } catch (error) {
    console.error(logTheFrickinTime, 'Error fetching heartbeat:', error);
    serverHealth = 1;
    return serverHealth;
  }
}


areWeDead()
setInterval(areWeDead, 30000);

let alertRegistry = {};
let alertPollTimer = null;

function setupAlertPolling(hasAlert) {
  if (alertPollTimer) clearInterval(alertPollTimer);
  const interval = hasAlert
    ? serverConfig.alertPollIntervalSevere * 60000
    : serverConfig.alertPollIntervalNormal * 60000;
  alertPollTimer = setInterval(checkAlerts, interval);
}

async function checkAlerts() {
  const primaryLocation = locationConfig.localLocations.find(g => g.playlist === "primary")?.locations?.[0]?.name;
  const alertData = await requestAlertData(primaryLocation);

  if (alertData !== null) {
    const alert = alertData.headline.alerts[0];
    const alertText = alertData.detail?.alertDetail?.texts?.[0];

    if (alertRegistry[primaryLocation]?.identifier === alert.identifier) return;

    cancelBulletinCrawl();
    alertRegistry = {};
    alertRegistry[primaryLocation] = alert;

    const bulletinText = alertText
      ? (alertText.description + " " + (alertText.instruction || "")).trim()
      : alert.headlineText;

    requestBulletinCrawl(
      bulletinText,
      alert.severityCode,
      alert.eventDescription,
      alert.countryCode,
      alert.sourceColorName
    );
    setupAlertPolling(true);
  } else {
    if (alertRegistry[primaryLocation]) {
      cancelBulletinCrawl();
      delete alertRegistry[primaryLocation];
    }
    setupAlertPolling(false);
  }
}

checkAlerts();

if (config.presentationConfig.ldl) {
  runInitialLDL();
}

