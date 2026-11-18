package server

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"weatherhds/internal/changeset"
	"weatherhds/internal/control"
)

// downloadTimeout bounds staging a file from a URL.
const downloadTimeout = 30 * time.Minute

// handleChangeSets carries out the changeSet commands. Provisioning tells the pages
// (changeSet provisioned) what each presentation should do, with the files that changed.
func handleChangeSets(dispatcher *control.Dispatcher, stage *changeset.Stage, backgrounds *BackgroundManager) {
	dispatcher.Handle("changeSet", "stageFile", func(_ context.Context, r control.Request) (control.Outcome, error) {
		var file changeset.File
		var err error
		if isURL(r.Target) {
			// Its own deadline: a big video can take longer than the console waits for an answer.
			ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
			defer cancel()
			file, err = stage.StageURL(ctx, r.Target, r.Args["as"], r.From)
		} else {
			file, err = stage.StagePath(r.Target, r.Args["as"], r.From)
		}
		if err != nil {
			return control.Outcome{}, err
		}
		return control.Outcome{Message: fmt.Sprintf("staged %s (%s)", file.Target, formatSize(file.Size))}, nil
	})
	dispatcher.Handle("changeSet", "presControl", func(_ context.Context, r control.Request) (control.Outcome, error) {
		if err := stage.SetPresControl(r.Target, r.Args["action"]); err != nil {
			return control.Outcome{}, err
		}
		return control.Outcome{Message: fmt.Sprintf("on provisioning, %s will %s", r.Target, r.Args["action"])}, nil
	})
	dispatcher.Handle("changeSet", "list", func(context.Context, control.Request) (control.Outcome, error) {
		manifest, err := stage.List()
		if err != nil {
			return control.Outcome{}, err
		}
		return control.Outcome{Message: describeManifest(manifest)}, nil
	})
	dispatcher.Handle("changeSet", "discard", func(context.Context, control.Request) (control.Outcome, error) {
		return control.Outcome{Message: "stage emptied"}, stage.Discard()
	})
	dispatcher.Handle("changeSet", "provisionNow", func(ctx context.Context, r control.Request) (control.Outcome, error) {
		result, err := stage.Provision(time.Now())
		if err != nil {
			return control.Outcome{}, err
		}
		backgrounds.Reindex()

		targets := make([]string, len(result.Files))
		for i, file := range result.Files {
			targets[i] = file.Target
		}
		args := map[string]string{"files": strings.Join(targets, ",")}
		for presentation, action := range result.PresControl {
			args[presentation] = action
		}
		pages := dispatcher.ToPages(ctx, control.Request{Type: "changeSet", Command: "provisioned", Args: args, Feed: r.Feed})

		message := fmt.Sprintf("provisioned %d files", len(result.Files))
		if result.BackupDir != "" {
			message += "; replaced files backed up in " + result.BackupDir
		}
		message += "; pages: " + pages.Message
		if slices.Contains(targets, "public/config.js") {
			message += "; config.js changed: serverConfig settings take effect after a server restart"
		}
		return control.Outcome{Message: message, Result: pages.Result}, nil
	})
}

func describeManifest(manifest changeset.Manifest) string {
	if len(manifest.Files) == 0 && len(manifest.PresControl) == 0 {
		return "nothing staged"
	}
	parts := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		parts = append(parts, fmt.Sprintf("%s (%s, by %s)", file.Target, formatSize(file.Size), file.StagedBy))
	}
	text := fmt.Sprintf("%d staged: %s", len(manifest.Files), strings.Join(parts, ", "))
	if len(manifest.PresControl) > 0 {
		var controls []string
		for presentation, action := range manifest.PresControl {
			controls = append(controls, presentation+"="+action)
		}
		slices.Sort(controls)
		text += "; presControl: " + strings.Join(controls, " ")
	} else {
		text += "; pages will reload"
	}
	return text
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(bytes)/(1<<10))
	}
	return fmt.Sprintf("%d B", bytes)
}
