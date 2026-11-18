package server

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"weatherhds/internal/control"
	"weatherhds/internal/events"
	"weatherhds/internal/music"
)

// videosRoute is where the files in Videos/ are served.
const videosRoute = "/media/videos"

// newChannel builds the realtime channel to the pages (/events) and the dispatcher that commands go
// through. Each page gets its own music session, signalled over its channel, and is told the on-air
// background and today's date as it connects.
func newChannel(version string, logger *slog.Logger, musicService *music.Service, backgrounds *BackgroundManager,
	zone *time.Location, videosDir string) (*events.Hub, *control.Dispatcher) {
	hub := events.NewHub(version, func(format string, args ...any) {
		logInfo(logger, "events", format, args...)
	})
	hub.Extend("music", func(page *events.Page) (func(events.Message), func()) {
		session := musicService.Attach(func(command string, payload any) {
			page.Send(events.NewMessage("music", command, payload))
		})
		return func(message events.Message) { session.Handle(message.Command, message.Args) }, session.Close
	})
	backgroundSync := func(state BackgroundState) events.Message {
		return events.NewMessage(events.TypeHealth, "backgroundSync", state)
	}
	backgrounds.OnChange(func(state BackgroundState) { hub.Broadcast(backgroundSync(state)) })
	hub.OnConnect(func(page *events.Page) {
		page.Send(backgroundSync(backgrounds.State()))
		page.Send(holidaySync(time.Now(), zone))
	})

	dispatcher := control.NewDispatcher(hub, func(format string, args ...any) {
		logInfo(logger, "control", format, args...)
	})
	dispatcher.Handle("background", "setBackground", func(_ context.Context, r control.Request) (control.Outcome, error) {
		if err := backgrounds.Pin(r.Target); err != nil {
			return control.Outcome{}, err
		}
		if r.Target == "auto" {
			return control.Outcome{Message: "backgrounds rotate again"}, nil
		}
		return control.Outcome{Message: "on air: " + r.Target}, nil
	})
	dispatcher.Handle("background", "hideBackground", func(context.Context, control.Request) (control.Outcome, error) {
		backgrounds.Hide()
		return control.Outcome{Message: "background hidden"}, nil
	})
	// playFile names a file in Videos/; the pages get its URL.
	dispatcher.Handle("video", "playFile", func(ctx context.Context, r control.Request) (control.Outcome, error) {
		if _, err := videoFile(videosDir, r.Target); err != nil {
			return control.Outcome{}, err
		}
		r.Target = videosRoute + "/" + filepath.ToSlash(r.Target)
		return dispatcher.ToPages(ctx, r), nil
	})
	return hub, dispatcher
}

// videoFile is the path of a file in Videos/, refusing anything outside it.
func videoFile(videosDir, name string) (string, error) {
	path := filepath.Join(videosDir, filepath.FromSlash(name))
	if !strings.HasPrefix(path, filepath.Clean(videosDir)+string(filepath.Separator)) || !fileExists(path) {
		return "", fmt.Errorf("no video %s in %s", name, videosDir)
	}
	return path, nil
}

// videoFiles lists the playable files in Videos/, for completion.
func videoFiles(videosDir string) []string {
	var files []string
	_ = filepath.WalkDir(videosDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".mp4", ".webm", ".m4v", ".mov", ".m3u8":
			relative, _ := filepath.Rel(videosDir, path)
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	return files
}
