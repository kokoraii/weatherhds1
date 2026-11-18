// A MapLibre tile protocol that answers a request for tile z/x/y with the chosen layers of its four
// children at z+1, merged into one tile. OpenFreeMap only puts trunk route numbers and airports into its
// zoom 8+ tiles, so this lets the radar maps (zoom 5-7) show them. MapLibre won't fetch deeper tiles itself:
// vector sources must use 512 px tiles, and a source with any other size never loads.
//
// Only line and point layers are merged (polygons would need re-clipping). Mapbox Vector Tile reference:
// https://github.com/mapbox/vector-tile-spec/tree/master/2.1

const PROTOCOL = "detail";

// ---- protobuf reading ----

function readVarint(buf, pos) {
    let value = 0;
    let shift = 0;
    let byte;
    do {
        byte = buf[pos.i++];
        value += (byte & 0x7f) * 2 ** shift;
        shift += 7;
    } while (byte >= 0x80);
    return value;
}

/** Calls visit(field, wireType, value) for each field of a message; value is a number or a byte view. */
function readFields(buf, visit) {
    const pos = { i: 0 };
    while (pos.i < buf.length) {
        const key = readVarint(buf, pos);
        const field = Math.floor(key / 8);
        const wire = key & 7;
        if (wire === 0) visit(field, wire, readVarint(buf, pos));
        else if (wire === 2) {
            const length = readVarint(buf, pos);
            visit(field, wire, buf.subarray(pos.i, pos.i + length));
            pos.i += length;
        } else if (wire === 5) { visit(field, wire, buf.subarray(pos.i, pos.i + 4)); pos.i += 4; }
        else if (wire === 1) { visit(field, wire, buf.subarray(pos.i, pos.i + 8)); pos.i += 8; }
        else throw new Error(`unsupported wire type ${wire}`);
    }
}

function readPacked(bytes) {
    const out = [];
    const pos = { i: 0 };
    while (pos.i < bytes.length) out.push(readVarint(bytes, pos));
    return out;
}

// ---- protobuf writing ----

class Writer {
    bytes = [];
    varint(value) {
        while (value >= 0x80) {
            this.bytes.push((value % 0x80) | 0x80);
            value = Math.floor(value / 0x80);
        }
        this.bytes.push(value);
    }
    key(field, wire) { this.varint(field * 8 + wire); }
    message(field, bytes) { this.key(field, 2); this.varint(bytes.length); for (const b of bytes) this.bytes.push(b); }
    packed(field, values) { const inner = new Writer(); for (const v of values) inner.varint(v); this.message(field, inner.bytes); }
    uint(field, value) { this.key(field, 0); this.varint(value); }
}

const zigzag = (n) => (n << 1) ^ (n >> 31);
const unzigzag = (n) => (n >>> 1) ^ -(n & 1);
const text = new TextDecoder();
const encoder = new TextEncoder();

// ---- merging ----

/** Moves a child tile's geometry into its quadrant of the parent: x' = (x + qx * extent) / 2. */
function transformGeometry(commands, qx, qy, extent) {
    const out = [];
    let x = 0, y = 0; // child cursor
    let px = 0, py = 0; // parent cursor
    for (let i = 0; i < commands.length;) {
        const command = commands[i++];
        const id = command & 7;
        const count = command >> 3;
        out.push(command);
        if (id === 7) continue; // ClosePath has no parameters
        for (let n = 0; n < count; n++) {
            x += unzigzag(commands[i++]);
            y += unzigzag(commands[i++]);
            const nx = Math.round((x + qx * extent) / 2);
            const ny = Math.round((y + qy * extent) / 2);
            out.push(zigzag(nx - px), zigzag(ny - py));
            px = nx;
            py = ny;
        }
    }
    return out;
}

/** One output layer, gathering features from the children with shared key and value tables. */
class MergedLayer {
    keys = [];
    keyIndex = new Map();
    values = [];
    valueIndex = new Map();
    features = [];
    extent = 4096;

    constructor(name) { this.name = name; }

    #index(list, index, item, id) {
        let i = index.get(id);
        if (i === undefined) { i = list.length; list.push(item); index.set(id, i); }
        return i;
    }

    add(layerBytes, qx, qy) {
        const keys = [];
        const values = [];
        const features = [];
        readFields(layerBytes, (field, wire, value) => {
            if (field === 3) keys.push(text.decode(value));
            else if (field === 4) values.push(value);
            else if (field === 2) features.push(value);
            else if (field === 5) this.extent = value;
        });
        for (const featureBytes of features) {
            const feature = { tags: [], type: 0, geometry: [] };
            readFields(featureBytes, (field, wire, value) => {
                if (field === 2) feature.tags = readPacked(value);
                else if (field === 3) feature.type = value;
                else if (field === 4) feature.geometry = readPacked(value);
            });
            if (feature.type === 3) continue; // polygons aren't supported
            const tags = [];
            for (let t = 0; t < feature.tags.length; t += 2) {
                const key = keys[feature.tags[t]];
                const value = values[feature.tags[t + 1]];
                tags.push(
                    this.#index(this.keys, this.keyIndex, key, key),
                    this.#index(this.values, this.valueIndex, value, String.fromCharCode(...value)),
                );
            }
            this.features.push({ tags, type: feature.type, geometry: transformGeometry(feature.geometry, qx, qy, this.extent) });
        }
    }

    write(tile) {
        const layer = new Writer();
        layer.uint(15, 2);
        layer.message(1, encoder.encode(this.name));
        for (const feature of this.features) {
            const f = new Writer();
            f.packed(2, feature.tags);
            f.uint(3, feature.type);
            f.packed(4, feature.geometry);
            layer.message(2, f.bytes);
        }
        for (const key of this.keys) layer.message(3, encoder.encode(key));
        for (const value of this.values) layer.message(4, value);
        layer.uint(5, this.extent);
        tile.message(3, layer.bytes);
    }
}

/**
 * Registers the protocol. Sources then use tiles: ["detail://{z}/{x}/{y}"] and get, at each tile, the
 * `layers` of its children from `template` (a z/x/y tile URL). Returns nothing; call once.
 */
export function registerDetailTiles(template, layers) {
    maplibregl.addProtocol(PROTOCOL, async ({ url }, abort) => {
        const [z, x, y] = url.slice(PROTOCOL.length + 3).split("/").map(Number);
        const merged = new Map(layers.map((name) => [name, new MergedLayer(name)]));
        await Promise.all([0, 1].flatMap((qy) => [0, 1].map(async (qx) => {
            const childUrl = template.replace("{z}", z + 1).replace("{x}", 2 * x + qx).replace("{y}", 2 * y + qy);
            const response = await fetch(childUrl, { signal: abort.signal });
            if (!response.ok) return; // ocean and edge-of-world tiles can be missing
            const tile = new Uint8Array(await response.arrayBuffer());
            readFields(tile, (field, wire, layerBytes) => {
                if (field !== 3) return;
                let name = null;
                readFields(layerBytes, (f, w, v) => { if (f === 1) name = text.decode(v); });
                merged.get(name)?.add(layerBytes, qx, qy);
            });
        })));
        const tile = new Writer();
        for (const layer of merged.values()) if (layer.features.length) layer.write(tile);
        return { data: new Uint8Array(tile.bytes).buffer };
    });
}
