// Second and third smoothing passes: one axis of a Gaussian blur over premultiplied colour.

struct GaussianParams {
    direction: vec2i,
    radius: i32,
    sigma: f32,
}

@group(0) @binding(0) var source: texture_2d<f32>;
@group(0) @binding(1) var<uniform> params: GaussianParams;

@fragment
fn fs(in: Varying) -> @location(0) vec4f {
    let max_coord = vec2i(textureDimensions(source)) - vec2i(1);
    let center = vec2i(in.position.xy);

    var sum = vec4f(0.0);
    var total = 0.0;
    for (var i = -params.radius; i <= params.radius; i++) {
        let weight = exp(-f32(i * i) / max(2.0 * params.sigma * params.sigma, 0.0001));
        sum += weight * textureLoad(source, clamp(center + params.direction * i, vec2i(0), max_coord), 0);
        total += weight;
    }
    return sum / total;
}
