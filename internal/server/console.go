package server

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"weatherhds/internal/console"
	"weatherhds/internal/control"
	"weatherhds/internal/events"
	"weatherhds/internal/music"
)

// startConsole opens the HDS Server console in this terminal (when there is one) and over SSH.
// stop is what Ctrl+C in this terminal does.
func startConsole(ctx context.Context, stop func(), cfg AppConfig, paths RuntimePaths, versionID string, started time.Time,
	logger *slog.Logger, broker *console.Broker, dispatcher *control.Dispatcher, musicService *music.Service,
	backgrounds *BackgroundManager, videosDir string) {
	hub := dispatcher.Hub()
	sshDir := "/etc/ssh"
	if runtime.GOOS == "windows" {
		sshDir = filepath.Join(os.Getenv("ProgramData"), "ssh")
	}
	console.Start(ctx, console.Options{
		Dispatcher: dispatcher,
		Broker:     broker,
		Sources:    consoleSources(hub, backgrounds, videosDir),
		Status: func() console.Status {
			status := console.Status{Version: versionID, Uptime: time.Since(started), Feeds: feedNames(hub)}
			if state := musicService.State(); state.Track != nil {
				status.Track = state.Track.Title
				if state.Track.Artist != "" {
					status.Track += " – " + state.Track.Artist
				}
			}
			return status
		},
		Stop: stop,
		Log: func(format string, args ...any) {
			logInfo(logger, "console", format, args...)
		},
		SSHPort:        cfg.SSHPort,
		SSHDConfig:     filepath.Join(sshDir, "sshd_config"),
		SystemHostKeys: []string{filepath.Join(sshDir, "ssh_host_ed25519_key")},
		HostKeyPath:    filepath.Join(paths.WorkspaceDir, "persistentCache", "ssh", "host_ed25519_key"),
	})
}

// consoleSources gives the console's completion the live values commands can take.
func consoleSources(hub *events.Hub, backgrounds *BackgroundManager, videosDir string) console.Sources {
	return func(name string) []string {
		switch name {
		case "feeds":
			return feedNames(hub)
		case "backgrounds":
			return append([]string{"auto"}, backgrounds.Images()...)
		case "videos":
			return videoFiles(videosDir)
		}
		return nil
	}
}

func feedNames(hub *events.Hub) []string {
	var names []string
	for _, page := range hub.Pages() {
		feed := page.Feed
		if feed == "" {
			feed = "default"
		}
		if !slices.Contains(names, feed) {
			names = append(names, feed)
		}
	}
	slices.Sort(names)
	return names
}
