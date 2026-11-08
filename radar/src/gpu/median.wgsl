// First smoothing pass: premultiplies the raw frame, then takes the per-channel median of a square window.
// Removes the single-pixel speckle and stair-steps of the source tiles without smearing them into the blur.

struct MedianParams {
    radius: i32, // 0, 1 or 2
    pad0: i32,
    pad1: i32,
    pad2: i32,
}

@group(0) @binding(0) var source: texture_2d<f32>;
@group(0) @binding(1) var<uniform> params: MedianParams;

@fragment
fn fs(in: Varying) -> @location(0) vec4f {
    let max_coord = vec2i(textureDimensions(source)) - vec2i(1);
    let center = vec2i(in.position.xy);

    var samples: array<vec4f, 25>;
    var count = 0;
    for (var y = -params.radius; y <= params.radius; y++) {
        for (var x = -params.radius; x <= params.radius; x++) {
            let color = textureLoad(source, clamp(center + vec2i(x, y), vec2i(0), max_coord), 0);
            samples[count] = vec4f(color.rgb * color.a, color.a);
            count++;
        }
    }

    // Selection sort, stopping once the middle is in place. min/max work per channel.
    let middle = count / 2;
    for (var i = 0; i <= middle; i++) {
        for (var j = i + 1; j < count; j++) {
            let low = min(samples[i], samples[j]);
            let high = max(samples[i], samples[j]);
            samples[i] = low;
            samples[j] = high;
        }
    }
    return samples[middle];
}
