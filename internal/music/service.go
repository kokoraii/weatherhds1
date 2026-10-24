package music

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

const (
	opusClockRate     = 48000
	defaultPagePeriod = 20 * time.Millisecond
	maxWSMessageBytes = 64 * 1024
	oggNoGranule      = ^uint64(0)
	maxPlaybackLag    = 120 * time.Millisecond
)

type Logger func(format string, args ...any)

type Track struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Year     string  `json:"year"`
	Duration float64 `json:"duration"`
	HasArt   bool    `json:"has_art"`

	path    string
	art     []byte
	artMime string
}

type State struct {
	State   string  `json:"state"`
	Track   *Track  `json:"track,omitempty"`
	Elapsed float64 `json:"elapsed"`
	Message string  `json:"message,omitempty"`
}

type Service struct {
	musicDir string
	log      Logger
	rng      *rand.Rand

	mu          sync.RWMutex
	library     []Track
	deck        []Track
	current     *Track
	startedAt   time.Time
	state       string
	stateDetail string
	clients     map[*client]struct{}

	ctx    context.Context
	cancel context.CancelFunc
}

func NewService(musicDir string, log Logger) (*Service, error) {
	if log == nil {
		log = func(string, ...any) {}
	}
	if err := os.MkdirAll(musicDir, 0o755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		musicDir: musicDir,
		log:      log,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // playlist shuffle only
		state:    "EMPTY",
		clients:  make(map[*client]struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

func (s *Service) Start(parent context.Context) {
	go func() {
		<-parent.Done()
		s.Close()
	}()
	go s.playbackLoop()
}

func (s *Service) Close() {
	s.cancel()
	s.mu.Lock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
}

func (s *Service) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	s.logf("websocket connect from %s", r.RemoteAddr)
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     sameOrigin,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log("websocket upgrade failed: %v", err)
		return
	}
	c := newClient(s, conn)
	s.register(c)
	go c.writePump()
	go c.samplePump()
	c.readPump()
}

func (s *Service) HandleArt(w http.ResponseWriter, r *http.Request, trackID string) {
	track, ok := s.findTrack(trackID)
	if !ok || len(track.art) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", track.artMime)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(track.art)
}

func (s *Service) HandleState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.State())
}

func (s *Service) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stateLocked()
}

func (s *Service) stateLocked() State {
	var current *Track
	elapsed := 0.0
	if s.current != nil {
		copy := *s.current
		current = &copy
		if !s.startedAt.IsZero() {
			elapsed = time.Since(s.startedAt).Seconds()
			if current.Duration > 0 && elapsed > current.Duration {
				elapsed = current.Duration
			}
		}
	}
	return State{State: s.state, Track: current, Elapsed: elapsed, Message: s.stateDetail}
}

func (s *Service) register(c *client) {
	s.mu.Lock()
	s.clients[c] = struct{}{}
	state := s.stateLocked()
	s.mu.Unlock()
	c.sendJSON(map[string]any{"type": "state", "state": state})
}

func (s *Service) unregister(c *client) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
	c.close()
}

func (s *Service) findTrack(trackID string) (Track, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, track := range s.library {
		if track.ID == trackID {
			return track, true
		}
	}
	if s.current != nil && s.current.ID == trackID {
		return *s.current, true
	}
	return Track{}, false
}

func (s *Service) playbackLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		track, ok := s.nextTrack()
		if !ok {
			s.setEmpty(unsupportedLibraryMessage())
			if !sleepContext(s.ctx, 30*time.Second) {
				return
			}
			continue
		}

		if err := s.playTrack(track); err != nil {
			s.log("track playback failed for %s: %v", track.path, err)
			if !sleepContext(s.ctx, time.Second) {
				return
			}
		}
	}
}

func (s *Service) nextTrack() (Track, bool) {
	s.mu.Lock()
	if len(s.deck) == 0 {
		s.mu.Unlock()
		tracks, unsupported := scanLibrary(s.musicDir)
		s.logf("music scan complete: %d supported, %d unsupported", len(tracks), len(unsupported))
		s.mu.Lock()
		s.library = tracks
		s.deck = append([]Track(nil), tracks...)
		s.rng.Shuffle(len(s.deck), func(i, j int) { s.deck[i], s.deck[j] = s.deck[j], s.deck[i] })
	}
	if len(s.deck) == 0 {
		s.mu.Unlock()
		return Track{}, false
	}
	track := s.deck[0]
	s.deck = s.deck[1:]
	s.mu.Unlock()

	probed, err := probeTrack(s.musicDir, track.path)
	if err != nil {
		s.logf("music metadata probe failed for %s: %v", track.path, err)
		return track, true
	}
	track = probed
	s.mu.Lock()
	for i := range s.library {
		if s.library[i].ID == track.ID {
			s.library[i] = track
			break
		}
	}
	s.mu.Unlock()
	return track, true
}

func (s *Service) setEmpty(message string) {
	s.mu.Lock()
	s.current = nil
	s.startedAt = time.Time{}
	s.state = "EMPTY"
	s.stateDetail = message
	state := s.stateLocked()
	clients := s.clientSnapshotLocked()
	s.mu.Unlock()
	broadcastState(clients, "state", state)
}

func (s *Service) playTrack(track Track) error {
	if !isOggOpusExtension(filepath.Ext(track.path)) {
		return s.playNativeTrack(track)
	}
	return s.playOggOpusTrack(track)
}

func (s *Service) playOggOpusTrack(track Track) error {
	file, err := os.Open(track.path)
	if err != nil {
		return err
	}
	defer file.Close()
	return s.playOggOpusReader(track, file)
}

func (s *Service) playOggOpusReader(track Track, reader io.Reader) error {
	s.mu.Lock()
	s.current = &track
	s.startedAt = time.Now()
	s.state = "PLAYING"
	s.stateDetail = ""
	state := s.stateLocked()
	clients := s.clientSnapshotLocked()
	s.mu.Unlock()
	s.logf("now playing: %s", track.path)
	broadcastState(clients, "track_change", state)

	var lastGranule uint64
	var pending []byte
	seenOpusHead := false
	nextSendAt := time.Now()
	for {
		select {
		case <-s.ctx.Done():
			return nil
		default:
		}
		packets, granule, err := readOggPackets(reader, &pending)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		audioPackets := make([][]byte, 0, len(packets))
		for _, packet := range packets {
			if len(packet) >= 8 {
				switch oggreader.HeaderType(packet[:8]) {
				case oggreader.HeaderOpusID:
					seenOpusHead = true
					continue
				case oggreader.HeaderOpusTags:
					continue
				}
			}
			if !seenOpusHead {
				return fmt.Errorf("missing OpusHead before audio packets")
			}
			audioPackets = append(audioPackets, packet)
		}
		if len(audioPackets) == 0 {
			continue
		}

		duration := defaultPagePeriod
		if granule != oggNoGranule && granule > lastGranule {
			sampleCount := granule - lastGranule
			lastGranule = granule
			perPacket := sampleCount / uint64(len(audioPackets))
			if perPacket > 0 {
				duration = time.Duration(float64(perPacket) / opusClockRate * float64(time.Second))
			}
		}
		if duration <= 0 {
			duration = defaultPagePeriod
		}

		for _, packet := range audioPackets {
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
}

func (s *Service) clientSnapshotLocked() []*client {
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	return clients
}

func (s *Service) writeSample(sample media.Sample) {
	s.mu.RLock()
	clients := s.clientSnapshotLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		c.writeSample(sample)
	}
}

func scanLibrary(root string) ([]Track, []string) {
	tracks := []Track{}
	unsupported := []string{}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !isSupportedExtension(ext) {
			if isCommonMediaExtension(ext) {
				unsupported = append(unsupported, path)
			}
			return nil
		}
		tracks = append(tracks, Track{ID: trackID(root, path), path: path, Title: titleFromPath(path)})
		return nil
	})
	return tracks, unsupported
}

func isSupportedExtension(ext string) bool {
	return isOggOpusExtension(ext) || isNativeAudioExtension(ext)
}

func isOggOpusExtension(ext string) bool {
	ext = strings.ToLower(ext)
	return ext == ".ogg" || ext == ".opus"
}

func isCommonMediaExtension(ext string) bool {
	switch ext {
	case ".mp3", ".m4a", ".m4b", ".m4p", ".aac", ".ac3", ".aif", ".aiff", ".alac", ".amr", ".ape", ".au", ".caf", ".dts", ".flac", ".mka", ".mpc", ".oga", ".ogg", ".opus", ".ra", ".wav", ".wave", ".wma", ".wv",
		".3gp", ".asf", ".avi", ".dv", ".flv", ".m2ts", ".m2v", ".m4v", ".mkv", ".mov", ".mp4", ".mpe", ".mpeg", ".mpg", ".mts", ".mxf", ".nut", ".ogv", ".ts", ".vob", ".webm", ".wmv":
		return true
	default:
		return false
	}
}

func readOggPackets(r io.Reader, pending *[]byte) ([][]byte, uint64, error) {
	header := make([]byte, 27)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, 0, err
	}
	if string(header[:4]) != "OggS" {
		return nil, 0, fmt.Errorf("invalid Ogg page signature")
	}

	granule := binary.LittleEndian.Uint64(header[6:14])
	segmentCount := int(header[26])
	segments := make([]byte, segmentCount)
	if _, err := io.ReadFull(r, segments); err != nil {
		return nil, 0, err
	}

	payloadSize := 0
	for _, segment := range segments {
		payloadSize += int(segment)
	}
	payload := make([]byte, payloadSize)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, 0, err
	}

	packets := make([][]byte, 0, segmentCount)
	pos := 0
	for _, segment := range segments {
		size := int(segment)
		if pos+size > len(payload) {
			return nil, 0, fmt.Errorf("invalid Ogg lacing size")
		}
		*pending = append(*pending, payload[pos:pos+size]...)
		pos += size
		if segment < 255 {
			packet := append([]byte(nil), (*pending)...)
			packets = append(packets, packet)
			*pending = (*pending)[:0]
		}
	}

	return packets, granule, nil
}

func probeTrack(root string, path string) (Track, error) {
	file, err := os.Open(path)
	if err != nil {
		return Track{}, err
	}
	defer file.Close()

	track := Track{ID: trackID(root, path), path: path, Title: titleFromPath(path)}
	var lastGranule uint64
	var pending []byte
	for {
		packets, granule, err := readOggPackets(file, &pending)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Track{}, err
		}
		for _, packet := range packets {
			if len(packet) < 8 {
				continue
			}
			if oggreader.HeaderType(packet[:8]) == oggreader.HeaderOpusTags {
				applyTags(&track, packet)
			}
		}
		if granule != oggNoGranule && granule > lastGranule {
			lastGranule = granule
		}
	}
	if lastGranule > 0 {
		track.Duration = float64(lastGranule) / opusClockRate
	}
	if track.Title == "" {
		track.Title = titleFromPath(path)
	}
	track.HasArt = len(track.art) > 0
	if track.artMime == "" && track.HasArt {
		track.artMime = http.DetectContentType(track.art)
	}
	return track, nil
}

func applyTags(track *Track, payload []byte) {
	tags, err := oggreader.ParseOpusTags(payload)
	if err != nil {
		return
	}
	coverArtMime := ""
	coverArtValue := ""
	for _, comment := range tags.UserComments {
		key := strings.ToUpper(strings.TrimSpace(comment.Comment))
		switch key {
		case "TITLE":
			track.Title = strings.TrimSpace(comment.Value)
		case "ARTIST":
			track.Artist = strings.TrimSpace(comment.Value)
		case "ALBUM":
			track.Album = strings.TrimSpace(comment.Value)
		case "DATE", "YEAR":
			if track.Year == "" {
				track.Year = strings.TrimSpace(comment.Value)
			}
		case "METADATA_BLOCK_PICTURE":
			if len(track.art) == 0 {
				mime, art := parsePictureBlock(comment.Value)
				track.artMime = mime
				track.art = art
			}
		case "COVERARTMIME":
			coverArtMime = strings.TrimSpace(comment.Value)
		case "COVERART":
			coverArtValue = strings.TrimSpace(comment.Value)
		}
	}
	if len(track.art) == 0 && coverArtValue != "" {
		if art, err := decodeBase64Tag(coverArtValue); err == nil && len(art) > 0 {
			track.art = append([]byte(nil), art...)
			track.artMime = coverArtMime
		}
	}
}

func parsePictureBlock(value string) (string, []byte) {
	raw, err := decodeBase64Tag(value)
	if err != nil || len(raw) < 32 {
		return "", nil
	}
	pos := 4
	if len(raw) < pos+4 {
		return "", nil
	}
	mimeLen := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
	pos += 4
	if mimeLen < 0 || len(raw) < pos+mimeLen+20 {
		return "", nil
	}
	mimeType := string(raw[pos : pos+mimeLen])
	pos += mimeLen

	if len(raw) < pos+4 {
		return "", nil
	}
	descriptionLen := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
	pos += 4
	if descriptionLen < 0 || len(raw) < pos+descriptionLen+20 {
		return "", nil
	}
	pos += descriptionLen
	pos += 16

	if len(raw) < pos+4 {
		return "", nil
	}
	dataLen := int(binary.BigEndian.Uint32(raw[pos : pos+4]))
	pos += 4
	if dataLen < 0 || len(raw) < pos+dataLen {
		return "", nil
	}
	art := append([]byte(nil), raw[pos:pos+dataLen]...)
	return mimeType, art
}

func decodeBase64Tag(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if comma := strings.Index(value, ","); strings.HasPrefix(strings.ToLower(value), "data:") && comma >= 0 {
		value = value[comma+1:]
	}
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		default:
			return r
		}
	}, value)
	if raw, err := base64.StdEncoding.DecodeString(cleaned); err == nil {
		return raw, nil
	}
	if raw, err := base64.RawStdEncoding.DecodeString(cleaned); err == nil {
		return raw, nil
	}
	if raw, err := base64.URLEncoding.DecodeString(cleaned); err == nil {
		return raw, nil
	}
	return base64.RawURLEncoding.DecodeString(cleaned)
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func trackID(root string, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	sum := sha1.Sum([]byte(filepath.ToSlash(strings.ToLower(rel))))
	return hex.EncodeToString(sum[:10])
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func broadcastState(clients []*client, messageType string, state State) {
	for _, c := range clients {
		c.sendJSON(map[string]any{"type": messageType, "state": state})
	}
}

func (s *Service) logf(format string, args ...any) {
	if s.log != nil {
		s.log(format, args...)
	}
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	host := r.Host
	return origin == "http://"+host || origin == "https://"+host
}

func newErrorMessage(message string) map[string]any {
	return map[string]any{"type": "error", "message": message}
}

func jsonMarshal(v any) []byte {
	payload, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"type":"error","message":"json encode failed"}`)
	}
	return payload
}

func jsonUnmarshal(data []byte, v any) error {
	if len(data) > maxWSMessageBytes {
		return fmt.Errorf("message too large")
	}
	return json.Unmarshal(data, v)
}
