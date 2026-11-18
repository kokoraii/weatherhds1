package music

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	iceGrace          = 5 * time.Second
	sampleQueueLength = 24
	sampleQueueTarget = 4
)

// Sender delivers one music message to a page over its event channel: "state", "track", "answer", "ice"
// or "error", with a JSON-encodable payload. It must not block.
type Sender func(command string, payload any)

// Session is one page's music: its WebRTC audio peer, signalled over the page's event channel.
// The page sends "offer" and "ice"; the server answers with "answer" and "ice", and pushes "state"
// and "track" as playback changes.
type Session struct {
	service *Service
	send    Sender
	samples chan media.Sample

	mu    sync.RWMutex
	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticSample
	once  sync.Once
}

// Attach starts a page's music session and sends it the current state.
func (s *Service) Attach(send Sender) *Session {
	c := &Session{service: s, send: send, samples: make(chan media.Sample, sampleQueueLength)}
	s.register(c)
	go c.samplePump()
	return c
}

// Close ends the session when its page goes.
func (c *Session) Close() {
	c.service.unregister(c)
}

func (c *Session) samplePump() {
	for sample := range c.samples {
		c.mu.RLock()
		track := c.track
		c.mu.RUnlock()
		if track == nil {
			continue
		}
		if err := track.WriteSample(sample); err != nil {
			c.service.logf("music sample write failed: %v", err)
			c.closePeer()
		}
	}
}

// Handle takes one music message from the page.
func (c *Session) Handle(command string, args json.RawMessage) {
	var payload struct {
		SDP       *webrtc.SessionDescription `json:"sdp"`
		Candidate *webrtc.ICECandidateInit   `json:"candidate"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &payload); err != nil {
			c.sendError("malformed music message")
			return
		}
	}
	switch command {
	case "offer":
		if payload.SDP == nil {
			c.sendError("missing offer")
			return
		}
		if err := c.handleOffer(*payload.SDP); err != nil {
			c.sendError(err.Error())
		}
	case "ice":
		if payload.Candidate == nil {
			return
		}
		c.mu.RLock()
		pc := c.pc
		c.mu.RUnlock()
		if pc != nil {
			_ = pc.AddICECandidate(*payload.Candidate)
		}
	default:
		c.sendError("unsupported music command " + command)
	}
}

func (c *Session) sendError(message string) {
	c.send("error", map[string]string{"message": message})
}

func (c *Session) handleOffer(offer webrtc.SessionDescription) error {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   opusClockRate,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=1;sprop-stereo=1",
		},
		"music",
		"weatherhds-music",
	)
	if err != nil {
		_ = pc.Close()
		return err
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		_ = pc.Close()
		return err
	}
	go func() {
		buffer := make([]byte, 1500)
		for {
			if _, _, err := sender.Read(buffer); err != nil {
				return
			}
		}
	}()
	pc.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate == nil {
			return
		}
		c.send("ice", map[string]any{"candidate": candidate.ToJSON()})
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		c.service.logf("webrtc connection state: %s", state.String())
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			c.closePeer()
		case webrtc.PeerConnectionStateDisconnected:
			go func(pc *webrtc.PeerConnection) {
				timer := time.NewTimer(iceGrace)
				defer timer.Stop()
				<-timer.C
				if pc.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
					c.closePeerIfCurrent(pc)
				}
			}(pc)
		}
	})
	if err := pc.SetRemoteDescription(offer); err != nil {
		_ = pc.Close()
		return err
	}
	if err := enableOpusStereo(pc, offer.SDP); err != nil {
		_ = pc.Close()
		return err
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		_ = pc.Close()
		return err
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		_ = pc.Close()
		return err
	}

	c.closePeer()
	c.mu.Lock()
	c.pc = pc
	c.track = track
	c.mu.Unlock()
	c.send("answer", map[string]any{"sdp": pc.LocalDescription()})
	return nil
}

func enableOpusStereo(pc *webrtc.PeerConnection, rawSDP string) error {
	description := &sdp.SessionDescription{}
	if err := description.UnmarshalString(rawSDP); err != nil {
		return fmt.Errorf("parse WebRTC offer SDP: %w", err)
	}

	var opusCodec *webrtc.RTPCodecParameters
	for _, media := range description.MediaDescriptions {
		if media.MediaName.Media != "audio" {
			continue
		}
		for _, format := range media.MediaName.Formats {
			payload, err := strconv.ParseUint(format, 10, 8)
			if err != nil {
				continue
			}
			codec, err := description.GetCodecForPayloadType(uint8(payload))
			if err == nil && strings.EqualFold(codec.Name, "opus") && codec.ClockRate == opusClockRate {
				opusCodec = &webrtc.RTPCodecParameters{
					RTPCodecCapability: webrtc.RTPCodecCapability{
						MimeType:    webrtc.MimeTypeOpus,
						ClockRate:   opusClockRate,
						Channels:    2,
						SDPFmtpLine: forceStereoParameters(codec.Fmtp),
					},
					PayloadType: webrtc.PayloadType(payload),
				}
				break
			}
		}
		break
	}
	if opusCodec == nil {
		return fmt.Errorf("WebRTC offer did not contain an Opus audio codec")
	}
	for _, transceiver := range pc.GetTransceivers() {
		if transceiver.Kind() != webrtc.RTPCodecTypeAudio {
			continue
		}
		if err := transceiver.SetCodecPreferences([]webrtc.RTPCodecParameters{*opusCodec}); err != nil {
			return fmt.Errorf("set Opus stereo codec preference: %w", err)
		}
	}
	return nil
}

func forceStereoParameters(parameters string) string {
	values := make([]string, 0, 6)
	for _, parameter := range strings.Split(parameters, ";") {
		parameter = strings.TrimSpace(parameter)
		if parameter == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(strings.SplitN(parameter, "=", 2)[0]))
		if key == "stereo" || key == "sprop-stereo" {
			continue
		}
		values = append(values, parameter)
	}
	values = append(values, "stereo=1", "sprop-stereo=1")
	return strings.Join(values, ";")
}

func (c *Session) writeSample(sample media.Sample) {
	defer func() {
		_ = recover()
	}()
	select {
	case c.samples <- sample:
		return
	default:
	}
drain:
	for len(c.samples) > sampleQueueTarget {
		select {
		case <-c.samples:
		default:
			break drain
		}
	}
	select {
	case c.samples <- sample:
	default:
	}
}

func (c *Session) closePeer() {
	c.mu.Lock()
	pc := c.pc
	c.pc = nil
	c.track = nil
	c.mu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}
}

func (c *Session) closePeerIfCurrent(pc *webrtc.PeerConnection) {
	c.mu.Lock()
	if c.pc != pc {
		c.mu.Unlock()
		return
	}
	c.pc = nil
	c.track = nil
	c.mu.Unlock()
	_ = pc.Close()
}

func (c *Session) close() {
	c.once.Do(func() {
		c.closePeer()
		close(c.samples)
	})
}
