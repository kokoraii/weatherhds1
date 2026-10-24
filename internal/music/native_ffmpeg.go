//go:build ffmpeg && cgo

package music

/*
#cgo pkg-config: libavformat libavcodec libavutil libswresample
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/audio_fifo.h>
#include <libavutil/channel_layout.h>
#include <libavutil/error.h>
#include <libavutil/frame.h>
#include <libavutil/mem.h>
#include <libavutil/opt.h>
#include <libavutil/samplefmt.h>
#include <libswresample/swresample.h>

typedef struct WHDSFFmpegStream {
	AVFormatContext *format;
	AVCodecContext *decoder;
	AVCodecContext *encoder;
	SwrContext *resampler;
	AVAudioFifo *fifo;
	AVPacket *input_packet;
	AVPacket *output_packet;
	AVFrame *decoded_frame;
	uint8_t **converted;
	int converted_linesize;
	int converted_capacity;
	int audio_stream;
	int demux_eof;
	int decoder_flushed;
	int encoder_flushed;
	int64_t next_pts;
	int64_t duration_us;
} WHDSFFmpegStream;

static void whds_set_error(char *errbuf, int errlen, const char *prefix, int code) {
	if (!errbuf || errlen <= 0) return;
	char av_error[AV_ERROR_MAX_STRING_SIZE] = {0};
	av_strerror(code, av_error, sizeof(av_error));
	snprintf(errbuf, errlen, "%s: %s", prefix, av_error);
}

static int whds_sample_fmt_supported(const enum AVSampleFormat *formats, enum AVSampleFormat target) {
	if (!formats) return 1;
	for (const enum AVSampleFormat *fmt = formats; *fmt != AV_SAMPLE_FMT_NONE; fmt++) {
		if (*fmt == target) return 1;
	}
	return 0;
}

static enum AVSampleFormat whds_pick_sample_fmt(const AVCodec *encoder) {
	if (whds_sample_fmt_supported(encoder->sample_fmts, AV_SAMPLE_FMT_FLT)) return AV_SAMPLE_FMT_FLT;
	if (whds_sample_fmt_supported(encoder->sample_fmts, AV_SAMPLE_FMT_S16)) return AV_SAMPLE_FMT_S16;
	if (encoder->sample_fmts) return encoder->sample_fmts[0];
	return AV_SAMPLE_FMT_FLT;
}

static int whds_ensure_converted(WHDSFFmpegStream *stream, int samples, char *errbuf, int errlen) {
	if (samples <= stream->converted_capacity) return 0;
	if (stream->converted) {
		av_freep(&stream->converted[0]);
		av_freep(&stream->converted);
		stream->converted_capacity = 0;
	}
	int ret = av_samples_alloc_array_and_samples(
		&stream->converted,
		&stream->converted_linesize,
		stream->encoder->ch_layout.nb_channels,
		samples,
		stream->encoder->sample_fmt,
		0
	);
	if (ret < 0) {
		whds_set_error(errbuf, errlen, "allocate converted audio", ret);
		return ret;
	}
	stream->converted_capacity = samples;
	return 0;
}

static int whds_open_stream(const char *path, WHDSFFmpegStream **out, char *errbuf, int errlen) {
	WHDSFFmpegStream *stream = av_mallocz(sizeof(WHDSFFmpegStream));
	if (!stream) {
		snprintf(errbuf, errlen, "allocate ffmpeg stream failed");
		return AVERROR(ENOMEM);
	}
	stream->audio_stream = -1;

	int ret = avformat_open_input(&stream->format, path, NULL, NULL);
	if (ret < 0) {
		whds_set_error(errbuf, errlen, "open input", ret);
		goto fail;
	}
	ret = avformat_find_stream_info(stream->format, NULL);
	if (ret < 0) {
		whds_set_error(errbuf, errlen, "find stream info", ret);
		goto fail;
	}
	for (unsigned int i = 0; i < stream->format->nb_streams; i++) {
		if (stream->format->streams[i]->codecpar->codec_type == AVMEDIA_TYPE_AUDIO) {
			stream->audio_stream = (int)i;
			break;
		}
	}
	if (stream->audio_stream < 0) {
		snprintf(errbuf, errlen, "no audio stream found");
		ret = AVERROR_STREAM_NOT_FOUND;
		goto fail;
	}

	AVStream *audio = stream->format->streams[stream->audio_stream];
	const AVCodec *decoder = avcodec_find_decoder(audio->codecpar->codec_id);
	if (!decoder) {
		snprintf(errbuf, errlen, "audio decoder not found");
		ret = AVERROR_DECODER_NOT_FOUND;
		goto fail;
	}
	stream->decoder = avcodec_alloc_context3(decoder);
	if (!stream->decoder) {
		ret = AVERROR(ENOMEM);
		goto fail;
	}
	ret = avcodec_parameters_to_context(stream->decoder, audio->codecpar);
	if (ret < 0) {
		whds_set_error(errbuf, errlen, "copy decoder parameters", ret);
		goto fail;
	}
	if (stream->decoder->ch_layout.nb_channels == 0) {
		av_channel_layout_default(&stream->decoder->ch_layout, audio->codecpar->ch_layout.nb_channels > 0 ? audio->codecpar->ch_layout.nb_channels : 2);
	}
	ret = avcodec_open2(stream->decoder, decoder, NULL);
	if (ret < 0) {
		whds_set_error(errbuf, errlen, "open decoder", ret);
		goto fail;
	}

	const AVCodec *encoder = avcodec_find_encoder_by_name("libopus");
	if (!encoder) encoder = avcodec_find_encoder(AV_CODEC_ID_OPUS);
	if (!encoder) {
		snprintf(errbuf, errlen, "opus encoder not found in libavcodec");
		ret = AVERROR_ENCODER_NOT_FOUND;
		goto fail;
	}
	stream->encoder = avcodec_alloc_context3(encoder);
	if (!stream->encoder) {
		ret = AVERROR(ENOMEM);
		goto fail;
	}
	stream->encoder->sample_rate = 48000;
	stream->encoder->sample_fmt = whds_pick_sample_fmt(encoder);
	stream->encoder->bit_rate = 128000;
	stream->encoder->time_base = (AVRational){1, 48000};
	stream->encoder->strict_std_compliance = FF_COMPLIANCE_EXPERIMENTAL;
	av_channel_layout_default(&stream->encoder->ch_layout, 2);
	ret = avcodec_open2(stream->encoder, encoder, NULL);
	if (ret < 0) {
		whds_set_error(errbuf, errlen, "open opus encoder", ret);
		goto fail;
	}

	ret = swr_alloc_set_opts2(
		&stream->resampler,
		&stream->encoder->ch_layout,
		stream->encoder->sample_fmt,
		stream->encoder->sample_rate,
		&stream->decoder->ch_layout,
		stream->decoder->sample_fmt,
		stream->decoder->sample_rate,
		0,
		NULL
	);
	if (ret < 0 || !stream->resampler) {
		whds_set_error(errbuf, errlen, "allocate resampler", ret);
		goto fail;
	}
	ret = swr_init(stream->resampler);
	if (ret < 0) {
		whds_set_error(errbuf, errlen, "init resampler", ret);
		goto fail;
	}

	stream->fifo = av_audio_fifo_alloc(stream->encoder->sample_fmt, stream->encoder->ch_layout.nb_channels, stream->encoder->frame_size * 8);
	stream->input_packet = av_packet_alloc();
	stream->output_packet = av_packet_alloc();
	stream->decoded_frame = av_frame_alloc();
	if (!stream->fifo || !stream->input_packet || !stream->output_packet || !stream->decoded_frame) {
		ret = AVERROR(ENOMEM);
		goto fail;
	}

	if (audio->duration != AV_NOPTS_VALUE) {
		stream->duration_us = av_rescale_q(audio->duration, audio->time_base, (AVRational){1, 1000000});
	} else if (stream->format->duration != AV_NOPTS_VALUE) {
		stream->duration_us = stream->format->duration;
	}

	*out = stream;
	return 0;

fail:
	if (stream) {
		if (stream->converted) {
			av_freep(&stream->converted[0]);
			av_freep(&stream->converted);
		}
		if (stream->fifo) av_audio_fifo_free(stream->fifo);
		if (stream->decoded_frame) av_frame_free(&stream->decoded_frame);
		if (stream->input_packet) av_packet_free(&stream->input_packet);
		if (stream->output_packet) av_packet_free(&stream->output_packet);
		if (stream->resampler) swr_free(&stream->resampler);
		if (stream->decoder) avcodec_free_context(&stream->decoder);
		if (stream->encoder) avcodec_free_context(&stream->encoder);
		if (stream->format) avformat_close_input(&stream->format);
		av_free(stream);
	}
	return ret;
}

static int whds_encode_fifo_frame(WHDSFFmpegStream *stream, char *errbuf, int errlen) {
	int frame_samples = stream->encoder->frame_size > 0 ? stream->encoder->frame_size : 960;
	int available = av_audio_fifo_size(stream->fifo);
	if (available <= 0) return 0;
	if (available < frame_samples && !stream->demux_eof) return 0;
	if (available < frame_samples) frame_samples = available;

	AVFrame *frame = av_frame_alloc();
	if (!frame) return AVERROR(ENOMEM);
	frame->nb_samples = frame_samples;
	frame->format = stream->encoder->sample_fmt;
	frame->sample_rate = stream->encoder->sample_rate;
	av_channel_layout_copy(&frame->ch_layout, &stream->encoder->ch_layout);
	int ret = av_frame_get_buffer(frame, 0);
	if (ret < 0) {
		av_frame_free(&frame);
		whds_set_error(errbuf, errlen, "allocate encoder frame", ret);
		return ret;
	}
	ret = av_audio_fifo_read(stream->fifo, (void **)frame->data, frame_samples);
	if (ret < frame_samples) {
		av_frame_free(&frame);
		snprintf(errbuf, errlen, "read audio fifo failed");
		return AVERROR(EIO);
	}
	frame->pts = stream->next_pts;
	stream->next_pts += frame_samples;
	ret = avcodec_send_frame(stream->encoder, frame);
	av_frame_free(&frame);
	if (ret < 0) whds_set_error(errbuf, errlen, "send opus frame", ret);
	return ret;
}

static int whds_decode_more(WHDSFFmpegStream *stream, char *errbuf, int errlen) {
	while (!stream->demux_eof) {
		int ret = av_read_frame(stream->format, stream->input_packet);
		if (ret == AVERROR_EOF) {
			stream->demux_eof = 1;
			ret = avcodec_send_packet(stream->decoder, NULL);
			if (ret < 0) {
				whds_set_error(errbuf, errlen, "flush decoder", ret);
				return ret;
			}
			break;
		}
		if (ret < 0) {
			whds_set_error(errbuf, errlen, "read audio packet", ret);
			return ret;
		}
		if (stream->input_packet->stream_index != stream->audio_stream) {
			av_packet_unref(stream->input_packet);
			continue;
		}
		ret = avcodec_send_packet(stream->decoder, stream->input_packet);
		av_packet_unref(stream->input_packet);
		if (ret < 0 && ret != AVERROR(EAGAIN)) {
			whds_set_error(errbuf, errlen, "send decode packet", ret);
			return ret;
		}
		break;
	}

	for (;;) {
		int ret = avcodec_receive_frame(stream->decoder, stream->decoded_frame);
		if (ret == AVERROR(EAGAIN) || ret == AVERROR_EOF) return 0;
		if (ret < 0) {
			whds_set_error(errbuf, errlen, "receive decoded frame", ret);
			return ret;
		}
		int max_samples = swr_get_out_samples(stream->resampler, stream->decoded_frame->nb_samples);
		if (max_samples < 0) {
			av_frame_unref(stream->decoded_frame);
			whds_set_error(errbuf, errlen, "calculate resampled samples", max_samples);
			return max_samples;
		}
		ret = whds_ensure_converted(stream, max_samples, errbuf, errlen);
		if (ret < 0) {
			av_frame_unref(stream->decoded_frame);
			return ret;
		}
		int converted_samples = swr_convert(
			stream->resampler,
			stream->converted,
			max_samples,
			(const uint8_t **)stream->decoded_frame->extended_data,
			stream->decoded_frame->nb_samples
		);
		av_frame_unref(stream->decoded_frame);
		if (converted_samples < 0) {
			whds_set_error(errbuf, errlen, "resample audio", converted_samples);
			return converted_samples;
		}
		int fifo_size = av_audio_fifo_size(stream->fifo);
		ret = av_audio_fifo_realloc(stream->fifo, fifo_size + converted_samples);
		if (ret < 0) {
			whds_set_error(errbuf, errlen, "grow audio fifo", ret);
			return ret;
		}
		ret = av_audio_fifo_write(stream->fifo, (void **)stream->converted, converted_samples);
		if (ret < converted_samples) {
			snprintf(errbuf, errlen, "write audio fifo failed");
			return AVERROR(EIO);
		}
		if (av_audio_fifo_size(stream->fifo) >= (stream->encoder->frame_size > 0 ? stream->encoder->frame_size : 960)) {
			return 0;
		}
	}
}

static int whds_next_packet(WHDSFFmpegStream *stream, uint8_t **data, int *size, int64_t *duration_samples, char *errbuf, int errlen) {
	*data = NULL;
	*size = 0;
	*duration_samples = 0;
	for (;;) {
		av_packet_unref(stream->output_packet);
		int ret = avcodec_receive_packet(stream->encoder, stream->output_packet);
		if (ret == 0) {
			*data = stream->output_packet->data;
			*size = stream->output_packet->size;
			*duration_samples = stream->output_packet->duration > 0 ? stream->output_packet->duration : (stream->encoder->frame_size > 0 ? stream->encoder->frame_size : 960);
			return 1;
		}
		if (ret < 0 && ret != AVERROR(EAGAIN) && ret != AVERROR_EOF) {
			whds_set_error(errbuf, errlen, "receive opus packet", ret);
			return ret;
		}
		if (stream->encoder_flushed) return 0;

		int frame_samples = stream->encoder->frame_size > 0 ? stream->encoder->frame_size : 960;
		if (av_audio_fifo_size(stream->fifo) >= frame_samples || (stream->demux_eof && av_audio_fifo_size(stream->fifo) > 0)) {
			ret = whds_encode_fifo_frame(stream, errbuf, errlen);
			if (ret < 0) return ret;
			continue;
		}
		if (stream->demux_eof) {
			ret = avcodec_send_frame(stream->encoder, NULL);
			if (ret < 0 && ret != AVERROR_EOF) {
				whds_set_error(errbuf, errlen, "flush opus encoder", ret);
				return ret;
			}
			stream->encoder_flushed = 1;
			continue;
		}
		ret = whds_decode_more(stream, errbuf, errlen);
		if (ret < 0) return ret;
	}
}

static int64_t whds_duration_us(WHDSFFmpegStream *stream) {
	return stream->duration_us;
}

static void whds_close_stream(WHDSFFmpegStream *stream) {
	if (!stream) return;
	if (stream->converted) {
		av_freep(&stream->converted[0]);
		av_freep(&stream->converted);
	}
	if (stream->fifo) av_audio_fifo_free(stream->fifo);
	if (stream->decoded_frame) av_frame_free(&stream->decoded_frame);
	if (stream->input_packet) av_packet_free(&stream->input_packet);
	if (stream->output_packet) av_packet_free(&stream->output_packet);
	if (stream->resampler) swr_free(&stream->resampler);
	if (stream->decoder) avcodec_free_context(&stream->decoder);
	if (stream->encoder) avcodec_free_context(&stream->encoder);
	if (stream->format) avformat_close_input(&stream->format);
	av_free(stream);
}
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/pion/webrtc/v4/pkg/media"
)

func isNativeAudioExtension(ext string) bool {
	return isCommonMediaExtension(ext)
}

func unsupportedLibraryMessage() string {
	return "No supported audio files found in ./Music"
}

func (s *Service) playNativeTrack(track Track) error {
	cPath := C.CString(track.path)
	defer C.free(unsafe.Pointer(cPath))
	errBuf := (*C.char)(C.calloc(1, 1024))
	defer C.free(unsafe.Pointer(errBuf))

	var stream *C.WHDSFFmpegStream
	if ret := C.whds_open_stream(cPath, &stream, errBuf, 1024); ret < 0 {
		return fmt.Errorf("ffmpeg open failed for %s: %s", track.path, C.GoString(errBuf))
	}
	defer C.whds_close_stream(stream)

	if track.Duration <= 0 {
		if durationUS := C.whds_duration_us(stream); durationUS > 0 {
			track.Duration = float64(durationUS) / 1_000_000
		}
	}

	s.mu.Lock()
	s.current = &track
	s.startedAt = time.Now()
	s.state = "PLAYING"
	s.stateDetail = ""
	state := s.stateLocked()
	clients := s.clientSnapshotLocked()
	s.mu.Unlock()
	s.logf("now playing with native ffmpeg: %s", track.path)
	broadcastState(clients, "track_change", state)

	nextSendAt := time.Now()
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
		}
		var data *C.uint8_t
		var size C.int
		var durationSamples C.int64_t
		ret := C.whds_next_packet(stream, &data, &size, &durationSamples, errBuf, 1024)
		if ret == 0 {
			return nil
		}
		if ret < 0 {
			return fmt.Errorf("ffmpeg decode/encode failed for %s: %s", track.path, C.GoString(errBuf))
		}
		if data == nil || size <= 0 {
			continue
		}
		packet := C.GoBytes(unsafe.Pointer(data), size)
		duration := defaultPagePeriod
		if durationSamples > 0 {
			duration = time.Duration(float64(durationSamples) / opusClockRate * float64(time.Second))
		}
		s.writeSample(media.Sample{Data: packet, Duration: duration})
		nextSendAt = nextSendAt.Add(duration)
		delay := time.Until(nextSendAt)
		if delay < -maxPlaybackLag {
			nextSendAt = time.Now()
			continue
		}
		if delay <= 0 {
			continue
		}
		if !sleepContext(s.ctx, delay) {
			return nil
		}
	}
}
