package control

// The background commands, carried out on the server: every page shows the server's on-air background.
func init() {
	register(
		Spec{
			Type: "background", Command: "setBackground", Local: true,
			Help:   "Put one background on air on every page (auto: back to rotating).",
			Target: &Param{Name: "image", Required: true, Source: "backgrounds"},
		},
		Spec{
			Type: "background", Command: "hideBackground", Local: true,
			Help: "Fade the background out on every page.",
		},
	)
}
