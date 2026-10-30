#!/usr/bin/env python3
"""Kiosk Chromium on the connected displays: the SD feed on the APU's HDMI output and, when
another display is connected, the HD feed on it.

Both feeds run in ONE Xorg server (one VT); two servers on separate VTs would freeze whichever
isn't on the active VT. The HD display is picked at startup, preferring the SD display's own
card (the APU renders both, side by side in one X screen), else a display on a second GPU
(its own X screen, :0.1). With no HD display found, only SD starts.

Audio: each feed plays to its GPU's HDMI audio sink. Two feeds on one card share its audio
device, so the HD feed is muted there unless --hd-sink names a sink for it (see --hd-sink).
"""

from __future__ import annotations

import argparse
from dataclasses import dataclass
import os
from pathlib import Path
import pwd
import re
import shutil
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request

DEFAULT_SD_URL = "http://localhost:3000?videoType=vga&sidebar=false"
DEFAULT_HD_URL = "http://localhost:3000?videoType=hdtv"
DEFAULT_SD_MODE = "800x600"
DEFAULT_HD_MODE = "1920x1080"
DEFAULT_DISPLAY = ":0"
# Modes the capture-card setup wants that monitors don't advertise: xrandr --newmode arguments after the name
# (pixel clock in MHz, then the horizontal and vertical timings and flags). 1080i59.94 is the CEA-861 timing.
CUSTOM_MODES = {
    "1920x1080i": ("74.176", "1920", "2008", "2052", "2200", "1080", "1084", "1094", "1125", "interlace", "+hsync", "+vsync"),
}
DRM_ROOT = Path("/sys/class/drm")
PCI_ADDRESS = re.compile(r"^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]$")
WM_CLASS_PREFIX = "wx-"  # windows are matched by class (wx-sd, wx-hd) in the window manager config

CHROMIUM_FLAGS = (
    "--kiosk",
    "--ozone-platform=x11",
    "--ignore-gpu-blocklist",
    "--enable-gpu-rasterization",
    "--enable-zero-copy",
    "--force-device-scale-factor=1",
    "--autoplay-policy=no-user-gesture-required",
    "--no-first-run",
    "--no-default-browser-check",
    "--noerrdialogs",
    "--disable-infobars",
    "--disable-session-crashed-bubble",
    "--hide-crash-restore-bubble",
    "--disable-breakpad",
    "--disable-features=Translate,MediaRouter",
    "--password-store=basic",
    "--overscroll-history-navigation=0",
    "--disable-pinch",
)
# Fedora's chromium.conf only enables VAAPI video decode when GRAPHIC_DRIVER=amd, and it ships as "default".
# Chromium takes the last --enable-features, so this repeats the distro's list with the AMD additions.
AMD_VIDEO_DECODE_FLAGS = (
    "--enable-accelerated-video-decode",
    "--enable-features=AllowQt,AcceleratedVideoDecodeLinuxGL,AcceleratedVideoDecodeLinuxZeroCopyGL,"
    "VaapiIgnoreDriverChecks,UseMultiPlaneFormatForHardwareVideo",
)


@dataclass(frozen=True)
class DisplayTarget:
    card: str
    connector: str
    gpu_bdf: str
    modes: frozenset[str]

    @property
    def xorg_bus_id(self) -> str:
        return xorg_bus_id(self.gpu_bdf)


@dataclass(frozen=True)
class Feed:
    """One browser window, full screen on one output."""

    name: str  # "sd" / "hd"
    screen: int  # X screen number: 0 = SD GPU, 1 = a second GPU
    output: str
    mode: str
    url: str
    gpu_bdf: str
    x: int = 0  # left edge of the output within its X screen
    sink: str | None = None  # PipeWire sink to play to; None picks the GPU's HDMI sink

    @property
    def size(self) -> tuple[int, int]:
        return parse_mode(self.mode)

    @property
    def wm_class(self) -> str:
        return f"{WM_CLASS_PREFIX}{self.name}"


@dataclass(frozen=True)
class BrowserUser:
    """Who the browsers run as. Xorg needs root; the browsers don't, so they drop to the desktop user
    (whose PipeWire they play through) and keep Chromium's sandbox."""

    uid: int
    gid: int
    home: Path
    groups: list[int]
    switch: bool  # started as root on behalf of a different user

    @property
    def runtime_dir(self) -> Path:
        return Path(f"/run/user/{self.uid}")

    @property
    def spawn_args(self) -> dict:
        """subprocess keyword arguments that run a child as this user."""
        return {"user": self.uid, "group": self.gid, "extra_groups": self.groups} if self.switch else {}


def xorg_bus_id(gpu_bdf: str) -> str:
    domain, bus, device_function = gpu_bdf.split(":")
    device, function = device_function.split(".")
    return f"PCI:{int(bus, 16)}@{int(domain, 16)}:{int(device, 16)}:{int(function, 16)}"


def runtime_uid() -> int:
    """The real desktop user's uid, even when launched through sudo or the service's SUDO_UID."""
    sudo_uid = os.environ.get("SUDO_UID")
    if os.geteuid() == 0 and sudo_uid and sudo_uid.isdigit():
        return int(sudo_uid)
    return os.geteuid()


def browser_user() -> BrowserUser:
    uid = runtime_uid()
    entry = pwd.getpwuid(uid)
    return BrowserUser(
        uid=uid,
        gid=entry.pw_gid,
        home=Path(entry.pw_dir),
        groups=os.getgrouplist(entry.pw_name, entry.pw_gid),
        switch=os.geteuid() == 0 and uid != 0,
    )


# Grayscale antialiasing + slight hinting: the safe choice when the signal may
# go through an HDMI->VGA adapter and the subpixel order is unknown.
FONTCONFIG_OVERRIDES = """<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <include ignore_missing="yes">/etc/fonts/fonts.conf</include>
  <match target="font">
    <edit name="antialias" mode="assign"><bool>true</bool></edit>
    <edit name="hinting" mode="assign"><bool>true</bool></edit>
    <edit name="hintstyle" mode="assign"><const>hint@@HINTING@@</const></edit>
    <edit name="rgba" mode="assign"><const>@@SUBPIXEL@@</const></edit>
  </match>
</fontconfig>
"""


def openbox_config(feeds: list[Feed]) -> str:
    """Pins each kiosk window on one X screen to its output, undecorated and full screen there.
    A bare X session would otherwise place the windows wherever the pointer is."""
    applications = "\n".join(
        f"""    <application class="{feed.wm_class}">
      <decor>no</decor>
      <position force="yes"><x>{feed.x}</x><y>0</y></position>
      <fullscreen>yes</fullscreen>
    </application>"""
        for feed in feeds
    )
    return (
        '<?xml version="1.0"?>\n'
        '<openbox_config xmlns="http://openbox.org/3.4/rc">\n'
        f"  <applications>\n{applications}\n  </applications>\n"
        "</openbox_config>\n"
    )


def pactl(env: dict[str, str], user: BrowserUser, *args: str) -> str:
    result = subprocess.run(
        ["pactl", *args], env=env, capture_output=True, text=True, check=False, **user.spawn_args
    )
    return result.stdout.strip() or result.stderr.strip()


def hdmi_sink(env: dict[str, str], user: BrowserUser, gpu_bdf: str) -> str | None:
    """The PipeWire sink of the GPU's own HDMI audio function (same PCI slot), e.g.
    alsa_output.pci-0000_0a_00.1.hdmi-stereo-extra1 for the GPU at 0000:0a:00.0."""
    if shutil.which("pactl") is None:
        return None
    slot = gpu_bdf.rsplit(".", maxsplit=1)[0].replace(":", "_")
    for line in pactl(env, user, "list", "short", "sinks").splitlines():
        fields = line.split("\t")
        if len(fields) > 1 and f"pci-{slot}." in fields[1] and "hdmi" in fields[1]:
            return fields[1]
    return None


def report_audio_state(env: dict[str, str], user: BrowserUser) -> None:
    """Print what the audio server sees ~15s after the browsers start, so a silent
    kiosk can be diagnosed: is there a stream, and which sink is it on?"""
    if shutil.which("pactl") is None:
        return
    time.sleep(15)
    queries = (
        ("default sink", ["get-default-sink"]),
        ("sinks (id name driver format state)", ["list", "short", "sinks"]),
        ("playback streams (id sink client ...)", ["list", "short", "sink-inputs"]),
    )
    for label, args in queries:
        output = pactl(env, user, *args) or "(nothing)"
        indented = output.replace("\n", "\n    ")
        print(f"[audio] {label}:\n    {indented}", flush=True)


def probe_google_fonts() -> None:
    """Report whether this machine can reach the Google Fonts hosts. If it can't,
    the page falls back to default fonts."""
    proxy = os.environ.get("https_proxy") or os.environ.get("HTTPS_PROXY") or "none"
    checks = {
        "fonts.googleapis.com": "https://fonts.googleapis.com/css2?family=Roboto",
        "fonts.gstatic.com": "https://fonts.gstatic.com/",
    }
    for host, url in checks.items():
        try:
            with urllib.request.urlopen(url, timeout=5) as response:
                print(f"Font host {host}: reachable (HTTP {response.status}).", flush=True)
        except urllib.error.HTTPError as error:
            # Any HTTP status means the host was reached (gstatic's root is a 404).
            print(f"Font host {host}: reachable (HTTP {error.code}).", flush=True)
        except Exception as error:  # DNS failure, timeout, TLS error, proxy needed...
            print(
                f"Font host {host}: NOT reachable ({error}); https_proxy={proxy}. "
                "Web fonts will fall back to defaults.",
                file=sys.stderr,
                flush=True,
            )


def gpu_kernel_driver(gpu_bdf: str) -> str:
    try:
        return (Path("/sys/bus/pci/devices") / gpu_bdf / "driver").resolve().name
    except OSError:
        return "unknown"


def parse_mode(mode: str) -> tuple[int, int]:
    match = re.match(r"^(\d+)x(\d+)", mode)
    if match is None:
        raise RuntimeError(f"Unrecognised mode: {mode}")
    return int(match.group(1)), int(match.group(2))


def start_window_manager(env: dict[str, str], config_path: Path) -> subprocess.Popen | None:
    """openbox manages one X screen (the one in env's DISPLAY), so each feed gets its own."""
    if shutil.which("openbox") is None:
        return None
    process = subprocess.Popen(["openbox", "--config-file", str(config_path)], env=env)
    time.sleep(1)
    if process.poll() is None:
        print(f"Window manager: openbox on {env['DISPLAY']}.", flush=True)
        return process
    return None


def start_cursor_hider(env: dict[str, str]) -> subprocess.Popen | None:
    unclutter = shutil.which("unclutter")
    if unclutter is None:
        return None
    probe = subprocess.run(
        [unclutter, "--help"], capture_output=True, text=True, check=False
    )
    if "--timeout" in probe.stdout + probe.stderr:  # unclutter-xfixes
        command = [unclutter, "--timeout", "1"]
    else:  # classic unclutter
        command = [unclutter, "-idle", "1", "-root"]
    print("Cursor auto-hide: unclutter (1s idle).", flush=True)
    return subprocess.Popen(command, env=env)


def detect_displays() -> list[DisplayTarget]:
    targets: list[DisplayTarget] = []
    for status_file in sorted(DRM_ROOT.glob("card*-*/status")):
        if status_file.read_text(encoding="utf-8").strip() != "connected":
            continue

        card, connector = status_file.parent.name.split("-", maxsplit=1)
        device_path = status_file.parent.joinpath("device").resolve()
        gpu_bdf = next(
            (part for part in reversed(device_path.parts) if PCI_ADDRESS.fullmatch(part)),
            None,
        )
        if gpu_bdf is None:
            continue

        modes_file = status_file.parent / "modes"
        modes = frozenset(modes_file.read_text(encoding="utf-8").splitlines())
        targets.append(DisplayTarget(card, connector, gpu_bdf, modes))

    return targets


def choose_display(targets: list[DisplayTarget], mode: str) -> DisplayTarget:
    """The SD display: an HDMI output on the amdgpu card (the APU) that supports the SD mode,
    falling back to any HDMI output, then any output."""
    if not targets:
        raise RuntimeError("No connected DRM display was detected.")

    usable = [target for target in targets if mode in target.modes]
    if not usable:
        connectors = ", ".join(target.connector for target in targets)
        raise RuntimeError(f"No connected display supports {mode}. Detected: {connectors}")

    def rank(target: DisplayTarget) -> tuple[bool, bool]:
        return gpu_kernel_driver(target.gpu_bdf) != "amdgpu", "HDMI" not in target.connector.upper()

    return min(usable, key=rank)


def choose_hd_display(
    targets: list[DisplayTarget], sd: DisplayTarget, mode: str
) -> DisplayTarget | None:
    """The HD display: another connected output that supports the HD mode, preferring the SD display's
    own card (so the APU renders both) over a second GPU."""
    usable = [target for target in targets if target != sd and (mode in CUSTOM_MODES or mode in target.modes)]
    return min(usable, key=lambda target: target.gpu_bdf != sd.gpu_bdf, default=None)


def device_section(identifier: str, bus_id: str, driver: str) -> str:
    # TearFree (vsynced output) is supported by the amdgpu and modesetting X drivers.
    return f'''Section "Device"
    Identifier "{identifier}"
    Driver "{driver}"
    BusID "{bus_id}"
    Option "AccelMethod" "glamor"
    Option "TearFree" "true"
EndSection
'''


def write_xorg_config(sd: DisplayTarget, sd_driver: str, hd: DisplayTarget | None, hd_driver: str) -> str:
    """One X screen per GPU: screen 0 on the SD GPU, screen 1 when the HD display is on a second GPU.
    No Modes lines: each output's mode is set with xrandr once the server is up."""
    if hd is not None and hd.gpu_bdf == sd.gpu_bdf:
        hd = None  # same card: its output is just another output of screen 0
    hd_layout = '    Screen 1 "HDScreen" RightOf "SDScreen"\n' if hd else ""
    hd_sections = (
        device_section("HDGPU", hd.xorg_bus_id, hd_driver)
        + '''
Section "Screen"
    Identifier "HDScreen"
    Device "HDGPU"
    DefaultDepth 24
EndSection
'''
        if hd
        else ""
    )
    contents = f'''Section "ServerLayout"
    Identifier "HazeDisplay"
    Screen 0 "SDScreen"
{hd_layout}EndSection

Section "ServerFlags"
    Option "AutoAddGPU" "off"
    Option "BlankTime" "0"
    Option "StandbyTime" "0"
    Option "SuspendTime" "0"
    Option "OffTime" "0"
EndSection

Section "Extensions"
    Option "DPMS" "Disable"
EndSection

{device_section("SDGPU", sd.xorg_bus_id, sd_driver)}
Section "Screen"
    Identifier "SDScreen"
    Device "SDGPU"
    DefaultDepth 24
EndSection

{hd_sections}'''
    with tempfile.NamedTemporaryFile(
        mode="w", encoding="utf-8", prefix="haze-xorg-", suffix=".conf", delete=False
    ) as config:
        config.write(contents)
        config_path = config.name
    # NamedTemporaryFile creates the file 0600. Xorg (or Xorg.wrap) may read it
    # under a different effective uid, so make it world-readable.
    os.chmod(config_path, 0o644)
    return config_path


AMDGPU_DDX_PATHS = (
    "/usr/lib64/xorg/modules/drivers/amdgpu_drv.so",
    "/usr/lib/xorg/modules/drivers/amdgpu_drv.so",
)


def choose_xorg_driver(target: DisplayTarget, requested: str) -> str:
    """The amdgpu X driver for amdgpu cards when installed (most reliable TearFree); modesetting for
    everything else, including nouveau (the proprietary nvidia X driver isn't used)."""
    if requested != "auto":
        print(f"Xorg driver for {target.gpu_bdf}: {requested} (requested).", flush=True)
        return requested
    if gpu_kernel_driver(target.gpu_bdf) == "amdgpu" and any(Path(path).exists() for path in AMDGPU_DDX_PATHS):
        print(f"Xorg driver for {target.gpu_bdf}: amdgpu.", flush=True)
        return "amdgpu"
    print(f"Xorg driver for {target.gpu_bdf}: modesetting.", flush=True)
    return "modesetting"


def xrandr(screen: int, *args: str, check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["xrandr", "--screen", str(screen), *args], check=check, capture_output=True, text=True
    )


def xrandr_mode_rates(screen: int, output: str, mode: str) -> tuple[list[float], float | None]:
    """All refresh rates xrandr lists for this output's mode, and the active one."""
    rates: list[float] = []
    active: float | None = None
    in_output = False
    for line in xrandr(screen, "--query").stdout.splitlines():
        if line and not line[0].isspace():
            in_output = line.split()[0] == output
            continue
        tokens = line.split()
        if not in_output or not tokens or tokens[0] != mode:
            continue
        for token in tokens[1:]:
            match = re.match(r"^(\d+(?:\.\d+)?)([*+]*)$", token)
            if match is None:
                continue
            rate = float(match.group(1))
            rates.append(rate)
            if "*" in match.group(2):
                active = rate
    return rates, active


def connected_xrandr_outputs(screen: int) -> list[str]:
    return [
        match.group(1)
        for line in xrandr(screen, "--query").stdout.splitlines()
        if (match := re.match(r"^(\S+) connected(?:\s|$)", line))
    ]


def choose_sd_output(target: DisplayTarget, outputs: list[str]) -> str:
    """X names outputs differently from the kernel (the amdgpu X driver counts from 0: HDMI-A-0)."""
    candidates = [name for name in outputs if "HDMI" in name.upper()]
    connector_number = target.connector.rsplit("-", maxsplit=1)[-1]
    matching = [name for name in candidates if name.rsplit("-", maxsplit=1)[-1] == connector_number]
    if len(matching) == 1:
        return matching[0]
    if len(candidates) == 1:
        return candidates[0]
    if len(outputs) == 1:
        return outputs[0]
    raise RuntimeError(f"Could not map DRM connector {target.connector} to an X11 output: {outputs}")


def add_custom_mode(screen: int, mode: str, outputs: list[str]) -> None:
    """Defines a CUSTOM_MODES entry in X and offers it on `outputs`. Both calls are best effort: --newmode
    fails if the mode already exists, and an output that rejects the mode just won't list it afterwards."""
    clock, *timing = CUSTOM_MODES[mode]
    xrandr(screen, "--newmode", mode, clock, *timing, check=False)
    for name in outputs:
        xrandr(screen, "--addmode", name, mode, check=False)


def choose_hd_output(screen: int, outputs: list[str], mode: str) -> str | None:
    """A connected output on the given X screen that lists the HD mode (SD's output already excluded)."""
    return next((name for name in outputs if xrandr_mode_rates(screen, name, mode)[0]), None)


def set_mode(feed: Feed, off: list[str], refresh: float) -> None:
    """Sets the feed's output to its mode and position at the listed rate nearest `refresh` (within 1 Hz),
    and turns off the `off` outputs (connected but unused) so nothing mirrors over a feed."""
    rates, _ = xrandr_mode_rates(feed.screen, feed.output, feed.mode)
    nearest = min(rates, key=lambda rate: abs(rate - refresh), default=None)
    command = ["--output", feed.output, "--mode", feed.mode, "--pos", f"{feed.x}x0"]
    if feed.name == "sd":
        command.append("--primary")
    for name in off:
        command += ["--output", name, "--off"]
    if nearest is not None and abs(nearest - refresh) <= 1.0:
        attempt = xrandr(feed.screen, *command, "--rate", f"{nearest:.2f}", check=False)
        if attempt.returncode == 0:
            command = []
        else:
            print(f"xrandr rejected {nearest:.2f} Hz: {attempt.stderr.strip()}", file=sys.stderr, flush=True)
    else:
        print(
            f"No rate near {refresh:g} Hz for {feed.mode} on {feed.output} "
            f"(available: {rates or 'unknown'}); leaving the default rate.",
            file=sys.stderr,
            flush=True,
        )
    if command:
        xrandr(feed.screen, *command)
    _, active = xrandr_mode_rates(feed.screen, feed.output, feed.mode)
    print(f"{feed.name.upper()} {feed.output} (screen {feed.screen}): {feed.mode} at {active if active is not None else '?'} Hz.", flush=True)


def give_to_user(user: BrowserUser, *paths: Path) -> None:
    if user.switch:
        for path in paths:
            shutil.chown(path, user.uid, user.gid)


def browser_env(user: BrowserUser, work_dir: Path, hinting: str, subpixel: str) -> dict[str, str]:
    """The environment shared by the browsers (and pactl): the desktop user's home, PipeWire and X access."""
    env = os.environ.copy()
    fontconfig = work_dir / "fonts.conf"
    fontconfig.write_text(
        FONTCONFIG_OVERRIDES.replace("@@HINTING@@", hinting).replace("@@SUBPIXEL@@", subpixel),
        encoding="utf-8",
    )
    give_to_user(user, fontconfig)
    env["FONTCONFIG_FILE"] = str(fontconfig)
    env["NO_AT_BRIDGE"] = "1"
    env["GTK_A11Y"] = "none"
    if user.switch:
        env["HOME"] = str(user.home)
        env["USER"] = env["LOGNAME"] = pwd.getpwuid(user.uid).pw_name
        env["XDG_RUNTIME_DIR"] = str(user.runtime_dir)
        bus = user.runtime_dir / "bus"
        if bus.exists():
            env["DBUS_SESSION_BUS_ADDRESS"] = f"unix:path={bus}"
        pulse = user.runtime_dir / "pulse" / "native"
        if pulse.exists():
            env["PULSE_SERVER"] = f"unix:{pulse}"
        # The X cookie xinit made is root's; hand the user a copy.
        cookie = os.environ.get("XAUTHORITY")
        if cookie:
            copy = work_dir / "Xauthority"
            shutil.copyfile(cookie, copy)
            give_to_user(user, copy)
            env["XAUTHORITY"] = str(copy)
    return env


def screen_display(screen: int) -> str:
    """DISPLAY for an X screen of this server (xinit sets DISPLAY to the server, e.g. ':0')."""
    return f"{os.environ['DISPLAY'].split('.')[0]}.{screen}"


def feed_sink(feed: Feed, feeds: list[Feed], env: dict[str, str], user: BrowserUser) -> str | None:
    """The sink a feed plays to: --sd-sink/--hd-sink if given, else its GPU's HDMI sink. An HD feed on
    the SD feed's card gets none (that sink is SD's), so without --hd-sink it is muted."""
    if feed.sink:
        return feed.sink
    if feed.name != "sd" and any(other.name == "sd" and other.gpu_bdf == feed.gpu_bdf for other in feeds):
        return None
    return hdmi_sink(env, user, feed.gpu_bdf)


def start_browser(
    feed: Feed, sink: str | None, user: BrowserUser, base_env: dict[str, str], work_dir: Path, chromium: str
) -> subprocess.Popen:
    profile = work_dir / f"{feed.name}-profile"  # a fresh profile each run, so no restore prompts or stale caches
    profile.mkdir()
    give_to_user(user, profile)

    env = {**base_env, "DISPLAY": screen_display(feed.screen)}
    if gpu_kernel_driver(feed.gpu_bdf) == "amdgpu":
        env["LIBVA_DRIVER_NAME"] = "radeonsi"
    # With no sink, the SD feed falls back to the default output and the HD feed is muted, so it can
    # never double up on the SD feed's audio.
    muted = sink is None and feed.name != "sd"
    if sink:
        env["PULSE_SINK"] = sink

    width, height = feed.size
    command = [
        chromium,
        *CHROMIUM_FLAGS,
        *(AMD_VIDEO_DECODE_FLAGS if gpu_kernel_driver(feed.gpu_bdf) == "amdgpu" else ()),
        f"--class={feed.wm_class}",
        f"--user-data-dir={profile}",
        f"--window-position={feed.x},0",
        f"--window-size={width},{height}",
        *(["--mute-audio"] if muted else []),
        *(["--no-sandbox"] if user.uid == 0 else []),  # Chromium refuses to run as root otherwise
        feed.url,
    ]
    audio = sink or ("muted, no audio sink of its own (see --hd-sink)" if muted else "default output")
    print(f"{feed.name.upper()}: Chromium on {env['DISPLAY']} {feed.output} -> {feed.url} (audio: {audio}).", flush=True)
    return subprocess.Popen(command, env=env, **user.spawn_args)


def run_feeds(feeds: list[Feed], cursor: str, hinting: str, subpixel: str) -> int:
    """Opens every feed's browser and waits. If any browser exits, they all stop and this returns,
    so the service restarts the whole display rather than leave one feed dead."""
    chromium = shutil.which("chromium-browser") or shutil.which("chromium")
    if chromium is None:
        raise RuntimeError("Chromium was not found in PATH (sudo dnf install chromium).")

    user = browser_user()
    work_dir = Path(tempfile.mkdtemp(prefix="haze-kiosk-"))
    give_to_user(user, work_dir)
    print(f"Font rendering: hint{hinting}, subpixel {subpixel}.", flush=True)

    env = browser_env(user, work_dir, hinting, subpixel)
    threading.Thread(target=probe_google_fonts, daemon=True).start()

    helpers: list[subprocess.Popen] = []
    browsers: list[subprocess.Popen] = []
    try:
        for screen in sorted({feed.screen for feed in feeds}):
            screen_env = {**os.environ, "DISPLAY": screen_display(screen)}
            if shutil.which("xset") is not None:
                # Harmless if DPMS is already disabled in the server config.
                subprocess.run(["xset", "s", "off", "s", "noblank", "-dpms"], env=screen_env, check=False)
            wm_config = work_dir / f"openbox-{screen}.xml"
            wm_config.write_text(openbox_config([feed for feed in feeds if feed.screen == screen]), encoding="utf-8")
            window_manager = start_window_manager(screen_env, wm_config)
            if window_manager is None:
                print(
                    "No window manager (install openbox): windows will sit at --window-position, "
                    "without true fullscreen.",
                    file=sys.stderr,
                    flush=True,
                )
            else:
                helpers.append(window_manager)
            if cursor == "auto":
                hider = start_cursor_hider(screen_env)
                if hider is not None:
                    helpers.append(hider)

        browsers = [
            start_browser(feed, feed_sink(feed, feeds, env, user), user, env, work_dir, chromium) for feed in feeds
        ]
        threading.Thread(target=report_audio_state, args=(env, user), daemon=True).start()
        while all(browser.poll() is None for browser in browsers):
            time.sleep(1)
        exited = next(browser for browser in browsers if browser.poll() is not None)
        print(f"A browser exited with code {exited.returncode}; stopping the display.", file=sys.stderr, flush=True)
        return exited.returncode
    except KeyboardInterrupt:
        return 0
    finally:
        for process in (*browsers, *helpers):
            process.terminate()
        for process in (*browsers, *helpers):
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
        shutil.rmtree(work_dir, ignore_errors=True)


def run_browser_session(args: argparse.Namespace) -> int:
    """Runs inside X (started by xinit): sets each output's mode and position, then opens the browsers."""
    sd_target = DisplayTarget(card="", connector=args.connector, gpu_bdf=args.gpu_bdf, modes=frozenset({args.mode}))
    outputs = {0: connected_xrandr_outputs(0)}
    sd = Feed("sd", 0, choose_sd_output(sd_target, outputs[0]), args.mode, args.url, args.gpu_bdf, sink=args.sd_sink)
    feeds = [sd]

    if args.hd_gpu_bdf:
        # Same card: another output of screen 0, to the right of SD. Second GPU: screen 1.
        screen = 0 if args.hd_gpu_bdf == args.gpu_bdf else 1
        outputs.setdefault(screen, connected_xrandr_outputs(screen))
        candidates = [name for name in outputs[screen] if name != sd.output]
        if args.hd_mode in CUSTOM_MODES:
            add_custom_mode(screen, args.hd_mode, candidates)
        hd_output = choose_hd_output(screen, candidates, args.hd_mode)
        if hd_output is None:
            print(
                f"No connected output on screen {screen} lists {args.hd_mode}; running SD only. Outputs: {candidates}",
                file=sys.stderr,
                flush=True,
            )
        else:
            x = parse_mode(args.mode)[0] if screen == 0 else 0
            feeds.append(Feed("hd", screen, hd_output, args.hd_mode, args.hd_url, args.hd_gpu_bdf, x, args.hd_sink))

    used = {feed.output for feed in feeds}
    for feed in feeds:
        set_mode(feed, [name for name in outputs[feed.screen] if name not in used], args.refresh)

    for feed in feeds:
        print(f"{feed.name.upper()} GPU {feed.gpu_bdf}: kernel driver {gpu_kernel_driver(feed.gpu_bdf)}.", flush=True)

    return run_feeds(feeds, args.cursor, args.hinting, args.subpixel)


def print_permission_hints() -> None:
    print(
        "\nIf the error above mentions a permission, tty, or path problem:\n"
        "  * 'Cannot open /dev/tty0' / 'Only console users are allowed to run the X server':\n"
        "      run from a real local console (not ssh), or pass --vt 1, or set\n"
        "      allowed_users=anybody and needs_root_rights=yes in /etc/X11/Xwrapper.config.\n"
        "  * 'Unable to locate/open config file' for /tmp/haze-xorg-*.conf:\n"
        "      setuid Xorg.wrap rejects absolute -config paths for non-root users;\n"
        "      run with sudo (use `sudo -E`), or copy the config into /etc/X11/xorg.conf.d/.\n"
        "  * 'Server is already active for display 0' / '/tmp/.X0-lock':\n"
        "      another X session owns :0; stop it or pass --display :1.\n"
        "  * The HD card took the server down: rerun with --no-hd to confirm, then check its kernel driver\n"
        "      (lspci -k; a GeForce 210 needs nouveau).\n"
        "  * No sound: each feed plays to its own GPU's HDMI sink (pactl list short sinks).\n",
        file=sys.stderr,
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default=DEFAULT_SD_URL, help="SD feed page (APU HDMI).")
    parser.add_argument("--mode", default=DEFAULT_SD_MODE, help="SD feed mode.")
    parser.add_argument("--hd-url", default=DEFAULT_HD_URL, help="HD feed page.")
    parser.add_argument(
        "--hd-mode",
        default=DEFAULT_HD_MODE,
        help="HD feed mode. 1920x1080i sends interlaced 1080i59.94, which the monitor doesn't advertise, so it is "
        "defined with xrandr --newmode and added to the HD output.",
    )
    parser.add_argument("--no-hd", action="store_true", help="Run the SD feed only.")
    parser.add_argument("--sd-sink", help="PipeWire sink for the SD feed's audio (default: its GPU's HDMI sink).")
    parser.add_argument(
        "--hd-sink",
        help="PipeWire sink for the HD feed's audio. Needed when HD shares the SD card: both outputs then need "
        "the card's Pro Audio profile, which gives each its own sink (pactl list short sinks).",
    )
    parser.add_argument("--display", default=DEFAULT_DISPLAY)
    parser.add_argument(
        "--vt",
        type=int,
        default=None,
        help="Virtual terminal for Xorg to take over (e.g. 1). Helps when not started from a console.",
    )
    parser.add_argument(
        "--cursor",
        choices=("auto", "hidden", "visible"),
        default="auto",
        help="auto: hide after 1s idle (unclutter), or always hide if unclutter is missing; "
        "hidden: never show; visible: leave alone.",
    )
    parser.add_argument(
        "--hinting",
        choices=("none", "slight", "medium", "full"),
        default="slight",
        help="Font hinting strength. Try 'none' if text looks uneven or distorted.",
    )
    parser.add_argument(
        "--subpixel",
        choices=("none", "rgb", "bgr", "vrgb", "vbgr"),
        default="none",
        help="Subpixel order for antialiasing. 'none' is grayscale; try 'rgb' on a direct HDMI panel.",
    )
    parser.add_argument(
        "--refresh",
        type=float,
        default=60.0,
        help="Refresh rate in Hz for both feeds; the closest rate each display lists for its mode is used.",
    )
    parser.add_argument(
        "--xdriver",
        choices=("auto", "amdgpu", "modesetting"),
        default="auto",
        help="Xorg video driver for the SD GPU. auto uses the amdgpu X driver when installed.",
    )
    parser.add_argument("--session", action="store_true", help=argparse.SUPPRESS)
    parser.add_argument("--connector", help=argparse.SUPPRESS)
    parser.add_argument("--gpu-bdf", help=argparse.SUPPRESS)
    parser.add_argument("--hd-gpu-bdf", help=argparse.SUPPRESS)  # set for the session when there is an HD display
    return parser.parse_args()


def main() -> int:
    args = parse_args()

    if args.session:
        return run_browser_session(args)

    for command in ("xinit", "xrandr"):
        if shutil.which(command) is None:
            raise RuntimeError(f"Required command was not found in PATH: {command}")

    targets = detect_displays()
    sd = choose_display(targets, args.mode)
    hd = None if args.no_hd else choose_hd_display(targets, sd, args.hd_mode)
    if hd is None and not args.no_hd:
        print(f"No display with {args.hd_mode} is connected besides the SD one; starting SD only.", flush=True)
    xorg_config = write_xorg_config(
        sd, choose_xorg_driver(sd, args.xdriver), hd, choose_xorg_driver(hd, "auto") if hd else ""
    )

    # "auto" needs unclutter for idle-hiding; without it, hide the cursor
    # permanently at the server level rather than leave it on screen.
    hide_cursor_in_server = args.cursor == "hidden" or (
        args.cursor == "auto" and shutil.which("unclutter") is None
    )
    if args.cursor == "auto" and hide_cursor_in_server:
        print(
            "unclutter not found; hiding the cursor permanently (-nocursor). "
            "Install unclutter-xfixes for hide-on-idle instead.",
            flush=True,
        )
    script = str(Path(__file__).resolve())
    command = [
        "xinit",
        sys.executable,
        script,
        "--session",
        "--connector",
        sd.connector,
        "--gpu-bdf",
        sd.gpu_bdf,
        "--mode",
        args.mode,
        "--url",
        args.url,
        "--hd-mode",
        args.hd_mode,
        "--hd-url",
        args.hd_url,
        *(["--hd-gpu-bdf", hd.gpu_bdf] if hd else []),
        *(["--sd-sink", args.sd_sink] if args.sd_sink else []),
        *(["--hd-sink", args.hd_sink] if args.hd_sink else []),
        "--cursor",
        args.cursor,
        "--hinting",
        args.hinting,
        "--subpixel",
        args.subpixel,
        "--refresh",
        str(args.refresh),
        "--",
        args.display,
        *([f"vt{args.vt}"] if args.vt else []),
        "-config",
        xorg_config,
        "-nolisten",
        "tcp",
        "-dpi",
        "96",
        *(["-nocursor"] if hide_cursor_in_server else []),
    ]

    print(
        f"Starting: SD on {sd.gpu_bdf} {sd.connector} at {args.mode}"
        + (f", HD on {hd.gpu_bdf} {hd.connector} at {args.hd_mode}" if hd else "")
        + ".",
        flush=True,
    )
    try:
        returncode = subprocess.run(command, check=False).returncode
    finally:
        Path(xorg_config).unlink(missing_ok=True)

    if returncode != 0:
        print_permission_hints()
    return returncode


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (OSError, RuntimeError, subprocess.CalledProcessError) as error:
        print(f"start_display_kiosk: {error}", file=sys.stderr)
        raise SystemExit(1)
