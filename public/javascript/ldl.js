import { requestWxData } from './data.js'
import { config, locationConfig, serverConfig, weatherIcons, displayUnits, brand } from "../config.js";
import { formatTime } from './weather.js';

const ldlPresentationSlides = {
    "0": { htmlID: "ldl-current",        durationMS: 15000, label: 'Now' },
    "1": { htmlID: "ldl-hourly",         durationMS: 16000, label: 'Next 6 hours' },
    "2": { htmlID: "ldl-period-summary", durationMS: 12000, label: 'Temp. Summary' },
    "3": { htmlID: "ldl-shortterm",      durationMS: 36000, label: 'Next 48 hours' },
    "4": { htmlID: "ldl-daily",          durationMS: 16000, label: 'Week Ahead' },
    "5": { htmlID: "ldl-riseset",        durationMS: 16000, label: '' },
}

let totalDuration = 0;
let totalDurationSec = 0;

for (let key in ldlPresentationSlides) {
    totalDuration += Number(ldlPresentationSlides[key].durationMS);
    totalDurationSec += Number(ldlPresentationSlides[key].durationMS) / 1000;
}

const logTheFrickinTime = () => `[ldl.js] | ${new Date().toLocaleString()} |`;

let ldlLocationIndex = 0;
let ldlSlideIndex = 0;
let iconDir = "animated"

const selectedDisplayUnits = displayUnits[serverConfig.units] || displayUnits['m'];
let endingTemp = selectedDisplayUnits.endingTemp, endingWind = selectedDisplayUnits.endingWind, endingDistance = selectedDisplayUnits.endingDistance, endingPressure = selectedDisplayUnits.endingPressure, endingCeiling = selectedDisplayUnits.endingCeiling;

const bulletinCrawlContainer = document.getElementsByClassName('ldl-bulletin-crawl')[0]

bulletinCrawlContainer.style.display = `none`

const ldlDomCache = Object.freeze({
    headlineBack: document.getElementById('ldl-bulletin-metadata-text'),
    bulletinText: document.getElementById('ldl-bulletin-text'),
    bulletinMetadataText: document.getElementById('ldl-bulletin-metadata-text'),
    locationLabel: document.getElementById('ldl-location-label'),
    progressBar: document.getElementById('ldl-location-progressbar'),
    currentTemp: document.getElementById('ldl-current-temp'),
    currentIcon: document.getElementById('ldl-current-icon'),
    currentCondition: document.getElementById('ldl-current-condition'),
    currentWindDirection: document.getElementById('ldl-current-wind-direction'),
    currentWindSpeed: document.getElementById('ldl-current-wind-speed'),
    currentWindGusts: document.getElementById('ldl-current-wind-gusts'),
    currentWindArrow: document.getElementById('ldl-current-wind-arrow'),
    currentHumidity: document.getElementById('ldl-current-humidity-value'),
    currentDewpoint: document.getElementById('ldl-current-dewpoint-value'),
    currentPressure: document.getElementById('ldl-current-pressure-value'),
    currentVisib: document.getElementById('ldl-current-visibility-value'),
    currentUv: document.getElementById('ldl-current-uv-value'),
    carouselCurrent: document.getElementById('ldl-carousel-current'),
    carouselTrack: document.getElementById('ldl-carousel-locations'),
    nowcastMessage: document.getElementById('ldl-nowcast-message2'),
    shorttermContainer: document.getElementById('ldl-shortterm-forecast-container'),
    dailyContainer: document.getElementById('ldl-daily-forecast-container'),
    ldlCurrent: document.getElementById('ldl-current'),
    ldlHourly: document.getElementById('ldl-hourly'),
    ldlPeriodSummary: document.getElementById('ldl-period-summary'),
    ldlShortterm: document.getElementById('ldl-shortterm'),
    ldlDaily: document.getElementById('ldl-daily'),
    ldlRiseset: document.getElementById('ldl-riseset'),
    hourlyGroup: {
        ldlHourlyTime0: document.getElementById('ldl-hourly-time0'),
        ldlHourlyIcon0: document.getElementById('ldl-hourly-icon0'),
        ldlHourlyTemp0: document.getElementById('ldl-hourly-temp0'),
        ldlHourlyCondition0: document.getElementById('ldl-hourly-condition0'),
        ldlHourlyTime1: document.getElementById('ldl-hourly-time1'),
        ldlHourlyIcon1: document.getElementById('ldl-hourly-icon1'),
        ldlHourlyTemp1: document.getElementById('ldl-hourly-temp1'),
        ldlHourlyCondition1: document.getElementById('ldl-hourly-condition1'),
        ldlHourlyTime2: document.getElementById('ldl-hourly-time2'),
        ldlHourlyIcon2: document.getElementById('ldl-hourly-icon2'),
        ldlHourlyTemp2: document.getElementById('ldl-hourly-temp2'),
        ldlHourlyCondition2: document.getElementById('ldl-hourly-condition2'),
        ldlHourlyTime3: document.getElementById('ldl-hourly-time3'),
        ldlHourlyIcon3: document.getElementById('ldl-hourly-icon3'),
        ldlHourlyTemp3: document.getElementById('ldl-hourly-temp3'),
        ldlHourlyCondition3: document.getElementById('ldl-hourly-condition3'),
        ldlHourlyTime4: document.getElementById('ldl-hourly-time4'),
        ldlHourlyIcon4: document.getElementById('ldl-hourly-icon4'),
        ldlHourlyTemp4: document.getElementById('ldl-hourly-temp4'),
        ldlHourlyCondition4: document.getElementById('ldl-hourly-condition4'),
        ldlHourlyTime5: document.getElementById('ldl-hourly-time5'),
        ldlHourlyIcon5: document.getElementById('ldl-hourly-icon5'),
        ldlHourlyTemp5: document.getElementById('ldl-hourly-temp5'),
        ldlHourlyCondition5: document.getElementById('ldl-hourly-condition5'),
        ldlHourlyTime6: document.getElementById('ldl-hourly-time6'),
        ldlHourlyIcon6: document.getElementById('ldl-hourly-icon6'),
        ldlHourlyTemp6: document.getElementById('ldl-hourly-temp6'),
        ldlHourlyCondition6: document.getElementById('ldl-hourly-condition6'),
    },
});

const activeProviders = Object.values(brand.providers ?? {}).filter(p => p.showOnLDLDur !== null);

const ldlProviderLogoEl = document.getElementById('ldl-provider-logo');
const ldlProviderHeadingEl = document.getElementById('ldl-provider-extratext-heading');
const ldlProviderTailingEl = document.getElementById('ldl-provider-extratext-tailing');

function applyProviderExtraText(extraText, fade) {
    const els = [ldlProviderHeadingEl, ldlProviderTailingEl];
    if (fade) {
        els.forEach(el => el?.classList.add('fading'));
        return;
    }
    if (!extraText) {
        if (ldlProviderHeadingEl) ldlProviderHeadingEl.textContent = '';
        if (ldlProviderTailingEl) ldlProviderTailingEl.textContent = '';
        return;
    }
    const applyStyle = el => {
        el.style.fontFamily = extraText.font ?? 'inherit';
        el.style.fontStyle = extraText.style ?? 'normal';
        el.style.fontWeight = extraText.weight ?? '400';
        el.style.fontSize = extraText.size ?? '0.8em';
        el.style.textShadow = extraText.shadow ?? 'none';
    };
    if (ldlProviderHeadingEl) {
        ldlProviderHeadingEl.textContent = extraText.heading ?? '';
        applyStyle(ldlProviderHeadingEl);
        ldlProviderHeadingEl.classList.remove('fading');
    }
    if (ldlProviderTailingEl) {
        ldlProviderTailingEl.textContent = extraText.tailing ?? '';
        applyStyle(ldlProviderTailingEl);
        ldlProviderTailingEl.classList.remove('fading');
    }
}

function initProviderLogoCycler() {
    if (!ldlProviderLogoEl || activeProviders.length === 0) return;

    const setProvider = (provider) => {
        ldlProviderLogoEl.src = provider.providerLogo;
        applyProviderExtraText(provider.extraText ?? null, false);
    };

    setProvider(activeProviders[0]);

    if (activeProviders.length === 1) return;

    let providerIndex = 0;

    const cycleToNext = () => {
        ldlProviderLogoEl.classList.add('fading');
        applyProviderExtraText(null, true);

        setTimeout(() => {
            providerIndex = (providerIndex + 1) % activeProviders.length;
            setProvider(activeProviders[providerIndex]);
            ldlProviderLogoEl.classList.remove('fading');

            setTimeout(cycleToNext, (activeProviders[providerIndex].showOnLDLDur ?? 120) * 1000);
        }, 450);
    };

    setTimeout(cycleToNext, (activeProviders[0].showOnLDLDur ?? 120) * 1000);
}

initProviderLogoCycler();

function initializeMarquee(retries = 3) {
  if (typeof $ === 'undefined' || typeof $.fn.marquee === 'undefined') {
    if (retries > 0) {
      setTimeout(() => initializeMarquee(retries - 1), 100);
    }
    return;
  }

  try {
    const $element = $('#ldl-bulletin-text');
    $element.marquee('destroy');
    $element.css('transform', 'none');
    
    setTimeout(() => {
      $element.marquee({
        speed: 180,
        gap: 100,
        direction: 'left',
        duplicated: false,
        pauseOnHover: false,
        startVisible: true,
        delayBeforeStart: 0
      });
    }, 100);
  } catch (error) {
    console.error('[initializeMarquee] Error:', error);
    if (retries > 0) {
      setTimeout(() => initializeMarquee(retries - 1), 200);
    }
  }
}

export function requestBulletinCrawl(text, alertCategory, headlineText, country, colorCode) {
  console.log('[requestBulletinCrawl] Called with:', { text, alertCategory, headlineText, country, colorCode });
  bulletinCrawlContainer.style.display = `flex`
  const beep = new Audio('../audio/beep.ogg');
  ldlDomCache.bulletinText.innerHTML = text;
  ldlDomCache.bulletinMetadataText.innerHTML = `
    <span class="bulletin-icon">⚠</span>
    <span class="bulletin-metadata-label">${headlineText || 'ACTIVE ALERT'}</span>
  `;

  initializeMarquee();

  beep.play();

  let bg;
  if (country === "CA") {
    bg = { Red: "rgba(189, 59, 29, 0.51)", Orange: "rgba(221, 115, 34, 0.51)", Yellow: "rgba(247, 231, 136, 0.51)" }[colorCode] ?? "rgba(87, 170, 87, 0.51)";
  } else {
    bg = { W: "rgba(188, 56, 33, 0.51)", Y: "rgba(221, 115, 34, 0.51)", A: "rgba(247, 231, 136, 0.51)" }[alertCategory] ?? "rgba(87, 170, 87, 0.51)";
  }
  ldlDomCache.headlineBack.style.background = bg;
}

export function cancelBulletinCrawl() {
  bulletinCrawlContainer.style.display = `none`
}

function staggerIn(els, step = 60, start = 0) {
    Array.from(els).forEach((el, i) => {
        el.style.transition = 'none'
        el.style.opacity = '0'
        el.style.transform = 'translateY(16px)'
        setTimeout(() => {
            el.style.transition = 'opacity 0.35s ease, transform 0.35s ease'
            el.style.opacity = '1'
            el.style.transform = 'translateY(0)'
        }, start + step * i)
    })
}

function staggerInX(els, step = 55) {
    Array.from(els).forEach((el, i) => {
        el.style.transition = 'none'
        el.style.opacity = '0'
        el.style.transform = 'translateX(-20px) scale(0.97)'
        setTimeout(() => {
            el.style.transition = 'opacity 0.28s ease, transform 0.28s ease'
            el.style.opacity = '1'
            el.style.transform = 'translateX(0) scale(1)'
        }, step * i)
    })
}

let shorttermPeriods = []
let shorttermPagerTimeout = null
let carouselCurrentX = 0
let carouselTailIndex = 0
let lastWindAngle = 0
let windJiggleTimeout = null
const ldlDailyCards = []
let ldlDailyCardsInitialized = false

let currentLDLData = null;

async function fetchLDLData(locationName) {
    try {
        if (config.verboseLogging === true) {
            console.log(`${logTheFrickinTime()} Fetching LDL data for: ${locationName}`)
        }
        
        const wxData = await requestWxData(locationName, "ldl");
        
        if (wxData && wxData.weather) {
            currentLDLData = {
                current: wxData.weather["v3-wx-observations-current"] ?? null,
                hourly: wxData.weather["v3-wx-forecast-hourly-2day"] ?? null,
                forecast: wxData.weather["v3-wx-forecast-daily-7day"] ?? wxData.weather["v3-wx-forecast-daily-3day"] ?? null,
                aqi: wxData.weather["v3-wx-globalAirQuality"] ?? null,
            };
            
            if (config.verboseLogging === true) {
                console.log(`${logTheFrickinTime()} Successfully fetched LDL data for ${locationName}`, currentLDLData)
            }
            
            return true;
        } else {
            console.warn(`${logTheFrickinTime()} No weather data returned for ${locationName}`);
            return false;
        }
    } catch (error) {
        console.error(`${logTheFrickinTime()} Error fetching LDL data for ${locationName}:`, error);
        return false;
    }
}

async function LDLData() {
    try {
        if (config.staticIcons === true) {
            iconDir = "static"
        } else {
            iconDir = "animated"
        }

        const ldlLocations = locationConfig.ldlLocations;
        
        if (!ldlLocations || ldlLocations.length === 0) {
            console.warn(`${logTheFrickinTime()} No LDL locations configured!`);
            return;
        }

        if (ldlLocationIndex >= ldlLocations.length) {
            ldlLocationIndex = 0;
        }

        const locationName = ldlLocations[ldlLocationIndex];
        
        if (ldlDomCache.locationLabel) {
            ldlDomCache.locationLabel.textContent = `Weather for ${locationName}`
        }

        const success = await fetchLDLData(locationName);
        
        if (!success || !currentLDLData || !currentLDLData.current) {
            console.warn(`${logTheFrickinTime()} No valid data for ${locationName}, skipping...`);
            return;
        }

        if (config.verboseLogging === true) {
            console.log(`${logTheFrickinTime()} Current LDL location: ${locationName}`)
        }

        appendLDLCurrent()
        appendLDLHourly()
        appendLDLPeriodSummary()
        appendLDLShortterm()
        appendLDLNowcast()
        appendLDLDaily()

    } catch (error) {
        console.error(`${logTheFrickinTime()} Error in LDLData:`, error);
    }
}

function appendLDLCurrent() {
    if (!currentLDLData?.current) return

    const current = currentLDLData.current
    const c = ldlDomCache

    if (c.currentTemp) {
        c.currentTemp.innerHTML = `${current.temperature}<span class="ldl-small-degrees">${endingTemp}</span>`
    }
    if (c.currentCondition) c.currentCondition.textContent = current.wxPhraseMedium ?? current.wxPhraseLong ?? 'N/A'

    const cardinalExpand = { N: 'North', E: 'East', S: 'South', W: 'West' }
    const windCardinal = (current.windDirectionCardinal ?? 'N/A').toString()
    const windCardinalDisplay = cardinalExpand[windCardinal] ?? windCardinal
    const windSpeed = Number(current.windSpeed ?? 0)
    const windGust = Number(current.windGust ?? 0)
    if (c.currentWindDirection) c.currentWindDirection.textContent = windCardinalDisplay
    if (c.currentWindSpeed) c.currentWindSpeed.textContent = `${windSpeed}${endingWind}`
    if (c.currentWindGusts) c.currentWindGusts.textContent = `${windGust}${endingWind} Gusts`

    const windDirectionAngle = Number.isFinite(Number(current.windDirection))
        ? Number(current.windDirection)
        : 0

    if (c.currentWindArrow) {
        lastWindAngle = windDirectionAngle
        c.currentWindArrow.style.transition = 'none'
        c.currentWindArrow.style.transform = `translate(-50%, -50%) rotate(${windDirectionAngle - 330}deg) translateY(var(--ldl-wind-arrow-radius))`
    }

    const windCompass = document.getElementById('ldl-current-wind-compass')
    const ticksOuter = windCompass?.querySelector('.ldl-current-wind-ticks')
    const ticksInner = windCompass?.querySelector('.ldl-current-wind-ticks-inner')
    if (ticksOuter) {
        ticksOuter.classList.remove('ldl-wind-idle-cw')
        ticksOuter.style.transition = 'none'
        ticksOuter.style.transform = 'rotate(-330deg)'
    }
    if (ticksInner) {
        ticksInner.classList.remove('ldl-wind-idle-ccw')
        ticksInner.style.transition = 'none'
        ticksInner.style.transform = 'rotate(330deg)'
    }

    if (c.currentHumidity) c.currentHumidity.textContent = `${current.relativeHumidity ?? 0}%`
    if (c.currentDewpoint) c.currentDewpoint.textContent = `${current.temperatureDewPoint ?? 0}${endingTemp}`
    if (c.currentPressure) c.currentPressure.textContent = `${current.pressureAltimeter ?? 0}${endingPressure}`
    if (c.currentVisib) c.currentVisib.textContent = `${Math.round(current.visibility ?? 0)}${endingDistance}`
    if (c.currentUv) {
        const uvi = current.uvIndex ?? '--'
        const uvd = current.uvDescription ? ` ${current.uvDescription}` : ''
        c.currentUv.textContent = `${uvi}${uvd}`
    }

    if (c.currentIcon) {
        const iconCode = current.iconCode
        const dayOrNight = current.dayOrNight
        const iconPath = weatherIcons[iconCode]?.[dayOrNight === 'D' ? 0 : 1] ?? 'not-available.svg'
        c.currentIcon.src = `/graphics/${iconDir}/${iconPath}`
    }
}

function appendLDLHourly() {
    if (!currentLDLData?.hourly) return

    const hourly = currentLDLData.hourly
    const c = ldlDomCache.hourlyGroup

    for (let i = 0; i < 7; i++) {
        if (c[`ldlHourlyTime${i}`]) c[`ldlHourlyTime${i}`].textContent = formatTime(hourly.validTimeLocal[i])
        if (c[`ldlHourlyTemp${i}`]) {
            c[`ldlHourlyTemp${i}`].innerHTML = `${hourly.temperature[i]}<span class="ldl-small-degrees">${endingTemp}</span>`
        }
        if (c[`ldlHourlyCondition${i}`]) c[`ldlHourlyCondition${i}`].textContent = hourly.wxPhraseShort[i] ?? 'N/A'
        if (c[`ldlHourlyIcon${i}`]) {
            const iconCode = hourly.iconCode[i]
            const dayOrNight = hourly.dayOrNight[i]
            const iconPath = weatherIcons[iconCode]?.[dayOrNight === 'D' ? 0 : 1] ?? 'not-available.svg'
            c[`ldlHourlyIcon${i}`].src = `/graphics/${iconDir}/${iconPath}`
        }
    }
}

function appendLDLPeriodSummary() {
    if (!currentLDLData?.current || !currentLDLData?.forecast) return

    const ps = ldlDomCache.ldlPeriodSummary
    if (!ps) return

    const current = currentLDLData.current
    const forecast = currentLDLData.forecast

    const nowTempEl = ps.querySelector('.ldl-now-temp')
    if (nowTempEl) {
        nowTempEl.innerHTML = `${current.temperature}<span class="ldl-small-degrees">${endingTemp}</span>`
    }

    const todayHiEl = ps.querySelector('.ldl-today-highest .ldl-period-highest-temp')
    if (todayHiEl) {
        const hi = forecast.calendarDayTemperatureMax?.[0] ?? '--'
        todayHiEl.innerHTML = `<span class="temp-point point-highest">🡅</span>${hi}<span class="ldl-small-degrees">${endingTemp}</span>`
    }

    const todayLoEl = ps.querySelector('.ldl-today-lowest .ldl-period-lowest-temp')
    if (todayLoEl) {
        const lo = forecast.calendarDayTemperatureMin?.[0] ?? '--'
        todayLoEl.innerHTML = `<span class="temp-point point-lowest">🡇</span>${lo}<span class="ldl-small-degrees">${endingTemp}</span>`
    }

    const nextDayName = forecast.dayOfWeek?.[1] ?? ''
    const nextDate = (() => {
        const d = new Date()
        d.setDate(d.getDate() + 1)
        return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
    })()

    const nextHiPeriodEl = ps.querySelector('.ldl-period-highest .ldl-period-highest-period')
    if (nextHiPeriodEl) nextHiPeriodEl.textContent = nextDayName

    const nextHiDateEl = ps.querySelector('.ldl-period-highest .ldl-period-highest-date')
    if (nextHiDateEl) nextHiDateEl.textContent = nextDate

    const nextHiTempEl = ps.querySelector('.ldl-period-highest .ldl-period-highest-temp')
    if (nextHiTempEl) {
        const hi = forecast.calendarDayTemperatureMax?.[1] ?? '--'
        nextHiTempEl.innerHTML = `<span class="temp-point point-highest">🡅</span>${hi}<span class="ldl-small-degrees">${endingTemp}</span>`
    }

    const nextLoNight = nextDayName ? `${nextDayName} Night` : 'Tomorrow Night'

    const nextLoPeriodEl = ps.querySelector('.ldl-period-lowest .ldl-period-lowest-period')
    if (nextLoPeriodEl) nextLoPeriodEl.textContent = nextLoNight

    const nextLoDateEl = ps.querySelector('.ldl-period-lowest .ldl-period-lowest-date')
    if (nextLoDateEl) nextLoDateEl.textContent = nextDate

    const nextLoTempEl = ps.querySelector('.ldl-period-lowest .ldl-period-lowest-temp')
    if (nextLoTempEl) {
        const lo = forecast.calendarDayTemperatureMin?.[1] ?? '--'
        nextLoTempEl.innerHTML = `<span class="temp-point point-lowest">🡇</span>${lo}<span class="ldl-small-degrees">${endingTemp}</span>`
    }
}

function appendLDLNowcast() {
    const el = ldlDomCache.nowcastMessage
    if (!el) return
    const narrative = currentLDLData?.forecast?.daypart?.[0]?.narrative?.[0]
        ?? currentLDLData?.forecast?.daypart?.[0]?.narrative?.[1]
        ?? ''
    el.textContent = narrative
}

function buildShorttermPeriods() {
    if (!currentLDLData?.forecast?.daypart?.[0]) return []
    const dp = currentLDLData.forecast.daypart[0]
    const periods = []
    for (let i = 0; i < Math.min(dp.daypartName?.length ?? 0, 6); i++) {
        if (!dp.daypartName[i]) continue
        periods.push({
            title: dp.daypartName[i],
            icon: weatherIcons[dp.iconCode[i]]?.[dp.dayOrNight[i] === 'D' ? 0 : 1] ?? 'not-available.svg',
            temp: dp.temperature[i],
            dayOrNight: dp.dayOrNight[i] ?? 'D',
            pop: dp.precipChance[i] ?? 0,
            narrative: dp.narrative[i] ?? '',
        })
    }
    return periods
}

function renderShorttermPeriod(period) {
    const container = ldlDomCache.shorttermContainer
    if (!container || !period) return
    const titleEl = container.querySelector('#ldl-shortterm-period-title0')
    const iconEl = container.querySelector('#ldl-shortterm-period-icon0')
    const tempEl = container.querySelector('#ldl-shortterm-period-temp0')
    const popEl = container.querySelector('#ldl-shortterm-period-pop0')
    const narrativeEl = container.querySelector('#ldl-shortterm-period-narrative0')
    if (titleEl) titleEl.textContent = period.title
    if (iconEl) iconEl.src = `/graphics/${iconDir}/${period.icon}`
    if (tempEl) {
        tempEl.classList.toggle('ldl-temp-high', period.dayOrNight === 'D')
        tempEl.classList.toggle('ldl-temp-low', period.dayOrNight !== 'D')
        tempEl.innerHTML = `${period.temp ?? '--'}<span class="ldl-small-degrees">${endingTemp}</span>`
    }
    if (popEl) {
        if (period.pop > 0) {
            popEl.textContent = `${period.pop}% Precip.`
            popEl.style.visibility = 'visible'
        } else {
            popEl.style.visibility = 'hidden'
        }
    }
    if (narrativeEl) narrativeEl.textContent = period.narrative
}

function appendLDLShortterm() {
    shorttermPeriods = buildShorttermPeriods()
    if (shorttermPeriods.length > 0) renderShorttermPeriod(shorttermPeriods[0])
}

function runShorttermPager() {
    if (shorttermPagerTimeout) {
        clearTimeout(shorttermPagerTimeout)
        shorttermPagerTimeout = null
    }
    if (shorttermPeriods.length <= 1) return

    const slideConfig = Object.values(ldlPresentationSlides).find(s => s.htmlID === 'ldl-shortterm')
    const durationMS = slideConfig?.durationMS ?? 24000
    const perPeriod = Math.floor(durationMS / shorttermPeriods.length)

    let index = 0

    const page = () => {
        index = (index + 1) % shorttermPeriods.length
        if (index === 0) return

        const container = ldlDomCache.shorttermContainer
        if (!container) return

        container.style.transition = 'opacity 0.2s ease, transform 0.2s ease'
        container.style.opacity = '0'
        container.style.transform = 'translateX(-24px)'

        setTimeout(() => {
            renderShorttermPeriod(shorttermPeriods[index])
            container.style.transform = 'translateX(24px)'
            container.style.transition = 'none'
            void container.offsetWidth
            container.style.transition = 'opacity 0.2s ease, transform 0.2s ease'
            container.style.opacity = '1'
            container.style.transform = 'translateX(0)'
        }, 200)

        if (index < shorttermPeriods.length - 1) {
            shorttermPagerTimeout = setTimeout(page, perPeriod)
        }
    }

    shorttermPagerTimeout = setTimeout(page, perPeriod)
}

function appendLDLDaily() {
    const container = ldlDomCache.dailyContainer
    if (!container || !currentLDLData?.forecast) return

    const days = buildDailyPeriods(currentLDLData.forecast)
    ensureLDLDailyCards(container, days.length)
    renderLDLDailyCards(days)
}

function buildDailyPeriods(forecast) {
    const dp = forecast?.daypart?.[0]
    const days = Math.min(forecast?.dayOfWeek?.length ?? 0, 7)
    const periods = []

    for (let i = 0; i < days; i++) {
        const dpDay = i * 2
        const dpNight = i * 2 + 1
        const dpIndex = dp?.daypartName?.[dpDay] != null ? dpDay : dpNight
        const condition = dp?.wxPhraseShort?.[dpIndex] ?? '--'
        const iconCode = dp?.iconCode?.[dpIndex]
        const dayOrNight = dp?.dayOrNight?.[dpIndex] ?? 'D'
        const iconPath = weatherIcons[iconCode]?.[dayOrNight === 'D' ? 0 : 1] ?? 'not-available.svg'

        periods.push({
            title: forecast.dayOfWeek[i]?.slice(0, 3).toUpperCase() ?? '---',
            condition,
            iconUrl: `/graphics/${iconDir}/${iconPath}`,
            high: `${forecast.calendarDayTemperatureMax?.[i] ?? '--'}°`,
            low: `${forecast.calendarDayTemperatureMin?.[i] ?? '--'}°`,
            pop: dp?.precipChance?.[dpIndex] != null ? `${dp.precipChance[dpIndex]}%` : '--',
        })
    }

    return periods
}

function createLDLDailyCard() {
    const card = document.createElement('div')
    card.className = 'ldl-daily-period-summary'

    const title = document.createElement('div')
    title.className = 'ldl-daily-period-title'

    const icon = document.createElement('img')
    icon.className = 'ldl-daily-icon'

    const condition = document.createElement('div')
    condition.className = 'ldl-daily-period-condition'

    const pop = document.createElement('div')
    pop.className = 'ldl-daily-period-pop'

    const high = document.createElement('div')
    high.className = 'ldl-daily-period-high'

    const low = document.createElement('div')
    low.className = 'ldl-daily-period-low'

    card.append(title, icon, condition, pop, high, low)
    card._weatherHDSDaily = { title, icon, condition, pop, high, low }
    return card
}

function ensureLDLDailyCards(container, count) {
    if (!ldlDailyCardsInitialized) {
        container.querySelectorAll('.ldl-daily-period-summary').forEach(card => card.remove())
        ldlDailyCards.length = 0
        ldlDailyCardsInitialized = true
    }

    while (ldlDailyCards.length < count) {
        const card = createLDLDailyCard()
        ldlDailyCards.push(card)
        container.appendChild(card)
    }

    for (let i = 0; i < ldlDailyCards.length; i++) {
        const card = ldlDailyCards[i]
        if (card.parentNode !== container) {
            container.appendChild(card)
        }
        card.style.display = i < count ? 'grid' : 'none'
    }
}

function renderLDLDailyCards(days) {
    for (let i = 0; i < ldlDailyCards.length; i++) {
        const card = ldlDailyCards[i]
        const day = days[i]
        if (!day) continue
        const parts = card._weatherHDSDaily
        parts.title.textContent = day.title
        parts.condition.textContent = day.condition
        parts.pop.textContent = day.pop
        parts.high.textContent = day.high
        parts.low.textContent = day.low
        if (parts.icon.src !== day.iconUrl) {
            parts.icon.src = day.iconUrl
        }
        parts.icon.alt = day.condition
    }
}

function appendLDLAlmanac() {
    const c = ldlDomCache;

    const now = new Date();
    const rise = new Date(currentLDLData.current.sunriseTimeLocal);
    const set = new Date(currentLDLData.current.sunsetTimeLocal);

    let riseTimeLocal;
    let setTimeLocal;
        
    const totalDaylight = set.getTime() - rise.getTime();
    const elapsed = now.getTime() - rise.getTime();
        
    let progress = elapsed / totalDaylight;
    let isNight = (now < rise || now > set);

    if (isNight) {
        if (c.riseTimeLabel) c.riseTimeLabel.textContent = "Moonrise";
        if (c.setTimeLabel) c.setTimeLabel.textContent = "Moonset";

        const moonRises = (currentLDLData.forecast.moonriseTimeLocal || []).map(t => new Date(t));
        const moonSets = (currentLDLData.forecast.moonsetTimeLocal || []).map(t => new Date(t));
        
        const events = [];
        moonRises.forEach(t => events.push({type: 'rise', time: t}));
        moonSets.forEach(t => events.push({type: 'set', time: t}));
        
        events.sort((a, b) => a.time - b.time);
        
        const lastEvent = events.filter(e => e.time <= now).pop();
        
        if (lastEvent && lastEvent.type === 'rise') {
            const currentMoonRise = lastEvent.time;
            const currentMoonSet = events.find(e => e.time > now && e.type === 'set');
            
            if (currentMoonSet) {
                const totalMoonTime = currentMoonSet.time.getTime() - currentMoonRise.getTime();
                const moonElapsed = now.getTime() - currentMoonRise.getTime();
                progress = moonElapsed / totalMoonTime;
                
                riseTimeLocal = formatTime(currentMoonRise.toISOString());
                setTimeLocal = formatTime(currentMoonSet.time.toISOString());
            } else {
                riseTimeLocal = formatTime(currentLDLData.forecast.moonriseTimeLocal[0]);
                setTimeLocal = formatTime(currentLDLData.forecast.moonsetTimeLocal[0]);
                progress = 0;
            }
        } else {
            riseTimeLocal = formatTime(currentLDLData.forecast.moonriseTimeLocal[0]);
            setTimeLocal = formatTime(currentLDLData.forecast.moonsetTimeLocal[0]);
            progress = 0;
        }
    } else {
        if (c.riseTimeLabel) c.riseTimeLabel.textContent = "Sunrise";
        if (c.setTimeLabel) c.setTimeLabel.textContent = "Sunset";
        
        riseTimeLocal = formatTime(currentLDLData.current.sunriseTimeLocal);
        setTimeLocal = formatTime(currentLDLData.current.sunsetTimeLocal);
    }
        
    if (progress < 0) progress = 0;
    if (progress > 1) progress = 1;

    if (currentLDLData && currentLDLData.current) {
        c.almanacSunrise.textContent = riseTimeLocal || "N/A";
        c.almanacSunset.textContent = setTimeLocal || "N/A";
    }

    const radiusX = 56;
    const radiusY = 46;
    const centerX = 60;
    const centerY = 50;
    
    const angleRad = Math.PI * (1 - progress);
    const sunX = centerX + radiusX * Math.cos(angleRad);
    const sunY = centerY - radiusY * Math.sin(angleRad);
    if (c.sunIndicator) {
        c.sunIndicator.style.left = `${sunX}px`;
        c.sunIndicator.style.top = `${sunY}px`;
    }

    const phaseDays = {
        "N": 0, "WXC": 3.7, "FQ": 7.4, "WXG": 11.1,
        "F": 14.8, "WNG": 18.5, "LQ": 22.1, "WNC": 25.8
    };

    const phaseDataCode = currentLDLData.forecast.moonPhaseCode
    const currentCode = phaseDataCode[0];

    const phaseToSVG = {
        "WNG": "moon-waning-gibbous.svg",
        "WXC": "moon-waxing-crescent.svg",
        "FQ": "moon-first-quarter.svg",
        "WNC": "moon-waning-crescent.svg",
        "LQ": "moon-last-quarter.svg",
        "F": "moon-full.svg",
        "WXG": "moon-waxing-gibbous.svg",
        "N": "moon-new.svg"
    }

    const majorPhases = [
        { code: "FQ", day: 7.4, name: "First Quarter", icon: "moon-first-quarter" },
        { code: "F", day: 14.8, name: "Full Moon", icon: "moon-full" },
        { code: "LQ", day: 22.1, name: "Last Quarter", icon: "moon-last-quarter" },
        { code: "N", day: 0, name: "New Moon", icon: "moon-new" }
    ];

    if (c.sunArchProgress) {
        const progressPercent = progress * 100;
        c.sunArchProgress.style.setProperty('--sun-progress', `${progressPercent}%`);
    }
    if (c.sunProgressArch) {
        if (isNight) {
            c.sunProgressArch.classList.add('night-mode');
            c.sunIndicator.style.background = `url('/graphics/${iconDir}/${phaseToSVG[currentCode]}')`;
            c.riseTimeLabel.textContent = "moonrise";
            c.setTimeLabel.textContent = "moonset";
        } else {
            c.sunProgressArch.classList.remove('night-mode');
            c.sunIndicator.style.background = `url('/graphics/${iconDir}/clear-day.svg')`;
            c.riseTimeLabel.textContent = "sunrise";
            c.setTimeLabel.textContent = "sunset";
        }
    }

    const moonPhaseNames = currentLDLData.forecast.moonPhase;
    const moonPhaseCodes = currentLDLData.forecast.moonPhaseCode;
    
    function getPhaseInfo(index, labelOverride) {
        if (!moonPhaseNames || !moonPhaseNames[index]) return null;

        const date = new Date();
        date.setDate(date.getDate() + index);
        const month = date.toLocaleString('default', { month: 'short' });
        const day = date.getDate();
        const label = labelOverride || `${month} ${day}`;
        
        const code = moonPhaseCodes[index];
        const iconName = phaseToSVG[code] || "moon-full.svg";

        return {
            name: moonPhaseNames[index],
            date: label,
            icon: `/graphics/animated/${iconName}`,
            code: code,
            dayIndex: index
        };
    }

    const apiPhases = [];

    const slot1 = getPhaseInfo(0, "Tonight");
    if (slot1) apiPhases.push(slot1);

    let slot2 = null;
    if (slot1) {
        for (let i = 1; i < moonPhaseCodes.length; i++) {
            if (moonPhaseCodes[i] !== slot1.code) {
                slot2 = getPhaseInfo(i);
                break;
            }
        }
        if (!slot2 && moonPhaseNames[1]) {
             slot2 = getPhaseInfo(1);
        }
    }
    
    if (slot2) apiPhases.push(slot2);

    const lastApiPhase = slot2 || slot1;
    let baseAge = 0;
    let baseDateOffset = 0;

    if (lastApiPhase) {
        baseAge = phaseDays[lastApiPhase.code] !== undefined ? phaseDays[lastApiPhase.code] : 0;
        baseDateOffset = lastApiPhase.dayIndex;
    }

    const nextPhases = [...majorPhases].map(p => {
        let diff = p.day - baseAge;
        if (diff <= 1.5) diff += 29.53;
        return { ...p, diff };
    }).sort((a, b) => a.diff - b.diff);

    const predictedPhases = [];
    for (let i = 0; i < 2; i++) {
        const p = nextPhases[i];
        const daysToAdd = baseDateOffset + p.diff;
        const date = new Date();
        date.setDate(date.getDate() + Math.round(daysToAdd));
        const month = date.toLocaleString('default', { month: 'short' });
        const day = date.getDate();
        
        predictedPhases.push({
            name: p.name,
            date: `${month} ${day}`,
            icon: `/graphics/animated/${p.icon}.svg`
        });
    }

    const finalPhases = [...apiPhases, ...predictedPhases];

    if (c.ldlMoonPhaseName1 && finalPhases[0]) c.ldlMoonPhaseName1.textContent = finalPhases[0].name;
    if (c.ldlMoonPhaseIllumination1 && finalPhases[0]) c.ldlMoonPhaseIllumination1.textContent = finalPhases[0].date;
    if (c.ldlMoonPhaseIcon1 && finalPhases[0]) c.ldlMoonPhaseIcon1.src = finalPhases[0].icon;

    if (c.ldlMoonPhaseName2 && finalPhases[1]) c.ldlMoonPhaseName2.textContent = finalPhases[1].name;
    if (c.ldlMoonPhaseIllumination2 && finalPhases[1]) c.ldlMoonPhaseIllumination2.textContent = finalPhases[1].date;
    if (c.ldlMoonPhaseIcon2 && finalPhases[1]) c.ldlMoonPhaseIcon2.src = finalPhases[1].icon;

    if (c.ldlMoonPhaseName3 && finalPhases[2]) c.ldlMoonPhaseName3.textContent = finalPhases[2].name;
    if (c.ldlMoonPhaseIllumination3 && finalPhases[2]) c.ldlMoonPhaseIllumination3.textContent = finalPhases[2].date;
    if (c.ldlMoonPhaseIcon3 && finalPhases[2]) c.ldlMoonPhaseIcon3.src = finalPhases[2].icon;

    if (c.ldlMoonPhaseName4 && finalPhases[3]) c.ldlMoonPhaseName4.textContent = finalPhases[3].name;
    if (c.ldlMoonPhaseIllumination4 && finalPhases[3]) c.ldlMoonPhaseIllumination4.textContent = finalPhases[3].date;
    if (c.ldlMoonPhaseIcon4 && finalPhases[3]) c.ldlMoonPhaseIcon4.src = finalPhases[3].icon;
}

function showLocationLabel() {
    const label = ldlDomCache.locationLabel;
    if (!label) return;
    label.style.display = 'block';
    label.style.animation = 'slideIn 1s ease-out';
}

function hideLocationLabel() {
    const label = ldlDomCache.locationLabel;
    if (!label) return;
    label.style.animation = 'slideOut 1s ease-out';

    setTimeout(() => {
        label.style.display = 'none';
    }, 300);
}

function triggerExitAnimation(slideID) {
    if (slideID === 'ldl-current') stopWindJiggle()
    const slideElement = document.getElementById(slideID);
    if (!slideElement) return;

    slideElement.style.animation = 'slideOut 1s ease-out';

    setTimeout(() => {
        slideElement.style.display = 'none';
    }, 200);
}

function stopWindJiggle() {
    if (windJiggleTimeout) {
        if (typeof windJiggleTimeout.cancel === 'function') windJiggleTimeout.cancel()
        else clearTimeout(windJiggleTimeout)
        windJiggleTimeout = null
    }
}

function startWindJiggle(arrow, ticksOuter, ticksInner, baseAngle) {
    stopWindJiggle()
    if (config.performance?.lightweightMotion !== false) {
        windJiggleTimeout = null
        return
    }

    const pickTarget = () => {
        const spread = Math.random() < 0.15 ? 5 + Math.random() * 7 : 1 + Math.random() * 3.5
        const dir = Math.random() < 0.5 ? 1 : -1
        return baseAngle + spread * dir
    }

    let targetAngle = pickTarget()
    let rafId = null
    let startTime = null
    let fromAngle = baseAngle
    let segDur = 1200 + Math.random() * 1400

    const EASE = (t) => {
        const c1 = 1.70158, c2 = c1 * 1.525
        return t < 0.5
            ? (Math.pow(2 * t, 2) * ((c2 + 1) * 2 * t - c2)) / 2
            : (Math.pow(2 * t - 2, 2) * ((c2 + 1) * (2 * t - 2) + c2) + 2) / 2
    }

    const tick = (now) => {
        if (!ldlDomCache.ldlCurrent || ldlDomCache.ldlCurrent.style.display === 'none') return

        if (!startTime) startTime = now
        const elapsed = now - startTime
        const t = Math.min(elapsed / segDur, 1)
        const eased = EASE(t)
        const angle = fromAngle + (targetAngle - fromAngle) * eased
        const tickOffset = angle - baseAngle

        arrow.style.transition = 'none'
        arrow.style.transform = `translate(-50%, -50%) rotate(${angle}deg) translateY(var(--ldl-wind-arrow-radius))`
        if (ticksOuter) { ticksOuter.style.transition = 'none'; ticksOuter.style.transform = `rotate(${tickOffset}deg)` }
        if (ticksInner) { ticksInner.style.transition = 'none'; ticksInner.style.transform = `rotate(${-tickOffset}deg)` }

        if (t < 1) {
            rafId = requestAnimationFrame(tick)
        } else {
            fromAngle = targetAngle
            targetAngle = pickTarget()
            segDur = 1000 + Math.random() * 1600
            startTime = null
            rafId = requestAnimationFrame(tick)
        }
    }

    rafId = requestAnimationFrame(tick)

    windJiggleTimeout = { cancel: () => { if (rafId) cancelAnimationFrame(rafId); rafId = null } }
}

function initCarousel() {
    const track = ldlDomCache.carouselTrack
    if (!track) return
    const locations = locationConfig.ldlLocations ?? []
    if (!locations.length) return

    track.innerHTML = ''
    track.style.transition = 'none'
    track.style.transform = 'translateX(0)'
    carouselCurrentX = 0

    fillCarouselTrack(track, locations)

    setCarouselActiveLocation(0)
}

function fillCarouselTrack(track, locations) {
    const container = track.parentElement
    const currentBadge = ldlDomCache.carouselCurrent
    const visibleWidth = Math.max(0, (container?.clientWidth ?? 0) - (currentBadge?.offsetWidth ?? 0))
    const targetWidth = Math.max(visibleWidth, window.innerWidth * 0.5) + 360
    let guard = 0

    while ((track.scrollWidth < targetWidth || track.children.length < 2) && guard < locations.length * 4) {
        const locationIndex = guard % locations.length
        appendCarouselPill(track, locations[locationIndex], locationIndex, false)
        guard++
    }

    carouselTailIndex = guard % locations.length
}

function appendCarouselPill(track, name, locationIndex, fadeIn) {
    const el = document.createElement('div')
    el.className = 'ldl-carousel-location-entry'
    el.dataset.locationIndex = locationIndex
    el.textContent = formatLDLCarouselLocationName(name)
    if (fadeIn) {
        el.style.opacity = '0'
        requestAnimationFrame(() => requestAnimationFrame(() => {
            el.style.transition = 'opacity 0.35s ease'
            el.style.opacity = '1'
        }))
    }
    track.appendChild(el)
    return el
}

function formatLDLCarouselLocationName(name) {
    return String(name ?? '').replace(/,\s*SK\b/g, '')
}

const CAROUSEL_CRAWL_SPEED = 200

function scrollCarouselToLocation(index) {
    const track = ldlDomCache.carouselTrack
    if (!track) return
    const locations = locationConfig.ldlLocations ?? []

    const pills = Array.from(track.querySelectorAll('.ldl-carousel-location-entry'))
    if (pills.length < 2) return

    const scrollStep = pills[1].offsetLeft - pills[0].offsetLeft
    const duration = (scrollStep / CAROUSEL_CRAWL_SPEED).toFixed(2)

    pills[0].style.transition = 'opacity 0.22s ease'
    pills[0].style.opacity = '0'

    track.style.transition = `transform ${duration}s linear`
    track.style.transform = `translateX(-${carouselCurrentX + scrollStep}px)`
    carouselCurrentX += scrollStep

    setCarouselActiveLocation(index)

    setTimeout(() => {
        pills[0].remove()

        const nextName = locations[carouselTailIndex]
        const nextIdx = carouselTailIndex
        carouselTailIndex = (carouselTailIndex + 1) % locations.length
        appendCarouselPill(track, nextName, nextIdx, true)

        carouselCurrentX -= scrollStep
        track.style.transition = 'none'
        track.style.transform = `translateX(-${carouselCurrentX}px)`
    }, parseFloat(duration) * 1000 + 60)
}

function setCarouselActiveLocation(index) {
    const track = ldlDomCache.carouselTrack
    if (!track) return
    track.querySelectorAll('.ldl-carousel-location-entry').forEach(el => {
        el.classList.toggle('ldl-location-carousel-current', Number(el.dataset.locationIndex) === index)
    })
}

function updateCarousel(slide) {
    const badge = ldlDomCache.carouselCurrent
    if (!badge) return
    badge.style.transition = 'opacity 0.18s ease'
    badge.style.opacity = '0'
    setTimeout(() => {
        badge.textContent = slide.label ?? ''
        badge.style.opacity = '1'
    }, 180)
}

const slideElementMap = {
    'ldl-current':        () => ldlDomCache.ldlCurrent,
    'ldl-hourly':         () => ldlDomCache.ldlHourly,
    'ldl-period-summary': () => ldlDomCache.ldlPeriodSummary,
    'ldl-shortterm':      () => ldlDomCache.ldlShortterm,
    'ldl-daily':          () => ldlDomCache.ldlDaily,
    'ldl-riseset':        () => ldlDomCache.ldlRiseset,
}

const slideAnimations = {
    'ldl-current': () => {
        if (ldlDomCache.ldlCurrent) {
            staggerIn(ldlDomCache.ldlCurrent.querySelectorAll('.ldl-current-detail'), 80, 200)
        }
        const compass = document.getElementById('ldl-current-wind-compass')
        const arrow = ldlDomCache.currentWindArrow
        const ticksOuter = compass?.querySelector('.ldl-current-wind-ticks')
        const ticksInner = compass?.querySelector('.ldl-current-wind-ticks-inner')
        if (!arrow) return
        void arrow.offsetWidth
        if (ticksOuter) void ticksOuter.offsetWidth
        if (ticksInner) void ticksInner.offsetWidth

        const WIND_DUR = '2.4s'
        const WIND_EASE = 'cubic-bezier(0.34, 1.20, 0.64, 1)'
        setTimeout(() => {
            arrow.style.transition = `transform ${WIND_DUR} ${WIND_EASE}`
            arrow.style.transform = `translate(-50%, -50%) rotate(${lastWindAngle}deg) translateY(var(--ldl-wind-arrow-radius))`
            if (ticksOuter) {
                ticksOuter.style.transition = `transform ${WIND_DUR} ${WIND_EASE}`
                ticksOuter.style.transform = 'rotate(0deg)'
            }
            if (ticksInner) {
                ticksInner.style.transition = `transform ${WIND_DUR} ${WIND_EASE}`
                ticksInner.style.transform = 'rotate(0deg)'
            }
        }, 16)

        setTimeout(() => {
            startWindJiggle(arrow, ticksOuter, ticksInner, lastWindAngle)
        }, 2500)
    },
    'ldl-hourly': () => {
        if (ldlDomCache.ldlHourly) {
            staggerInX(ldlDomCache.ldlHourly.querySelectorAll('.ldl-hourly-period'), 55)
        }
    },
    'ldl-period-summary': () => {
        if (ldlDomCache.ldlPeriodSummary) {
            staggerIn(ldlDomCache.ldlPeriodSummary.querySelectorAll('.ldl-period-now, .ldl-today-highest, .ldl-today-lowest, .ldl-period-highest, .ldl-period-lowest'), 80)
        }
    },
    'ldl-shortterm': () => {
        const el = ldlDomCache.shorttermContainer
        if (!el) return
        el.style.transition = 'none'
        el.style.opacity = '0'
        setTimeout(() => {
            el.style.transition = 'opacity 0.4s ease'
            el.style.opacity = '1'
        }, 150)
    },
    'ldl-daily': () => {
        if (ldlDomCache.dailyContainer) {
            staggerInX(ldlDomCache.dailyContainer.querySelectorAll('.ldl-daily-period-summary'), 60)
        }
    },
    'ldl-riseset': () => {
        const el = ldlDomCache.nowcastMessage
        if (!el) return
        el.style.transition = 'none'
        el.style.opacity = '0'
        el.style.transform = 'translateY(8px)'
        setTimeout(() => {
            el.style.transition = 'opacity 0.5s ease, transform 0.5s ease'
            el.style.opacity = '1'
            el.style.transform = 'translateY(0)'
        }, 300)
    },
}

function showLDLSlide() {
    const slide = ldlPresentationSlides[ldlSlideIndex]
    const duration = slide.durationMS

    if (config.verboseLogging === true) {
        console.log(`${logTheFrickinTime()} Showing LDL slide: ${slide.htmlID} for ${duration}ms`)
    }

    const slideElement = slideElementMap[slide.htmlID]?.() || document.getElementById(slide.htmlID)
    if (!slideElement) {
        console.warn(`${logTheFrickinTime()} LDL slide element not found: ${slide.htmlID}`)
        setTimeout(nextLDLSlide, 2000)
        return
    }

    slideElement.style.cssText = 'display:flex;animation:slideIn 0.6s ease-out'
    updateCarousel(slide)
    slideAnimations[slide.htmlID]?.()

    if (slide.htmlID === 'ldl-shortterm') {
        runShorttermPager()
    }

    if (ldlSlideIndex === Object.keys(ldlPresentationSlides).length - 1) {
        setTimeout(() => hideLocationLabel(), duration - 1000)
    }

    setTimeout(() => triggerExitAnimation(slide.htmlID), duration - 1000)

    setTimeout(() => {
        if (slide.htmlID === 'ldl-shortterm' && shorttermPagerTimeout) {
            clearTimeout(shorttermPagerTimeout)
            shorttermPagerTimeout = null
        }
        nextLDLSlide()
    }, duration)
}

function nextLDLSlide() {
    ldlSlideIndex = (ldlSlideIndex + 1) % Object.keys(ldlPresentationSlides).length;
    
    if (ldlSlideIndex === 0) {
        nextLDLLocation();
    } else {
        showLDLSlide();
    }
}

async function nextLDLLocation() {
    const ldlLocations = locationConfig.ldlLocations;
    
    if (!ldlLocations || ldlLocations.length === 0) {
        console.warn(`${logTheFrickinTime()} No LDL locations configured!`);
        return;
    }

    ldlLocationIndex = (ldlLocationIndex + 1) % ldlLocations.length;

    scrollCarouselToLocation(ldlLocationIndex)
    showLocationLabel();
    runProgressBar();

    ldlSlideIndex = 0;

    await LDLData();
    showLDLSlide();
}

let progressBarTimeout = null;

function runProgressBar() {
    const progressBar = ldlDomCache.progressBar;
    if (!progressBar) return;

    if (progressBarTimeout) {
        clearTimeout(progressBarTimeout);
        progressBarTimeout = null;
    }

    progressBar.style.animation = 'none';
    progressBar.style.transform = 'scaleX(0)';
    progressBar.style.display = 'block';
    requestAnimationFrame(() => {
        progressBar.style.animation = `ldlProgressBar ${totalDurationSec}s linear`;
    });

    progressBarTimeout = setTimeout(() => {
        progressBar.style.display = 'none';
        progressBar.style.animation = 'none';
        progressBarTimeout = null;
    }, totalDuration);
}

export async function runInitialLDL() {
    const ldlLocations = locationConfig.ldlLocations;
    
    if (!ldlLocations || ldlLocations.length === 0) {
        console.warn(`${logTheFrickinTime()} No LDL locations configured! LDL will not run.`);
        return;
    }

    if (config.verboseLogging === true) {
        console.log(`${logTheFrickinTime()} Starting LDL presentation`);
        console.log(`${logTheFrickinTime()} Total Duration (ms): ${totalDuration}`);
        console.log(`${logTheFrickinTime()} Total Duration (sec): ${totalDurationSec}`);
        console.log(`${logTheFrickinTime()} LDL Locations:`, ldlLocations);
    }

    ldlLocationIndex = 0;
    ldlSlideIndex = 0;

    initCarousel()
    showLocationLabel()
    runProgressBar()
    await LDLData()
    showLDLSlide()
}
