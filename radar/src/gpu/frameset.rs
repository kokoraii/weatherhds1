//! The GPU storage of one interest list: its frames and the motion fields between them.

use super::composite::CompositeParams;
use super::{FRAME_FORMAT, Gpu};
use crate::flow::FlowField;
use half::f16;

pub struct FrameSet {
    frames: wgpu::Texture,
    flows: wgpu::Texture,
    params: wgpu::Buffer,
    pub bind_group: wgpu::BindGroup,
}

impl FrameSet {
    /// `layers` comes from the inventory, which keeps it to counts WebGL2 stores as a 2D array.
    pub fn new(gpu: &Gpu, frame_size: (u32, u32), flow_size: (u32, u32), layers: u32) -> Self {
        let array = |label: &str, size: (u32, u32), format, usage| {
            gpu.device.create_texture(&wgpu::TextureDescriptor {
                label: Some(label),
                size: wgpu::Extent3d { width: size.0, height: size.1, depth_or_array_layers: layers },
                mip_level_count: 1,
                sample_count: 1,
                dimension: wgpu::TextureDimension::D2,
                format,
                usage,
                view_formats: &[],
            })
        };
        let frames = array(
            "frames",
            frame_size,
            FRAME_FORMAT,
            wgpu::TextureUsages::TEXTURE_BINDING | wgpu::TextureUsages::RENDER_ATTACHMENT,
        );
        let flows = array(
            "flows",
            flow_size,
            wgpu::TextureFormat::Rg16Float,
            wgpu::TextureUsages::TEXTURE_BINDING | wgpu::TextureUsages::COPY_DST,
        );
        let params = gpu.device.create_buffer(&wgpu::BufferDescriptor {
            label: Some("composite params"),
            size: size_of::<CompositeParams>() as u64,
            usage: wgpu::BufferUsages::UNIFORM | wgpu::BufferUsages::COPY_DST,
            mapped_at_creation: false,
        });
        let bind_group = gpu.compositor.bind_group(&gpu.device, &frames, &flows, &params);
        Self { frames, flows, params, bind_group }
    }

    /// A render target for one frame layer.
    pub fn layer_view(&self, layer: u32) -> wgpu::TextureView {
        self.frames.create_view(&wgpu::TextureViewDescriptor {
            dimension: Some(wgpu::TextureViewDimension::D2),
            base_array_layer: layer,
            array_layer_count: Some(1),
            ..Default::default()
        })
    }

    pub fn write_params(&self, queue: &wgpu::Queue, params: &CompositeParams) {
        queue.write_buffer(&self.params, 0, bytemuck::bytes_of(params));
    }

    /// Stores a motion field, converted from grid pixels to the frame's 0..1 coordinates.
    pub fn write_flow(&self, queue: &wgpu::Queue, layer: u32, flow: &FlowField) {
        let (width, height) = (flow.width as f32, flow.height as f32);
        let mut bytes = Vec::with_capacity(flow.u.len() * 4);
        for (u, v) in flow.u.iter().zip(&flow.v) {
            bytes.extend_from_slice(&f16::from_f32(u / width).to_le_bytes());
            bytes.extend_from_slice(&f16::from_f32(v / height).to_le_bytes());
        }
        queue.write_texture(
            wgpu::TexelCopyTextureInfo {
                texture: &self.flows,
                mip_level: 0,
                origin: wgpu::Origin3d { x: 0, y: 0, z: layer },
                aspect: wgpu::TextureAspect::All,
            },
            &bytes,
            wgpu::TexelCopyBufferLayout { offset: 0, bytes_per_row: Some(flow.width as u32 * 4), rows_per_image: None },
            wgpu::Extent3d { width: flow.width as u32, height: flow.height as u32, depth_or_array_layers: 1 },
        );
    }
}
