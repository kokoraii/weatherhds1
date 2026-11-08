/* tslint:disable */
/* eslint-disable */

export class RadarEngine {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    /**
     * Sets up a canvas's GPU device. Resolves to a RadarSurface. The canvas needn't be in the page yet.
     */
    createSurface(name: string, canvas: HTMLCanvasElement, options: any): Promise<any>;
    /**
     * Looks up the configured products. `config` is `config.radar` from config.js.
     */
    static create(config: any, api_key: string): Promise<RadarEngine>;
    /**
     * Every interest list and how far along its frames are.
     */
    inventory(): any;
    /**
     * One slice of background work. Call once per animation frame.
     */
    pump(now_ms: number): void;
}

export class RadarSurface {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    /**
     * Draws the current moment for this camera. Returns the on-screen frame's timestamp in seconds.
     */
    render(now_ms: number, lat: number, lon: number, zoom: number, width: number, height: number, pixel_ratio: number): number | undefined;
    /**
     * Plays a stack from its oldest frame, registering it first if needed.
     */
    show(request: any): void;
    /**
     * Registers interest in a stack { layers: [{ product, opacity }], frames, lat, lon, zoom, width, height }
     * and starts fetching its frames, ahead of other downloads if `urgent`. Layers go bottom first, and
     * `frames` is the top layer's count. Safe to call while the radar is off screen.
     */
    want(request: any, urgent: boolean): void;
    /**
     * The graphics backend in use, for the startup log.
     */
    readonly backend: string;
}

export type InitInput = RequestInfo | URL | Response | BufferSource | WebAssembly.Module;

export interface InitOutput {
    readonly memory: WebAssembly.Memory;
    readonly __wbg_radarengine_free: (a: number, b: number) => void;
    readonly __wbg_radarsurface_free: (a: number, b: number) => void;
    readonly radarengine_create: (a: any, b: number, c: number) => any;
    readonly radarengine_createSurface: (a: number, b: number, c: number, d: any, e: any) => any;
    readonly radarengine_inventory: (a: number) => [number, number, number];
    readonly radarengine_pump: (a: number, b: number) => void;
    readonly radarsurface_backend: (a: number) => [number, number];
    readonly radarsurface_render: (a: number, b: number, c: number, d: number, e: number, f: number, g: number, h: number) => [number, number];
    readonly radarsurface_show: (a: number, b: any) => [number, number];
    readonly radarsurface_want: (a: number, b: any, c: number) => [number, number];
    readonly wasm_bindgen__convert__closures_____invoke__h2e05593c52c03b92: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen__convert__closures_____invoke__h2e05593c52c03b92_12: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen__convert__closures_____invoke__h2e05593c52c03b92_13: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen__convert__closures_____invoke__h816bf3962d1a5d77: (a: number, b: number, c: any, d: any) => void;
    readonly wasm_bindgen__convert__closures_____invoke__h86f7b186558c03e1: (a: number, b: number, c: any) => [number, number];
    readonly wasm_bindgen__convert__closures_____invoke__h2c6ad39f197d3a42: (a: number, b: number, c: any) => void;
    readonly __wbindgen_malloc: (a: number, b: number) => number;
    readonly __wbindgen_realloc: (a: number, b: number, c: number, d: number) => number;
    readonly __wbindgen_free: (a: number, b: number, c: number) => void;
    readonly __wbindgen_exn_store: (a: number) => void;
    readonly __externref_table_alloc: () => number;
    readonly __wbindgen_externrefs: WebAssembly.Table;
    readonly __wbindgen_destroy_closure: (a: number, b: number) => void;
    readonly __externref_table_dealloc: (a: number) => void;
    readonly __wbindgen_start: () => void;
}

export type SyncInitInput = BufferSource | WebAssembly.Module;

/**
 * Instantiates the given `module`, which can either be bytes or
 * a precompiled `WebAssembly.Module`.
 *
 * @param {{ module: SyncInitInput }} module - Passing `SyncInitInput` directly is deprecated.
 *
 * @returns {InitOutput}
 */
export function initSync(module: { module: SyncInitInput } | SyncInitInput): InitOutput;

/**
 * If `module_or_path` is {RequestInfo} or {URL}, makes a request and
 * for everything else, calls `WebAssembly.instantiate` directly.
 *
 * @param {{ module_or_path: InitInput | Promise<InitInput> }} module_or_path - Passing `InitInput` directly is deprecated.
 *
 * @returns {Promise<InitOutput>}
 */
export default function __wbg_init (module_or_path?: { module_or_path: InitInput | Promise<InitInput> } | InitInput | Promise<InitInput>): Promise<InitOutput>;
