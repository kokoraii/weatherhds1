package music

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media/oggwriter"
)

func TestNewServiceCreatesMissingMusicDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Music")
	service, err := NewService(dir, nil)
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}
	service.Close()

	tracks, unsupported := scanLibrary(dir)
	if len(tracks) != 0 {
		t.Fatalf("expected no tracks in new music dir, got %d", len(tracks))
	}
	if len(unsupported) != 0 {
		t.Fatalf("expected no unsupported files in new music dir, got %d", len(unsupported))
	}
}

func TestScanLibraryFindsMediaAndSkipsOtherFiles(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdirAll failed: %v", err)
	}

	writeTinyOgg(t, filepath.Join(nested, "sample.ogg"))
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), []byte("played through ffmpeg"), 0o644); err != nil {
		t.Fatalf("write mp3 failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("not media"), 0o644); err != nil {
		t.Fatalf("write txt failed: %v", err)
	}

	tracks, unsupported := scanLibrary(root)
	var ogg *Track
	for i := range tracks {
		if strings.HasSuffix(tracks[i].path, "sample.ogg") {
			ogg = &tracks[i]
		}
	}
	if len(tracks) != 2 || ogg == nil {
		t.Fatalf("expected the ogg and the mp3 (ffmpeg plays it), got %d tracks", len(tracks))
	}
	if ogg.Title != "sample" {
		t.Fatalf("expected filename title fallback, got %q", ogg.Title)
	}
	if len(unsupported) != 0 {
		t.Fatalf("other files are skipped, not reported: %#v", unsupported)
	}

	probed, err := probeTrack(root, ogg.path)
	if err != nil {
		t.Fatalf("probeTrack failed: %v", err)
	}
	if probed.Duration <= 0 {
		t.Fatalf("expected parsed duration, got %f", probed.Duration)
	}
}

func TestReadOggPacketsSplitsOpusPackets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.ogg")
	writeTinyOgg(t, path)

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open tiny ogg failed: %v", err)
	}
	defer file.Close()

	var pending []byte
	var packets [][]byte
	for len(packets) < 3 {
		pagePackets, _, err := readOggPackets(file, &pending)
		if err != nil {
			t.Fatalf("readOggPackets failed: %v", err)
		}
		packets = append(packets, pagePackets...)
	}
	if string(packets[0][:8]) != "OpusHead" {
		t.Fatalf("first packet = %q, want OpusHead", string(packets[0][:8]))
	}
	if string(packets[1][:8]) != "OpusTags" {
		t.Fatalf("second packet = %q, want OpusTags", string(packets[1][:8]))
	}
	if len(packets[2]) == 0 {
		t.Fatal("expected at least one audio packet")
	}
}

func TestApplyTagsReadsCoverArtTags(t *testing.T) {
	payload := opusTagsPayload(map[string]string{
		"TITLE":        "Song",
		"COVERARTMIME": "image/png",
		"COVERART":     base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 'G'}),
	})
	track := Track{}
	applyTags(&track, payload)

	if track.Title != "Song" {
		t.Fatalf("expected title tag, got %q", track.Title)
	}
	if track.artMime != "image/png" {
		t.Fatalf("expected image/png art mime, got %q", track.artMime)
	}
	if string(track.art) != string([]byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("unexpected art bytes: %#v", track.art)
	}
}

func TestApplyTagsReadsMetadataBlockPicture(t *testing.T) {
	art := []byte{0xff, 0xd8, 0xff, 0xdb}
	payload := opusTagsPayload(map[string]string{
		"METADATA_BLOCK_PICTURE": metadataBlockPicture("image/jpeg", "cover", art),
	})
	track := Track{}
	applyTags(&track, payload)

	if track.artMime != "image/jpeg" {
		t.Fatalf("expected image/jpeg art mime, got %q", track.artMime)
	}
	if string(track.art) != string(art) {
		t.Fatalf("unexpected art bytes: %#v", track.art)
	}
}

func TestNextTrackAvoidsRepeatsBeforeDeckExhaustion(t *testing.T) {
	service := &Service{
		musicDir: t.TempDir(),
		rng:      rand.New(rand.NewSource(1)),
		deck:     []Track{{ID: "a"}, {ID: "b"}, {ID: "c"}},
	}

	seen := map[string]bool{}
	for range 3 {
		track, ok := service.nextTrack()
		if !ok {
			t.Fatal("expected deck track")
		}
		if seen[track.ID] {
			t.Fatalf("track repeated before deck exhausted: %s", track.ID)
		}
		seen[track.ID] = true
	}
	if _, ok := service.nextTrack(); ok {
		t.Fatal("expected no track after manual deck exhaustion and empty music dir")
	}
}

// recordingSender collects what a session sends, for tests.
type recordingSender struct {
	commands []string
	payloads []any
}

func (r *recordingSender) send(command string, payload any) {
	r.commands = append(r.commands, command)
	r.payloads = append(r.payloads, payload)
}

func TestUnsupportedMusicCommandReturnsError(t *testing.T) {
	sent := &recordingSender{}
	session := &Session{service: &Service{}, send: sent.send}
	session.Handle("pause", nil)
	if len(sent.commands) != 1 || sent.commands[0] != "error" {
		t.Fatalf("expected one error, got %v", sent.commands)
	}
}

func TestHandleOfferCreatesWebRTCAnswer(t *testing.T) {
	peer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection failed: %v", err)
	}
	defer func() {
		_ = peer.Close()
	}()

	if _, err := peer.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatalf("AddTransceiverFromKind failed: %v", err)
	}
	offer, err := peer.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer failed: %v", err)
	}
	if err := peer.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription failed: %v", err)
	}

	sent := &recordingSender{}
	session := &Session{service: &Service{}, send: sent.send}
	defer session.closePeer()
	offerArgs, err := json.Marshal(map[string]any{"sdp": peer.LocalDescription()})
	if err != nil {
		t.Fatalf("marshal offer: %v", err)
	}
	session.Handle("offer", offerArgs)

	answerIndex := -1
	for i, command := range sent.commands {
		if command == "answer" {
			answerIndex = i
		}
	}
	if answerIndex < 0 {
		t.Fatalf("no answer sent, got %v", sent.commands)
	}
	// The payload is what the page receives: round-trip it through JSON like the channel does.
	encoded, err := json.Marshal(sent.payloads[answerIndex])
	if err != nil {
		t.Fatalf("marshal answer: %v", err)
	}
	var response struct {
		SDP *webrtc.SessionDescription `json:"sdp"`
	}
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatalf("answer payload was not JSON: %v", err)
	}
	if response.SDP == nil {
		t.Fatalf("answer carried no SDP: %s", encoded)
	}
	if response.SDP.Type != webrtc.SDPTypeAnswer {
		t.Fatalf("expected answer SDP, got %s", response.SDP.Type)
	}
	if !strings.Contains(response.SDP.SDP, "stereo=1") {
		t.Fatalf("answer SDP did not negotiate Opus stereo:\n%s", response.SDP.SDP)
	}
	if err := peer.SetRemoteDescription(*response.SDP); err != nil {
		t.Fatalf("client-side SetRemoteDescription failed: %v", err)
	}
}

func TestTrackIDStableAndRelative(t *testing.T) {
	root := filepath.Join("D:", "Music")
	first := trackID(root, filepath.Join(root, "Album", "Song.ogg"))
	second := trackID(root, filepath.Join(root, "album", "song.ogg"))
	otherRoot := trackID(filepath.Join("D:", "Other"), filepath.Join(root, "Album", "Song.ogg"))

	if first != second {
		t.Fatal("track ID should be case-insensitive for the relative path")
	}
	if first == otherRoot {
		t.Fatal("track ID should be rooted to the configured music directory")
	}
}

func opusTagsPayload(comments map[string]string) []byte {
	payload := []byte("OpusTags")
	vendor := []byte("test")
	payload = binary.LittleEndian.AppendUint32(payload, uint32(len(vendor)))
	payload = append(payload, vendor...)
	payload = binary.LittleEndian.AppendUint32(payload, uint32(len(comments)))
	for key, value := range comments {
		comment := []byte(key + "=" + value)
		payload = binary.LittleEndian.AppendUint32(payload, uint32(len(comment)))
		payload = append(payload, comment...)
	}
	return payload
}

func metadataBlockPicture(mimeType string, description string, art []byte) string {
	raw := []byte{}
	raw = binary.BigEndian.AppendUint32(raw, 3)
	raw = binary.BigEndian.AppendUint32(raw, uint32(len(mimeType)))
	raw = append(raw, []byte(mimeType)...)
	raw = binary.BigEndian.AppendUint32(raw, uint32(len(description)))
	raw = append(raw, []byte(description)...)
	raw = binary.BigEndian.AppendUint32(raw, 0)
	raw = binary.BigEndian.AppendUint32(raw, 0)
	raw = binary.BigEndian.AppendUint32(raw, 0)
	raw = binary.BigEndian.AppendUint32(raw, 0)
	raw = binary.BigEndian.AppendUint32(raw, uint32(len(art)))
	raw = append(raw, art...)
	return base64.StdEncoding.EncodeToString(raw)
}

func writeTinyOgg(t *testing.T, path string) {
	t.Helper()
	writer, err := oggwriter.New(path, opusClockRate, 2)
	if err != nil {
		t.Fatalf("oggwriter.New failed: %v", err)
	}
	for i := range 4 {
		packet := &rtp.Packet{
			Header: rtp.Header{Timestamp: uint32(960 * (i + 1))},
			Payload: []byte{
				0xf8, 0xff, 0xfe,
			},
		}
		if err := writer.WriteRTP(packet); err != nil {
			t.Fatalf("WriteRTP failed: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("ogg writer close failed: %v", err)
	}
}
