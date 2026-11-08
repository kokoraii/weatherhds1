//! The per-display-frame pass that draws the interpolated layers to the canvas, bottom layer first.

use super::{begin_clear_pass, fullscreen_pipeline};
use bytemuck::{Pod, Zeroable};

/// Matches `CompositeParams` in composite.wgsl.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Pod, Zeroable)]
pub struct CompositeParams {
    pub uv_origin: [f32; 2],
    pub uv_scale: [f32; 2],
    pub frame_size: [f32; 2],
    pub shadow_offset: [f32; 2],
    pub layers: [i32; 4],
    pub weights: [f32; 4],
    pub flow_layers: [i32; 4],
    /// Per frame, the shares of `flow_layers` xyz; w is padding.
    pub motion: [[f32; 4]; 4],
    pub opacity: f32,
    pub feather: f32,
    pub edge_low: f32,
    pub edge_high: f32,
    pub shadow_opacity: f32,
    pub pad: [f32; 3],
}

pub struct Compositor {
    pipeline: wgpu::RenderPipeline,
    layout: wgpu::BindGroupLayout,
    sampler: wgpu::Sampler,
}

impl Compositor {
    pub fn new(device: &wgpu::Device, target: wgpu::TextureFormat) -> Self {
        let array_texture = |binding| wgpu::BindGroupLayoutEntry {
            binding,
            visibility: wgpu::ShaderStages::FRAGMENT,
            ty: wgpu::BindingType::Texture {
                sample_type: wgpu::TextureSampleType::Float { filterable: true },
                view_dimension: wgpu::TextureViewDimension::D2Array,
                multisampled: false,
            },
            count: None,
        };
        let layout = device.create_bind_group_layout(&wgpu::BindGroupLayoutDescriptor {
            label: Some("composite"),
            entries: &[
                array_texture(0),
                array_texture(1),
                wgpu::BindGroupLayoutEntry {
                    binding: 2,
                    visibility: wgpu::ShaderStages::FRAGMENT,
                    ty: wgpu::BindingType::Sampler(wgpu::SamplerBindingType::Filtering),
                    count: None,
                },
                wgpu::BindGroupLayoutEntry {
                    binding: 3,
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
        let sampler = device.create_sampler(&wgpu::SamplerDescriptor {
            label: Some("linear clamp"),
            address_mode_u: wgpu::AddressMode::ClampToEdge,
            address_mode_v: wgpu::AddressMode::ClampToEdge,
            mag_filter: wgpu::FilterMode::Linear,
            min_filter: wgpu::FilterMode::Linear,
            ..Default::default()
        });
        Self {
            pipeline: fullscreen_pipeline(
                device,
                "composite",
                include_str!("composite.wgsl"),
                &layout,
                target,
                Some(wgpu::BlendState::PREMULTIPLIED_ALPHA_BLENDING),
            ),
            layout,
            sampler,
        }
    }

    /// A bind group for one frame set. `params` is that set's own uniform buffer, since several sets are
    /// drawn in one pass and a shared buffer would hold only the last one's values.
    pub fn bind_group(
        &self,
        device: &wgpu::Device,
        frames: &wgpu::Texture,
        flows: &wgpu::Texture,
        params: &wgpu::Buffer,
    ) -> wgpu::BindGroup {
        let array_view = |texture: &wgpu::Texture| {
            texture.create_view(&wgpu::TextureViewDescriptor {
                dimension: Some(wgpu::TextureViewDimension::D2Array),
                ..Default::default()
            })
        };
        device.create_bind_group(&wgpu::BindGroupDescriptor {
            label: Some("composite"),
            layout: &self.layout,
            entries: &[
                wgpu::BindGroupEntry { binding: 0, resource: wgpu::BindingResource::TextureView(&array_view(frames)) },
                wgpu::BindGroupEntry { binding: 1, resource: wgpu::BindingResource::TextureView(&array_view(flows)) },
                wgpu::BindGroupEntry { binding: 2, resource: wgpu::BindingResource::Sampler(&self.sampler) },
                wgpu::BindGroupEntry { binding: 3, resource: params.as_entire_binding() },
            ],
        })
    }

    /// Clears the canvas texture and draws each bind group over it, in order.
    pub fn draw(&self, device: &wgpu::Device, queue: &wgpu::Queue, target: &wgpu::TextureView, layers: &[wgpu::BindGroup]) {
        let mut encoder = device.create_command_encoder(&Default::default());
        {
            let mut pass = begin_clear_pass(&mut encoder, target);
            pass.set_pipeline(&self.pipeline);
            for bind_group in layers {
                pass.set_bind_group(0, bind_group, &[]);
                pass.draw(0..3, 0..1);
            }
        }
        queue.submit([encoder.finish()]);
    }
}
