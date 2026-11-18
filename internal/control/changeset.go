package control

import "weatherhds/internal/changeset"

// The change set commands, carried out on the server: stage files, say what the presentations do when
// they go live, then put them all in place at once.
func init() {
	register(
		Spec{
			Type: "changeSet", Command: "stageFile", Local: true,
			Help:   "Stage a file on this machine, or download a URL, to go live at as=public/…, Music/… or Videos/….",
			Target: &Param{Name: "source", Required: true},
			Params: []Param{{Name: "as", Required: true, Help: "where it goes, relative to the server's folder"}},
		},
		Spec{
			Type: "changeSet", Command: "presControl", Local: true,
			Help:   "What a presentation does when the change set goes live (pages reload if none is set).",
			Target: &Param{Name: "presentation", Required: true, Choices: Presentations},
			Params: []Param{{Name: "action", Required: true, Choices: changeset.Actions}},
		},
		Spec{Type: "changeSet", Command: "provisionNow", Local: true, Help: "Put every staged file in place and tell the pages."},
		Spec{Type: "changeSet", Command: "list", Local: true, Help: "Show what's staged."},
		Spec{Type: "changeSet", Command: "discard", Local: true, Help: "Drop everything staged."},
	)
}
