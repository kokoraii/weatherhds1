//go:build !ffmpeg || !cgo

package music

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func isNativeAudioExtension(ext string) bool {
	return isCommonMediaExtension(ext)
}

func unsupportedLibraryMessage() string {
	return "No supported media files found in ./Music"
}

func (s *Service) playNativeTrack(track Track) error {
	cmd := exec.CommandContext(s.ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-i", track.path, "-map", "0:a:0", "-vn", "-c:a", "libopus", "-ar", "48000", "-ac", "2", "-f", "ogg", "pipe:1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create FFmpeg output pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start FFmpeg for %s: %w", track.path, err)
	}

	streamErr := s.playOggOpusReader(track, stdout)
	waitErr := cmd.Wait()
	if s.ctx.Err() != nil {
		return nil
	}
	if waitErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return fmt.Errorf("FFmpeg failed for %s: %s", track.path, message)
		}
		return fmt.Errorf("FFmpeg failed for %s: %w", track.path, waitErr)
	}
	return streamErr
}
