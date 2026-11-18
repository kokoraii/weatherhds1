package music

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// makeTaggedFile writes a 2-second tone with tags, and a cover unless coverCodec is "".
func makeTaggedFile(t *testing.T, path, audioCodec, coverCodec string) {
	t.Helper()
	args := []string{"-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=330:duration=2"}
	if coverCodec != "" {
		cover := filepath.Join(filepath.Dir(path), "cover.png")
		if output, err := exec.Command(ffmpegBinary, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi",
			"-i", "color=red:s=32x32", "-frames:v", "1", cover).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg couldn't make a cover: %v\n%s", err, output)
		}
		args = append(args, "-i", cover, "-map", "0:a", "-map", "1:v", "-c:v", coverCodec, "-disposition:v:0", "attached_pic")
	}
	args = append(args, "-c:a", audioCodec, "-id3v2_version", "3",
		"-metadata", "title=Prairie Song", "-metadata", "artist=Some Artist", "-metadata", "album=Some Album",
		"-metadata", "date=2024-05-01", path)
	if output, err := exec.Command(ffmpegBinary, args...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg couldn't make %s: %v\n%s", filepath.Base(path), err, output)
	}
}

func TestProbeMediaReadsTagsDurationAndCover(t *testing.T) {
	for _, tool := range []string{ffmpegBinary, ffprobeBinary} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s isn't installed", tool)
		}
	}
	root := t.TempDir()
	cases := []struct {
		file, audio, cover, artMime string
	}{
		{"song.mp3", "libmp3lame", "mjpeg", "image/jpeg"},
		{"song.flac", "flac", "png", "image/png"},
		{"song.m4a", "aac", "png", "image/png"},
		{"song.wav", "pcm_s16le", "", ""},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			path := filepath.Join(root, c.file)
			makeTaggedFile(t, path, c.audio, c.cover)
			track, err := probeTrack(root, path)
			if err != nil {
				t.Fatal(err)
			}
			if track.Title != "Prairie Song" || track.Artist != "Some Artist" || track.Album != "Some Album" || track.Year != "2024" {
				t.Fatalf("tags: %+v", track)
			}
			if track.Duration < 1.9 || track.Duration > 2.2 {
				t.Fatalf("duration %f, want about 2", track.Duration)
			}
			if c.cover == "" {
				if track.HasArt {
					t.Fatal("no cover was embedded, but one was found")
				}
				return
			}
			if !track.HasArt || !strings.HasPrefix(track.artMime, c.artMime) {
				t.Fatalf("cover: has %v, type %q, want %s", track.HasArt, track.artMime, c.artMime)
			}
		})
	}
}

func TestYearOfKeepsTheYear(t *testing.T) {
	for input, want := range map[string]string{"2024-05-01": "2024", "2024": "2024", "1999/12": "1999", "Spring 2024": "Spring 2024", "": ""} {
		if got := yearOf(input); got != want {
			t.Errorf("yearOf(%q) = %q, want %q", input, got, want)
		}
	}
}
