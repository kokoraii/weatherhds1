package control

// The presentation commands, carried out by each page's presentation.js.
func init() {
	target := &Param{Name: "presentation", Required: true, Choices: Presentations}
	for _, command := range []struct{ name, help string }{
		{"start", "Start a presentation from the top."},
		{"stop", "Fade a presentation out and stop it."},
		{"kill", "Stop a presentation at once."},
		{"rebuild", "Re-read the config and fresh data, then start over."},
		{"pause", "Freeze a presentation where it is."},
		{"resume", "Carry on from a pause."},
		{"pauseResume", "Pause, or resume if paused."},
		{"letEnd", "Finish the current loop, then stop."},
	} {
		register(Spec{Type: "presentation", Command: command.name, Help: command.help, Target: target})
	}
}
