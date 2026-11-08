// A single triangle covering the target. uv runs (0,0) top-left to (1,1) bottom-right, matching frame rows.

struct Varying {
    @builtin(position) position: vec4f,
    @location(0) uv: vec2f,
}

@vertex
fn vs(@builtin(vertex_index) index: u32) -> Varying {
    let uv = vec2f(f32((index << 1u) & 2u), f32(index & 2u));
    return Varying(vec4f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, 0.0, 1.0), uv);
}
