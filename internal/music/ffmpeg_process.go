package music

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

const ffmpegBinary = "ffmpeg"

// playFFmpegProcess decodes a track with the ffmpeg executable, through the -af filter chain when
// one is given, and streams the Ogg Opus it produces to the clients.
func (s *Service) playFFmpegProcess(track Track, filter string) error {
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", track.path, "-map", "0:a:0", "-vn"}
	if filter != "" {
		args = append(args, "-af", filter)
	}
	args = append(args, "-c:a", "libopus", "-ar", "48000", "-ac", "2", "-f", "ogg", "pipe:1")
	cmd := exec.CommandContext(s.ctx, ffmpegBinary, args...)
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
