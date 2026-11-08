// Draws one moment of the loop: warps up to four neighbouring frames along the motion fields so they all
// line up at that moment, blends them with a smooth weighting in time, then cleans up the edges and adds a
// shadow. This runs every display frame, so in-between frames are never stored.

struct CompositeParams {
    uv_origin: vec2f,
    uv_scale: vec2f,
    frame_size: vec2f,
    shadow_offset: vec2f,     // in the frame's 0..1 coordinates
    layers: vec4<i32>,        // the frames to blend
    weights: vec4f,           // each frame's share at this moment; unused slots are 0
    flow_layers: vec4<i32>,   // xyz: the motion fields of the frame pairs around this moment
    motion: array<vec4f, 4>,  // per frame: how far along each of those fields it moves to this moment
    opacity: f32,
    feather: f32,
    edge_low: f32,
    edge_high: f32,
    shadow_opacity: f32,
    pad0: f32,
    pad1: f32,
    pad2: f32,
}

@group(0) @binding(0) var frames: texture_2d_array<f32>;
@group(0) @binding(1) var flows: texture_2d_array<f32>;
@group(0) @binding(2) var linear_clamp: sampler;
@group(0) @binding(3) var<uniform> params: CompositeParams;

// Catmull-Rom lookup in five bilinear taps (the four corner taps barely contribute, so they are dropped).
// Tiles are magnified on screen: plain bilinear shows the source grid as diamonds once the edge remap
// below sharpens it, and a B-spline would soften the frame again. This one passes through the source
// pixels, so it adds no blur of its own.
fn cubic(uv: vec2f, layer: i32) -> vec4f {
    let position = uv * params.frame_size;
    let center = floor(position - 0.5) + 0.5;
    let f = position - center;
    let f2 = f * f;
    let f3 = f2 * f;

    let w0 = -0.5 * f3 + f2 - 0.5 * f;
    let w1 = 1.5 * f3 - 2.5 * f2 + 1.0;
    let w2 = -1.5 * f3 + 2.0 * f2 + 0.5 * f;
    let w3 = 0.5 * f3 - 0.5 * f2;

    let w12 = w1 + w2;
    let middle = (center + w2 / w12) / params.frame_size;
    let before = (center - 1.0) / params.frame_size;
    let after = (center + 2.0) / params.frame_size;

    let sum = textureSampleLevel(frames, linear_clamp, vec2f(middle.x, before.y), layer, 0.0) * (w12.x * w0.y)
            + textureSampleLevel(frames, linear_clamp, vec2f(before.x, middle.y), layer, 0.0) * (w0.x * w12.y)
            + textureSampleLevel(frames, linear_clamp, middle, layer, 0.0) * (w12.x * w12.y)
            + textureSampleLevel(frames, linear_clamp, vec2f(after.x, middle.y), layer, 0.0) * (w3.x * w12.y)
            + textureSampleLevel(frames, linear_clamp, vec2f(middle.x, after.y), layer, 0.0) * (w12.x * w3.y);
    let total = w12.x * w0.y + w0.x * w12.y + w12.x * w12.y + w3.x * w12.y + w12.x * w3.y;

    // The negative lobes overshoot at hard edges; keep the result a valid premultiplied colour.
    let color = clamp(sum / total, vec4f(0.0), vec4f(1.0));
    return vec4f(min(color.rgb, vec3f(color.a)), color.a);
}

fn edge_alpha(alpha: f32) -> f32 {
    if (params.edge_high > params.edge_low) {
        return smoothstep(params.edge_low, params.edge_high, alpha);
    }
    return alpha;
}

@fragment
fn fs(in: Varying) -> @location(0) vec4f {
    let uv = params.uv_origin + in.uv * params.uv_scale;

    // Fades out at the edge of the fetched area, which only shows while the camera flies to a region.
    let edge = min(uv, vec2f(1.0) - uv);
    let inside = smoothstep(0.0, params.feather, min(edge.x, edge.y));

    let flow0 = textureSampleLevel(flows, linear_clamp, uv, params.flow_layers.x, 0.0).xy;
    let flow1 = textureSampleLevel(flows, linear_clamp, uv, params.flow_layers.y, 0.0).xy;
    let flow2 = textureSampleLevel(flows, linear_clamp, uv, params.flow_layers.z, 0.0).xy;
    var shift: array<vec2f, 4>;
    for (var i = 0; i < 4; i++) {
        let m = params.motion[i];
        shift[i] = m.x * flow0 + m.y * flow1 + m.z * flow2;
    }

    var color = vec4f(0.0);
    for (var i = 0; i < 4; i++) {
        if (params.weights[i] > 0.0) {
            color += params.weights[i] * cubic(uv - shift[i], params.layers[i]);
        }
    }

    if (params.edge_high > params.edge_low) {
        let alpha = edge_alpha(color.a);
        color = vec4f(color.rgb / max(color.a, 0.0001) * alpha, alpha);
    }

    // A hard black copy of this layer's outline, offset and showing only outside the imagery. Plain
    // bilinear is enough for something this faint, and keeps the cost down.
    if (params.shadow_opacity > 0.0) {
        var caster = 0.0;
        for (var i = 0; i < 4; i++) {
            if (params.weights[i] > 0.0) {
                let shifted = uv - params.shadow_offset - shift[i];
                caster += params.weights[i] * textureSampleLevel(frames, linear_clamp, shifted, params.layers[i], 0.0).a;
            }
        }
        color.a += (1.0 - color.a) * edge_alpha(caster) * params.shadow_opacity;
    }

    return color * params.opacity * inside;
}
