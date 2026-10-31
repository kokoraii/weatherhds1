package music

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStandard(t *testing.T) {
	for _, name := range []string{"", "off", "  OFF "} {
		if _, enabled, err := ParseStandard(name); enabled || err != nil {
			t.Fatalf("ParseStandard(%q) = enabled %v, err %v; want disabled without error", name, enabled, err)
		}
	}
	std, enabled, err := ParseStandard(" EBU-R128 ")
	if err != nil || !enabled || std.TargetLUFS != -23 || std.CeilingDB != -1 {
		t.Fatalf("ParseStandard(ebu-r128) = %+v, %v, %v", std, enabled, err)
	}
	if _, _, err := ParseStandard("ebu128"); err == nil {
		t.Fatal("ParseStandard accepted an unknown standard")
	}
}

func TestChain(t *testing.T) {
	ebu := standards["ebu-r128"] // target -23 LUFS, ceiling -1 dBTP, so -3 dBTP after the Opus margin

	filter, gain, ok := ebu.chain(Measurement{IntegratedLUFS: -30, TruePeakDB: -10})
	if !ok || gain != 7 || filter != "volume=7.00dB" {
		t.Fatalf("quiet track with headroom: %q, %v, %v; want gain only", filter, gain, ok)
	}

	filter, gain, ok = ebu.chain(Measurement{IntegratedLUFS: -30, TruePeakDB: -2})
	if !ok || gain != 7 || !strings.HasPrefix(filter, "volume=7.00dB,aresample=192000,alimiter=limit=0.7079:") {
		t.Fatalf("peaky track: %q, %v, %v; want a limiter at 0.7079 after the gain", filter, gain, ok)
	}

	filter, gain, ok = ebu.chain(Measurement{IntegratedLUFS: -14, TruePeakDB: -0.2})
	if !ok || gain != -9 || filter != "volume=-9.00dB" {
		t.Fatalf("loud track: %q, %v, %v; want it turned down with no limiter", filter, gain, ok)
	}

	if _, gain, _ = ebu.chain(Measurement{IntegratedLUFS: -55, TruePeakDB: -40}); gain != maxBoostDB {
		t.Fatalf("very quiet track got %v dB; want the %v dB cap", gain, maxBoostDB)
	}

	if _, _, ok = ebu.chain(Measurement{IntegratedLUFS: -200, TruePeakDB: -200}); ok {
		t.Fatal("a silent track should not be processed")
	}
}

func TestParseLoudnorm(t *testing.T) {
	report := `[Parsed_loudnorm_0 @ 000001] 
{
	"input_i" : "-14.32",
	"input_tp" : "-0.45",
	"input_lra" : "6.10",
	"input_thresh" : "-24.60",
	"output_i" : "-24.40",
	"normalization_type" : "dynamic",
	"target_offset" : "0.40"
}
`
	m, err := parseLoudnorm([]byte(report))
	if err != nil || m.IntegratedLUFS != -14.32 || m.TruePeakDB != -0.45 {
		t.Fatalf("parseLoudnorm = %+v, %v", m, err)
	}

	silent := strings.NewReplacer("-14.32", "-inf", "-0.45", "-inf").Replace(report)
	if m, err = parseLoudnorm([]byte(silent)); err != nil || m.IntegratedLUFS != -200 || m.TruePeakDB != -200 {
		t.Fatalf("silent report = %+v, %v; want values floored at -200", m, err)
	}

	if _, err = parseLoudnorm([]byte("no report here")); err == nil {
		t.Fatal("parseLoudnorm accepted output with no report")
	}
}

func TestNormalizerCache(t *testing.T) {
	dir := t.TempDir()
	track := filepath.Join(dir, "song.wav")
	if err := os.WriteFile(track, []byte("aaaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(dir, "cache", "loudness.json")
	scans := 0
	newCounting := func() *normalizer {
		n := newNormalizer(standards["ebu-r128"], cachePath, func(string, ...any) {})
		n.scan = func(context.Context, string) (Measurement, error) {
			scans++
			return Measurement{IntegratedLUFS: -18, TruePeakDB: -3}, nil
		}
		return n
	}

	n := newCounting()
	for range 2 {
		if _, err := n.measure(context.Background(), track); err != nil {
			t.Fatal(err)
		}
	}
	if scans != 1 {
		t.Fatalf("scanned %d times for the same file, want 1", scans)
	}

	if _, err := newCounting().measure(context.Background(), track); err != nil || scans != 1 {
		t.Fatalf("a restarted normalizer scanned again (%d scans, err %v); the cache file should serve it", scans, err)
	}

	if err := os.WriteFile(track, []byte("a longer file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newCounting().measure(context.Background(), track); err != nil || scans != 2 {
		t.Fatalf("a changed file was not scanned again (%d scans, err %v)", scans, err)
	}
}

// Runs the real ffmpeg: a quiet tone is measured, filtered with the standard's chain, and measured
// again, which must land on the target. The limiter chain is also run once to prove ffmpeg accepts it.
func TestNormalizeWithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath(ffmpegBinary); err != nil {
		t.Skip("ffmpeg is not in PATH")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "quiet.wav")
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command(ffmpegBinary, append([]string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg %v: %v\n%s", args, err, out)
		}
	}
	run("-f", "lavfi", "-i", "sine=frequency=440:duration=6:sample_rate=48000", "-af", "volume=-8dB,pan=stereo|c0=c0|c1=c0", source)

	std := standards["ebu-r128"]
	before, err := scanLoudness(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	filter, gain, ok := std.chain(before)
	if !ok || gain >= maxBoostDB {
		t.Fatalf("test tone measured %+v: gain %v, ok %v; it should need a normal boost", before, gain, ok)
	}

	processed := filepath.Join(dir, "processed.wav")
	run("-i", source, "-af", filter, processed)
	got, err := scanLoudness(context.Background(), processed)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.IntegratedLUFS-std.TargetLUFS) > 0.5 {
		t.Fatalf("filtered track is %.2f LUFS, want %.0f ±0.5 (filter %q)", got.IntegratedLUFS, std.TargetLUFS, filter)
	}

	limiter, _, _ := std.chain(Measurement{IntegratedLUFS: -30, TruePeakDB: -2})
	run("-i", source, "-af", limiter, "-f", "null", "-")
}
