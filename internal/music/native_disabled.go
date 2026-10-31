//go:build !ffmpeg || !cgo

package music

func isNativeAudioExtension(ext string) bool {
	return isCommonMediaExtension(ext)
}

func unsupportedLibraryMessage() string {
	return "No supported media files found in ./Music"
}

// Without the libav bindings, every non-Opus track is decoded by the ffmpeg executable.
func (s *Service) playNativeTrack(track Track) error {
	return s.playFFmpegProcess(track, "")
}
