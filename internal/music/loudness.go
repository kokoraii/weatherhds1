package music

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Standard is a loudness target that music is normalised to.
type Standard struct {
	TargetLUFS float64 // integrated loudness
	CeilingDB  float64 // true-peak ceiling, dBTP
}

var standards = map[string]Standard{
	"ebu-r128":  {TargetLUFS: -23, CeilingDB: -1}, // EBU R128 broadcast
	"atsc-a85":  {TargetLUFS: -24, CeilingDB: -2}, // ATSC A/85, North American broadcast
	"streaming": {TargetLUFS: -14, CeilingDB: -1}, // typical online streaming platforms
}

const (
	// A quiet track is never lifted by more than this, so near-silent files don't turn into noise.
	maxBoostDB = 12
	// Headroom under the ceiling for overshoot from the lossy Opus encode: measured at about 2 dB on a
	// limited hot master. The stream encoder's own limiter is what guarantees the delivered peak.
	opusMarginDB = 2
	// BS.1770 absolute gate: a track whose integrated loudness is at or below this is treated as silent.
	silenceLUFS = -70
)

// ParseStandard resolves a config value. "" and "off" mean no processing (enabled is false).
func ParseStandard(name string) (std Standard, enabled bool, err error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "off" {
		return Standard{}, false, nil
	}
	std, ok := standards[name]
	if !ok {
		return Standard{}, false, fmt.Errorf("unknown music loudness standard %q (use off, ebu-r128, atsc-a85 or streaming)", name)
	}
	return std, true, nil
}

// Measurement is a track's loudness as measured by ffmpeg's loudnorm filter.
type Measurement struct {
	IntegratedLUFS float64 `json:"lufs"`
	TruePeakDB     float64 `json:"tp"`
}

// chain returns the ffmpeg -af filter that brings a measured track to the standard, and its gain in dB.
// It is a constant gain, so the music keeps its dynamics; a true-peak limiter (4x oversampled) follows
// only when the boosted peaks would pass the ceiling. ok is false for a silent track.
func (s Standard) chain(m Measurement) (filter string, gain float64, ok bool) {
	if m.IntegratedLUFS <= silenceLUFS {
		return "", 0, false
	}
	gain = math.Min(s.TargetLUFS-m.IntegratedLUFS, maxBoostDB)
	ceiling := s.CeilingDB - opusMarginDB
	filter = fmt.Sprintf("volume=%.2fdB", gain)
	if m.TruePeakDB+gain > ceiling {
		filter += fmt.Sprintf(",aresample=192000,alimiter=limit=%.4f:attack=5:release=50:level=disabled,aresample=48000",
			math.Pow(10, ceiling/20))
	}
	return filter, gain, true
}

// parseLoudnorm reads the JSON report that loudnorm=print_format=json writes at the end of ffmpeg's stderr.
func parseLoudnorm(stderr []byte) (Measurement, error) {
	start, end := bytes.LastIndexByte(stderr, '{'), bytes.LastIndexByte(stderr, '}')
	if start < 0 || end < start {
		return Measurement{}, errors.New("no loudnorm report in ffmpeg output")
	}
	var report struct {
		InputI  string `json:"input_i"`
		InputTP string `json:"input_tp"`
	}
	if err := json.Unmarshal(stderr[start:end+1], &report); err != nil {
		return Measurement{}, fmt.Errorf("parse loudnorm report: %w", err)
	}
	lufs, err := strconv.ParseFloat(report.InputI, 64)
	if err != nil || math.IsNaN(lufs) {
		return Measurement{}, fmt.Errorf("bad loudnorm input_i %q", report.InputI)
	}
	peak, err := strconv.ParseFloat(report.InputTP, 64)
	if err != nil || math.IsNaN(peak) {
		return Measurement{}, fmt.Errorf("bad loudnorm input_tp %q", report.InputTP)
	}
	// ffmpeg reports silence as -inf, which JSON (and so the cache file) can't hold.
	return Measurement{IntegratedLUFS: math.Max(lufs, -200), TruePeakDB: math.Max(peak, -200)}, nil
}

type cacheEntry struct {
	Size    int64 `json:"size"`
	ModTime int64 `json:"mtime"`
	Measurement
}

// normalizer measures tracks with ffmpeg and remembers the results on disk, keyed by path and
// invalidated when a file's size or modification time changes.
type normalizer struct {
	std       Standard
	cachePath string
	log       Logger
	scan      func(ctx context.Context, path string) (Measurement, error) // scanLoudness; a field so tests can count scans

	mu    sync.Mutex // held across a measurement, so a prefetch and the playback request for a track run ffmpeg once
	cache map[string]cacheEntry
}

func newNormalizer(std Standard, cachePath string, log Logger) *normalizer {
	n := &normalizer{std: std, cachePath: cachePath, log: log, scan: scanLoudness, cache: map[string]cacheEntry{}}
	if payload, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(payload, &n.cache) // an unreadable cache is just measured again
	}
	return n
}

func (n *normalizer) measure(ctx context.Context, path string) (Measurement, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Measurement{}, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if entry, ok := n.cache[path]; ok && entry.Size == info.Size() && entry.ModTime == info.ModTime().UnixNano() {
		return entry.Measurement, nil
	}
	m, err := n.scan(ctx, path)
	if err != nil {
		return Measurement{}, err
	}
	n.cache[path] = cacheEntry{Size: info.Size(), ModTime: info.ModTime().UnixNano(), Measurement: m}
	n.saveLocked()
	return m, nil
}

// prefetch measures a track ahead of its turn so playback doesn't wait for the scan.
func (n *normalizer) prefetch(ctx context.Context, path string) {
	if _, err := n.measure(ctx, path); err != nil && ctx.Err() == nil {
		n.log("loudness scan failed for %s: %v", path, err)
	}
}

func (n *normalizer) saveLocked() {
	payload, err := json.Marshal(n.cache)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(n.cachePath), 0o755)
	}
	if err == nil {
		tmp := n.cachePath + ".tmp"
		if err = os.WriteFile(tmp, payload, 0o644); err == nil {
			err = os.Rename(tmp, n.cachePath)
		}
	}
	if err != nil {
		n.log("loudness cache not saved: %v", err)
	}
}

func scanLoudness(ctx context.Context, path string) (Measurement, error) {
	cmd := exec.CommandContext(ctx, ffmpegBinary, "-nostdin", "-hide_banner", "-nostats",
		"-i", path, "-map", "0:a:0", "-vn", "-af", "loudnorm=print_format=json", "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Measurement{}, fmt.Errorf("ffmpeg loudness scan: %w", err)
	}
	return parseLoudnorm(stderr.Bytes())
}

// Option configures a Service.
type Option func(*Service)

// WithLoudness normalises playback to std, caching measurements in cachePath. It needs the ffmpeg
// executable in PATH (it filters and re-encodes every track, including Ogg Opus files that otherwise
// pass through untouched); without it the music plays unprocessed.
func WithLoudness(std Standard, cachePath string) Option {
	return func(s *Service) {
		if _, err := exec.LookPath(ffmpegBinary); err != nil {
			s.log("loudness normalisation is off: %s was not found in PATH", ffmpegBinary)
			return
		}
		s.normalizer = newNormalizer(std, cachePath, s.log)
		s.log("loudness normalisation on: %.0f LUFS, %.0f dBTP ceiling", std.TargetLUFS, std.CeilingDB)
	}
}

// loudnessFilter is the filter chain for a track, or ok=false to play it unprocessed.
func (s *Service) loudnessFilter(track Track) (filter string, ok bool) {
	if s.normalizer == nil {
		return "", false
	}
	m, err := s.normalizer.measure(s.ctx, track.path)
	if err != nil {
		if s.ctx.Err() == nil {
			s.log("loudness scan failed for %s: %v; playing it unprocessed", track.path, err)
		}
		return "", false
	}
	filter, gain, ok := s.normalizer.std.chain(m)
	if !ok {
		return "", false
	}
	s.log("loudness: %s is %.1f LUFS, %+.1f dB to reach %.0f", filepath.Base(track.path), m.IntegratedLUFS, gain, s.normalizer.std.TargetLUFS)
	return filter, true
}
