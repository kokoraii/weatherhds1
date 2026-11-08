#!/usr/bin/env bash
# Builds the radar engine into public/wasm, which is served and embedded with the rest of public/.
# Needs: rustup target add wasm32-unknown-unknown
#        cargo install wasm-bindgen-cli --version 0.2.130   (must match the version pinned in radar/Cargo.toml)
set -euo pipefail
cd "$(dirname "$0")/.."

cargo build --manifest-path radar/Cargo.toml --release --target wasm32-unknown-unknown
wasm-bindgen --target web --out-dir public/wasm \
    radar/target/wasm32-unknown-unknown/release/weatherhds_radar.wasm
ls -la public/wasm
