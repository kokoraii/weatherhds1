package control

// The video commands, carried out by each page's video-takeover.js. playFile runs on the server first,
// to check the file and turn it into its URL.
func init() {
	mode := Param{Name: "mode", Help: "squeezeBack (default) or fullscreen", Choices: []string{"squeezeBack", "fullscreen"}}
	audio := Param{Name: "audio", Help: "what the music does meanwhile: mute (default), duck or ignore", Choices: []string{"mute", "duck", "ignore"}}
	presControl := func() []Param {
		params := make([]Param, 0, len(Presentations))
		for _, name := range Presentations {
			params = append(params, Param{Name: name, Help: "what " + name + " does meanwhile", Choices: []string{"pause", "stop", "continue"}})
		}
		return params
	}
	register(
		Spec{
			Type: "video", Command: "playFile", Local: true,
			Help:   "Play a file from Videos/, or a video URL, once, then carry on.",
			Target: &Param{Name: "file", Required: true, Source: "videos"},
			Params: append([]Param{mode, audio}, presControl()...),
		},
		Spec{
			Type: "video", Command: "startStream",
			Help:   "Show a live stream (HLS .m3u8, MP4, WebM URL) until stopStream.",
			Target: &Param{Name: "url", Required: true},
			Params: append([]Param{mode, audio}, presControl()...),
		},
		Spec{Type: "video", Command: "stopStream", Help: "End the video or stream on screen."},
		Spec{
			Type: "video", Command: "presControl",
			Help:   "Change what the presentations do during the video on screen.",
			Params: presControl(),
		},
	)
}
