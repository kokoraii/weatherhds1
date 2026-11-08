// Last smoothing pass: an unsharp mask over premultiplied colour, restoring the crispness the blur took.

struct SharpenParams {
    amount: f32,
    pad0: f32,
    pad1: f32,
    pad2: f32,
}

@group(0) @binding(0) var source: texture_2d<f32>;
@group(0) @binding(1) var<uniform> params: SharpenParams;

@fragment
fn fs(in: Varying) -> @location(0) vec4f {
    let max_coord = vec2i(textureDimensions(source)) - vec2i(1);
    let center = vec2i(in.position.xy);

    // A 3x3 tent filter is the blurred copy the mask subtracts.
    var blurred = vec4f(0.0);
    for (var y = -1; y <= 1; y++) {
        for (var x = -1; x <= 1; x++) {
            let weight = f32((2 - abs(x)) * (2 - abs(y)));
            blurred += weight * textureLoad(source, clamp(center + vec2i(x, y), vec2i(0), max_coord), 0);
        }
    }
    blurred /= 16.0;

    let color = textureLoad(source, center, 0);
    let sharpened = clamp(color + params.amount * (color - blurred), vec4f(0.0), vec4f(1.0));
    // Overshoot can't make a premultiplied colour brighter than its alpha.
    return vec4f(min(sharpened.rgb, vec3f(sharpened.a)), sharpened.a);
}
