//! The clean-up passes each frame goes through once: median, a two-pass Gaussian, then an unsharp mask.

use super::{FRAME_FORMAT, fullscreen_pass, fullscreen_pipeline};
use crate::config::Tuning;
use bytemuck::{Pod, Zeroable};
use std::cell::RefCell;
use std::collections::HashMap;
use std::rc::Rc;
use wgpu::util::DeviceExt;

#[repr(C)]
#[derive(Clone, Copy, Pod, Zeroable)]
struct MedianParams {
    radius: i32,
    pad: [i32; 3],
}

#[repr(C)]
#[derive(Clone, Copy, Pod, Zeroable)]
struct GaussianParams {
    direction: [i32; 2],
    radius: i32,
    sigma: f32,
}

#[repr(C)]
#[derive(Clone, Copy, Pod, Zeroable)]
struct SharpenParams {
    amount: f32,
    pad: [f32; 3],
}

/// One scratch texture, with the parameters and bind group of the pass that reads it.
struct Stage {
    texture: wgpu::Texture,
    view: wgpu::TextureView,
    params: wgpu::Buffer,
    bind_group: wgpu::BindGroup,
}

/// Scratch textures for one frame size. Radars of one canvas share them.
struct Targets {
    /// Holds the uploaded frame; read by the median pass.
    raw: Stage,
    /// Holds the median result; read by the horizontal blur.
    median: Stage,
    /// Holds the horizontal blur; read by the vertical blur.
    blur_x: Stage,
    /// Holds the full blur; read by the sharpen pass, which writes the frame's layer.
    blur_y: Stage,
}

pub struct Smoother {
    median_pipeline: wgpu::RenderPipeline,
    gaussian_pipeline: wgpu::RenderPipeline,
    sharpen_pipeline: wgpu::RenderPipeline,
    layout: wgpu::BindGroupLayout,
    targets: RefCell<HashMap<(u32, u32), Rc<Targets>>>,
}

impl Smoother {
    pub fn new(device: &wgpu::Device) -> Self {
        let layout = device.create_bind_group_layout(&wgpu::BindGroupLayoutDescriptor {
            label: Some("smooth"),
            entries: &[
                wgpu::BindGroupLayoutEntry {
                    binding: 0,
                    visibility: wgpu::ShaderStages::FRAGMENT,
                    ty: wgpu::BindingType::Texture {
                        sample_type: wgpu::TextureSampleType::Float { filterable: false },
                        view_dimension: wgpu::TextureViewDimension::D2,
                        multisampled: false,
                    },
                    count: None,
                },
                wgpu::BindGroupLayoutEntry {
                    binding: 1,
                    visibility: wgpu::ShaderStages::FRAGMENT,
                    ty: wgpu::BindingType::Buffer {
                        ty: wgpu::BufferBindingType::Uniform,
                        has_dynamic_offset: false,
                        min_binding_size: None,
                    },
                    count: None,
                },
            ],
        });
        let pipeline = |label, source| fullscreen_pipeline(device, label, source, &layout, FRAME_FORMAT, None);
        Self {
            median_pipeline: pipeline("median", include_str!("median.wgsl")),
            gaussian_pipeline: pipeline("gaussian", include_str!("gaussian.wgsl")),
            sharpen_pipeline: pipeline("sharpen", include_str!("sharpen.wgsl")),
            layout,
            targets: RefCell::new(HashMap::new()),
        }
    }

    fn targets(&self, device: &wgpu::Device, size: (u32, u32)) -> Rc<Targets> {
        self.targets
            .borrow_mut()
            .entry(size)
            .or_insert_with(|| {
                let stage = |label: &str| {
                    let texture = device.create_texture(&wgpu::TextureDescriptor {
                        label: Some(label),
                        size: wgpu::Extent3d { width: size.0, height: size.1, depth_or_array_layers: 1 },
                        mip_level_count: 1,
                        sample_count: 1,
                        dimension: wgpu::TextureDimension::D2,
                        format: FRAME_FORMAT,
                        usage: wgpu::TextureUsages::TEXTURE_BINDING
                            | wgpu::TextureUsages::RENDER_ATTACHMENT
                            | wgpu::TextureUsages::COPY_DST,
                        view_formats: &[],
                    });
                    let view = texture.create_view(&Default::default());
                    // Every pass's parameters are 16 bytes, written fresh for each frame.
                    let params = device.create_buffer_init(&wgpu::util::BufferInitDescriptor {
                        label: Some(label),
                        contents: &[0; 16],
                        usage: wgpu::BufferUsages::UNIFORM | wgpu::BufferUsages::COPY_DST,
                    });
                    let bind_group = device.create_bind_group(&wgpu::BindGroupDescriptor {
                        label: Some(label),
                        layout: &self.layout,
                        entries: &[
                            wgpu::BindGroupEntry { binding: 0, resource: wgpu::BindingResource::TextureView(&view) },
                            wgpu::BindGroupEntry { binding: 1, resource: params.as_entire_binding() },
                        ],
                    });
                    Stage { texture, view, params, bind_group }
                };
                Rc::new(Targets {
                    raw: stage("raw"),
                    median: stage("median"),
                    blur_x: stage("blur x"),
                    blur_y: stage("blur y"),
                })
            })
            .clone()
    }

    /// Uploads a raw frame and writes its cleaned-up, premultiplied version to `destination`.
    pub fn run(
        &self,
        device: &wgpu::Device,
        queue: &wgpu::Queue,
        rgba: &[u8],
        size: (u32, u32),
        tuning: &Tuning,
        destination: &wgpu::TextureView,
    ) {
        let targets = self.targets(device, size);
        queue.write_texture(
            targets.raw.texture.as_image_copy(),
            rgba,
            wgpu::TexelCopyBufferLayout { offset: 0, bytes_per_row: Some(size.0 * 4), rows_per_image: None },
            wgpu::Extent3d { width: size.0, height: size.1, depth_or_array_layers: 1 },
        );

        let median = MedianParams { radius: tuning.median.min(2) as i32, pad: [0; 3] };
        let radius = (tuning.gaussian * 3.0).ceil().clamp(0.0, 12.0) as i32;
        let blur = |direction| GaussianParams { direction, radius, sigma: tuning.gaussian };
        let sharpen = SharpenParams { amount: tuning.sharpen, pad: [0.0; 3] };
        queue.write_buffer(&targets.raw.params, 0, bytemuck::bytes_of(&median));
        queue.write_buffer(&targets.median.params, 0, bytemuck::bytes_of(&blur([1, 0])));
        queue.write_buffer(&targets.blur_x.params, 0, bytemuck::bytes_of(&blur([0, 1])));
        queue.write_buffer(&targets.blur_y.params, 0, bytemuck::bytes_of(&sharpen));

        let mut encoder = device.create_command_encoder(&Default::default());
        fullscreen_pass(&mut encoder, &targets.median.view, &self.median_pipeline, &targets.raw.bind_group);
        fullscreen_pass(&mut encoder, &targets.blur_x.view, &self.gaussian_pipeline, &targets.median.bind_group);
        fullscreen_pass(&mut encoder, &targets.blur_y.view, &self.gaussian_pipeline, &targets.blur_x.bind_group);
        fullscreen_pass(&mut encoder, destination, &self.sharpen_pipeline, &targets.blur_y.bind_group);
        queue.submit([encoder.finish()]);
    }
}
