package music

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	ffprobeBinary = "ffprobe"
	probeTimeout  = 15 * time.Second
	maxArtBytes   = 16 << 20
)

// ffprobeOutput is the part of `ffprobe -print_format json -show_format -show_streams` used here.
type ffprobeOutput struct {
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		Index       int               `json:"index"`
		CodecType   string            `json:"codec_type"`
		CodecName   string            `json:"codec_name"`
		Tags        map[string]string `json:"tags"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
}

// probeMedia reads a non-Ogg track's tags, duration and cover art: MP3, FLAC, M4A, WMA, WAV and
// anything else ffmpeg plays. ffprobe reads the tags and finds the cover (the stream marked as an
// attached picture, or a still image); ffmpeg copies the cover out as the JPEG or PNG it was stored as.
func probeMedia(root, path string) (Track, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, ffprobeBinary, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return Track{}, fmt.Errorf("ffprobe: %w", err)
	}
	var probe ffprobeOutput
	if err := json.Unmarshal(output, &probe); err != nil {
		return Track{}, fmt.Errorf("ffprobe output: %w", err)
	}

	track := Track{ID: trackID(root, path), path: path, Title: titleFromPath(path)}
	tags := mediaTags(probe)
	if title := tags["title"]; title != "" {
		track.Title = title
	}
	track.Artist = firstNonEmpty(tags["artist"], tags["album_artist"], tags["albumartist"])
	track.Album = tags["album"]
	track.Year = yearOf(firstNonEmpty(tags["date"], tags["year"], tags["originaldate"]))
	if seconds, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil && seconds > 0 {
		track.Duration = seconds
	}

	if cover, ok := coverStream(probe); ok {
		art, err := extractArt(ctx, path, cover)
		if err == nil && len(art) > 0 {
			track.art = art
			track.artMime = http.DetectContentType(art)
			track.HasArt = true
		}
	}
	return track, nil
}

// mediaTags merges the file's tags with its first audio stream's (Ogg Vorbis keeps them there), with
// lowercase keys: containers spell them TITLE or title.
func mediaTags(probe ffprobeOutput) map[string]string {
	tags := map[string]string{}
	add := func(from map[string]string) {
		for key, value := range from {
			key = strings.ToLower(key)
			if value = strings.TrimSpace(value); value != "" && tags[key] == "" {
				tags[key] = value
			}
		}
	}
	add(probe.Format.Tags)
	for _, stream := range probe.Streams {
		if stream.CodecType == "audio" {
			add(stream.Tags)
			break
		}
	}
	return tags
}

// coverStream is the stream holding the cover art: one marked as an attached picture, or a still image.
func coverStream(probe ffprobeOutput) (int, bool) {
	for _, stream := range probe.Streams {
		if stream.CodecType != "video" {
			continue
		}
		switch {
		case stream.Disposition.AttachedPic == 1:
			return stream.Index, true
		case stream.CodecName == "mjpeg" || stream.CodecName == "png" || stream.CodecName == "bmp" || stream.CodecName == "webp":
			return stream.Index, true
		}
	}
	return 0, false
}

func extractArt(ctx context.Context, path string, stream int) ([]byte, error) {
	var art bytes.Buffer
	cmd := exec.CommandContext(ctx, ffmpegBinary, "-v", "error", "-nostdin", "-i", path,
		"-map", "0:"+strconv.Itoa(stream), "-c", "copy", "-frames:v", "1", "-f", "image2pipe", "pipe:1")
	cmd.Stdout = &limitedBuffer{buffer: &art, limit: maxArtBytes}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return art.Bytes(), nil
}

// limitedBuffer stops collecting past limit, so an odd file can't fill memory with "cover art".
type limitedBuffer struct {
	buffer *bytes.Buffer
	limit  int
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if l.buffer.Len()+len(p) > l.limit {
		return 0, fmt.Errorf("cover art over %d bytes", l.limit)
	}
	return l.buffer.Write(p)
}

// yearOf keeps the year of a date tag ("2024-05-01" → "2024"); anything else is kept as written.
func yearOf(date string) string {
	if len(date) > 4 {
		if _, err := strconv.Atoi(date[:4]); err == nil {
			return date[:4]
		}
	}
	return date
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
