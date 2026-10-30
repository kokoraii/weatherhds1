import { brand, versionID, bumperBackgroundsRandom, stationIdBlurbs } from "../config.js";

// Station ID bumper. A random clip from bumperBackgroundsRandom.stationId plays full-bleed behind the UI,
// cued through video.js (loaded globally from index.html). The clip stays hidden until it actually starts
// playing, then fades in; VIDEO_LEAD_MS later the slide gets .station-id-slide--revealed and
// bumper-slides.css runs the bar -> logo -> blurb reveal. Every brand provider's logo sits under the network
// logo, and the time from the reveal until the slide leaves is split evenly between them.

const VIDEO_LEAD_MS = 1000;
const VIDEO_FADE_MS = 500; // matches the opacity transition on .station-id-video

const slideEl = document.querySelector('.station-id-slide');
const blurbEl = document.getElementById('station-id-blurb');
const networkLogoEl = document.getElementById('station-id-network-logo');

document.getElementById('station-id-hdsver').innerText = versionID;
document.getElementById('station-id-copyright').textContent = `This service uses WeatherHDS © ${new Date().getFullYear()} kokoraii`;
networkLogoEl.src = brand.networkLogo;
networkLogoEl.alt = brand.networkName;

const providerLogoEls = Object.entries(brand.providers).map(([name, provider]) => {
    const img = document.createElement('img');
    img.className = 'station-id-provider-logo';
    img.src = provider.providerLogo;
    img.alt = name;
    return img;
});
document.getElementById('station-id-providers').append(...providerLogoEls);

const player = videojs('station-id-video', {
    children: ['MediaLoader'], // just the <video>: no controls, poster, spinner or error overlay
    controls: false,
    muted: true,
    loop: true, // a few clips run shorter than the slide
    preload: 'auto',
});

let lastClip = null;
let lastOpener = null;
let revealTimer;
let providerTimers = [];
let leavesAt = 0; // performance.now() time the slide starts sliding out

// Random pick that never repeats the previous one back-to-back.
function pickFresh(list, previous) {
    const pool = list.length > 1 ? list.filter(item => item !== previous) : list;
    return pool[Math.floor(Math.random() * pool.length)];
}

function showProvider(index) {
    providerLogoEls.forEach((el, i) => el.classList.toggle('is-active', i === index));
}

function clearProviderTimers() {
    providerTimers.forEach(clearTimeout);
    providerTimers = [];
}

function reveal() {
    slideEl.classList.add('station-id-slide--revealed');

    // The first provider is already up; hand over to each next one at an even slice of the time left.
    clearProviderTimers();
    const sliceMs = (leavesAt - performance.now()) / providerLogoEls.length;
    for (let i = 1; i < providerLogoEls.length; i++) {
        providerTimers.push(setTimeout(() => showProvider(i), sliceMs * i));
    }
}

// Fires once the clip has loaded enough to actually play, so a slow load never fades in a blank frame.
function onClipPlaying() {
    player.addClass('is-playing');
    revealTimer = setTimeout(reveal, VIDEO_LEAD_MS);
}

// Called by runBumperSlide when the station ID comes on, with how long until it starts sliding out.
export function startStationId(onAirMs) {
    leavesAt = performance.now() + onAirMs;
    slideEl.classList.remove('station-id-slide--revealed');
    showProvider(0);

    lastOpener = pickFresh(stationIdBlurbs.openers, lastOpener);
    blurbEl.textContent = lastOpener;

    lastClip = pickFresh(bumperBackgroundsRandom.stationId, lastClip);
    player.src({ src: lastClip, type: 'video/webm' });
    player.one('playing', onClipPlaying);
    player.one('error', reveal); // a missing or undecodable clip still gets the logo on air
    player.play();
}

// Called by runBumperSlide as the station ID starts sliding out: fades the clip, then stops it once hidden.
export function endStationId() {
    clearTimeout(revealTimer);
    clearProviderTimers();
    player.off('playing', onClipPlaying);
    player.off('error', reveal);
    player.removeClass('is-playing');
    setTimeout(() => player.pause(), VIDEO_FADE_MS);
}
