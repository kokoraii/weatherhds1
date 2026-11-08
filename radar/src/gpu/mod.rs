//! Everything tied to one canvas's GPU device.
//!
//! Each canvas gets its own device because a WebGL2 device can't outlive or leave its canvas's context,
//! and the frames of different radars are never shared anyway. Every pass is a plain render pass with no
//! compute, so the same code runs on WebGPU and on WebGL2 (which is what the kiosk Chromium has).

pub mod composite;
pub mod frameset;
pub mod smooth;

use crate::config::Backend;
use composite::Compositor;
use smooth::Smoother;
use std::cell::Cell;
use std::sync::Arc;
use wasm_bindgen::JsValue;
use web_sys::HtmlCanvasElement;

/// Frames are stored and blended as plain unorm, the same space the map's own layers blend in.
pub const FRAME_FORMAT: wgpu::TextureFormat = wgpu::TextureFormat::Rgba8Unorm;

pub struct Gpu {
    pub device: wgpu::Device,
    pub queue: wgpu::Queue,
    pub smoother: Smoother,
    pub compositor: Compositor,
    surface: wgpu::Surface<'static>,
    config: std::cell::RefCell<wgpu::SurfaceConfiguration>,
    pub backend_name: String,
    configured: Cell<bool>,
}

impl Gpu {
    pub async fn new(canvas: HtmlCanvasElement, backend: Backend) -> Result<Self, JsValue> {
        let mut descriptor = wgpu::InstanceDescriptor::new_without_display_handle();
        descriptor.backends = match backend {
            Backend::Auto => wgpu::Backends::BROWSER_WEBGPU | wgpu::Backends::GL,
            Backend::Webgpu => wgpu::Backends::BROWSER_WEBGPU,
            Backend::Webgl => wgpu::Backends::GL,
        };
        let instance = wgpu::util::new_instance_with_webgpu_detection(descriptor).await;

        let surface = instance
            .create_surface(wgpu::SurfaceTarget::Canvas(canvas))
            .map_err(|error| JsValue::from_str(&format!("create_surface: {error}")))?;
        let adapter = instance
            .request_adapter(&wgpu::RequestAdapterOptions {
                power_preference: wgpu::PowerPreference::HighPerformance,
                compatible_surface: Some(&surface),
                ..Default::default()
            })
            .await
            .map_err(|error| JsValue::from_str(&format!("request_adapter: {error}")))?;

        let (device, queue) = adapter
            .request_device(&wgpu::DeviceDescriptor {
                label: Some("radar"),
                required_limits: wgpu::Limits::downlevel_webgl2_defaults().using_resolution(adapter.limits()),
                ..Default::default()
            })
            .await
            .map_err(|error| JsValue::from_str(&format!("request_device: {error}")))?;
        // A bad GPU call logs instead of the default panic, which would take every radar down with it.
        device.on_uncaptured_error(Arc::new(|error: wgpu::Error| {
            web_sys::console::error_1(&format!("[radar] GPU error: {error}").into());
        }));

        let capabilities = surface.get_capabilities(&adapter);
        let format = capabilities
            .formats
            .iter()
            .copied()
            .find(|format| !format.is_srgb())
            .unwrap_or(capabilities.formats[0]);
        // WebGL2 only reports Opaque, but wgpu creates its context with the browser's default of
        // premultiplied alpha, so the transparency still reaches the page.
        let alpha_mode = if capabilities.alpha_modes.contains(&wgpu::CompositeAlphaMode::PreMultiplied) {
            wgpu::CompositeAlphaMode::PreMultiplied
        } else {
            capabilities.alpha_modes[0]
        };

        let mut config = surface
            .get_default_config(&adapter, 1, 1)
            .ok_or_else(|| JsValue::from_str("canvas surface is not supported by this adapter"))?;
        config.format = format;
        config.alpha_mode = alpha_mode;

        let smoother = Smoother::new(&device);
        let compositor = Compositor::new(&device, format);
        let backend_name = format!("{:?}", adapter.get_info().backend);

        Ok(Self {
            device,
            queue,
            smoother,
            compositor,
            surface,
            config: std::cell::RefCell::new(config),
            backend_name,
            configured: Cell::new(false),
        })
    }

    /// Reconfigures the canvas surface when its size changed.
    pub fn resize(&self, width: u32, height: u32) {
        let mut config = self.config.borrow_mut();
        if self.configured.get() && config.width == width && config.height == height {
            return;
        }
        config.width = width;
        config.height = height;
        self.surface.configure(&self.device, &config);
        self.configured.set(true);
    }

    /// The canvas's next texture, or None to skip this frame (the surface is reconfigured if it went stale).
    pub fn acquire(&self) -> Option<wgpu::SurfaceTexture> {
        match self.surface.get_current_texture() {
            wgpu::CurrentSurfaceTexture::Success(texture) | wgpu::CurrentSurfaceTexture::Suboptimal(texture) => {
                Some(texture)
            }
            wgpu::CurrentSurfaceTexture::Outdated | wgpu::CurrentSurfaceTexture::Lost => {
                self.surface.configure(&self.device, &self.config.borrow());
                None
            }
            _ => None,
        }
    }
}

/// Builds a fullscreen-triangle pipeline with one bind group.
pub(crate) fn fullscreen_pipeline(
    device: &wgpu::Device,
    label: &str,
    fragment_source: &str,
    layout: &wgpu::BindGroupLayout,
    target: wgpu::TextureFormat,
    blend: Option<wgpu::BlendState>,
) -> wgpu::RenderPipeline {
    let source = format!("{}\n{}", include_str!("fullscreen.wgsl"), fragment_source);
    let module = device.create_shader_module(wgpu::ShaderModuleDescriptor {
        label: Some(label),
        source: wgpu::ShaderSource::Wgsl(source.into()),
    });
    let pipeline_layout = device.create_pipeline_layout(&wgpu::PipelineLayoutDescriptor {
        label: Some(label),
        bind_group_layouts: &[Some(layout)],
        immediate_size: 0,
    });
    device.create_render_pipeline(&wgpu::RenderPipelineDescriptor {
        label: Some(label),
        layout: Some(&pipeline_layout),
        vertex: wgpu::VertexState {
            module: &module,
            entry_point: Some("vs"),
            compilation_options: Default::default(),
            buffers: &[],
        },
        primitive: wgpu::PrimitiveState::default(),
        depth_stencil: None,
        multisample: wgpu::MultisampleState::default(),
        fragment: Some(wgpu::FragmentState {
            module: &module,
            entry_point: Some("fs"),
            compilation_options: Default::default(),
            targets: &[Some(wgpu::ColorTargetState {
                format: target,
                blend,
                write_mask: wgpu::ColorWrites::ALL,
            })],
        }),
        multiview_mask: None,
        cache: None,
    })
}

/// Runs a fullscreen pass into `target`, cleared to transparent first.
pub(crate) fn fullscreen_pass(
    encoder: &mut wgpu::CommandEncoder,
    target: &wgpu::TextureView,
    pipeline: &wgpu::RenderPipeline,
    bind_group: &wgpu::BindGroup,
) {
    let mut pass = begin_clear_pass(encoder, target);
    pass.set_pipeline(pipeline);
    pass.set_bind_group(0, bind_group, &[]);
    pass.draw(0..3, 0..1);
}

/// Begins a render pass that clears `target` to transparent.
pub(crate) fn begin_clear_pass<'a>(
    encoder: &'a mut wgpu::CommandEncoder,
    target: &'a wgpu::TextureView,
) -> wgpu::RenderPass<'a> {
    encoder.begin_render_pass(&wgpu::RenderPassDescriptor {
        label: None,
        color_attachments: &[Some(wgpu::RenderPassColorAttachment {
            view: target,
            depth_slice: None,
            resolve_target: None,
            ops: wgpu::Operations { load: wgpu::LoadOp::Clear(wgpu::Color::TRANSPARENT), store: wgpu::StoreOp::Store },
        })],
        depth_stencil_attachment: None,
        timestamp_writes: None,
        occlusion_query_set: None,
        multiview_mask: None,
    })
}
