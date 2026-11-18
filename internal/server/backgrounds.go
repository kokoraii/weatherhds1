package server

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// backgroundRotation is how often the on-air background changes. Every feed shows the same one: each
// change is sent to the pages (health backgroundSync).
const backgroundRotation = time.Hour

// Downloaded backgrounds (setBackground with a web address) are kept in persistentCache/backgrounds and
// served from here.
const downloadedBackgroundsRoute = "/persistentCache/backgrounds"

// BackgroundState is the on-air background, as the pages are told it.
type BackgroundState struct {
	Image  string `json:"image"`  // a served path, e.g. /images/bg_images/..., or "" for none
	Hidden bool   `json:"hidden"` // hideBackground: the wallpaper fades out
}

// BackgroundManager picks the on-air background: a random one for the season and weather, rotated
// hourly, unless one is pinned with setBackground.
type BackgroundManager struct {
	mu           sync.RWMutex
	publicDir    string
	downloadDir  string // where downloadedBackgroundsRoute is served from
	index        map[string]map[string][]string
	selected     string
	selectedCond string
	initialized  string
	tickerStop   chan struct{}
	pinned       string // setBackground; "" rotates
	hidden       bool
	onChange     func(BackgroundState)
}

func NewBackgroundManager(publicDir, workspaceDir string) *BackgroundManager {
	return &BackgroundManager{
		publicDir:    publicDir,
		downloadDir:  filepath.Join(workspaceDir, "persistentCache", "backgrounds"),
		index:        buildBackgroundIndex(publicDir),
		selectedCond: "wxgood",
	}
}

// OnChange sets what happens whenever the on-air background changes. Set it before Init.
func (b *BackgroundManager) OnChange(notify func(BackgroundState)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onChange = notify
}

func (b *BackgroundManager) Init(wxCond string) bool {
	if wxCond == "" {
		wxCond = "wxgood"
	}
	b.mu.Lock()
	if b.initialized == wxCond {
		b.mu.Unlock()
		return true
	}
	picked := b.pickExistingBackgroundLocked(wxCond)
	if picked == "" {
		b.mu.Unlock()
		return false
	}
	b.selected = picked
	b.selectedCond = wxCond
	b.initialized = wxCond
	if b.tickerStop != nil {
		close(b.tickerStop)
	}
	stop := make(chan struct{})
	b.tickerStop = stop
	go b.rotate(stop, wxCond)
	b.mu.Unlock()
	b.changed()
	return true
}

func (b *BackgroundManager) rotate(stop <-chan struct{}, wxCond string) {
	ticker := time.NewTicker(backgroundRotation)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			b.mu.Lock()
			picked := b.pickExistingBackgroundLocked(wxCond)
			if picked != "" {
				b.selected = picked
			}
			pinned := b.pinned != ""
			b.mu.Unlock()
			if !pinned {
				b.changed()
			}
		}
	}
}

// State is the on-air background.
func (b *BackgroundManager) State() BackgroundState {
	image := b.CurrentImage()
	b.mu.RLock()
	defer b.mu.RUnlock()
	return BackgroundState{Image: image, Hidden: b.hidden}
}

// Pin puts one image on air until another is pinned, or "auto" returns to rotation. The image is a path
// relative to public/, e.g. images/bg_images/winter/wxgood/lake.avif, or a web address, which is
// downloaded first and shown from the copy. Pinning also shows a hidden background.
func (b *BackgroundManager) Pin(ctx context.Context, image string) error {
	if isURL(image) {
		name, err := downloadImage(ctx, image, b.downloadDir)
		if err != nil {
			return err
		}
		b.mu.Lock()
		b.pinned = downloadedBackgroundsRoute + "/" + name
	} else {
		b.mu.Lock()
		if err := b.pinLocal(image); err != nil {
			b.mu.Unlock()
			return err
		}
	}
	b.hidden = false
	b.mu.Unlock()
	b.changed()
	return nil
}

func (b *BackgroundManager) pinLocal(image string) error {
	if image == "auto" {
		b.pinned = ""
		return nil
	}
	image = "/" + strings.TrimPrefix(filepath.ToSlash(image), "/")
	absolute := filepath.Join(b.publicDir, filepath.FromSlash(strings.TrimPrefix(image, "/")))
	if !isPathInPublicDir(b.publicDir, absolute) || !fileExists(absolute) || !isBackgroundImage(absolute) {
		return fmt.Errorf("no background image at %s", image)
	}
	b.pinned = image
	return nil
}

// File is where a served background path (CurrentImage) lives on disk: a downloaded copy, or a file
// under public/. ok is false for a path outside both.
func (b *BackgroundManager) File(served string) (path string, ok bool) {
	if name, downloaded := strings.CutPrefix(served, downloadedBackgroundsRoute+"/"); downloaded {
		path = filepath.Join(b.downloadDir, filepath.FromSlash(name))
		return path, isPathInPublicDir(b.downloadDir, path)
	}
	path = filepath.Join(b.publicDir, filepath.FromSlash(strings.TrimPrefix(served, "/")))
	return path, isPathInPublicDir(b.publicDir, path)
}

// Hide fades the background out on every page until one is pinned again (Pin, including "auto").
func (b *BackgroundManager) Hide() {
	b.mu.Lock()
	b.hidden = true
	b.mu.Unlock()
	b.changed()
}

// Reindex rescans the background folders, after a change set adds or replaces images.
func (b *BackgroundManager) Reindex() {
	index := buildBackgroundIndex(b.publicDir)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.index = index
}

// Images lists every background, relative to public/, for completion.
func (b *BackgroundManager) Images() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var images []string
	for _, conditions := range b.index {
		for _, list := range conditions {
			for _, image := range list {
				images = append(images, strings.TrimPrefix(image, "/"))
			}
		}
	}
	slices.Sort(images)
	return images
}

func (b *BackgroundManager) changed() {
	b.mu.RLock()
	notify := b.onChange
	b.mu.RUnlock()
	if notify != nil {
		notify(b.State())
	}
}

func (b *BackgroundManager) CurrentImage() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pinned != "" {
		return b.pinned
	}
	if b.selected == "" {
		return ""
	}
	absolute := filepath.Join(b.publicDir, filepath.FromSlash(strings.TrimPrefix(b.selected, "/")))
	if !isPathInPublicDir(b.publicDir, absolute) || !fileExists(absolute) {
		fallback := b.pickExistingBackgroundLocked(b.selectedCond)
		if fallback == "" {
			b.selected = ""
			return ""
		}
		b.selected = fallback
	}
	return b.selected
}

func (b *BackgroundManager) InitializedCondition() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.initialized
}

func (b *BackgroundManager) pickExistingBackgroundLocked(wxCond string) string {
	season := CurrentSeason(time.Now())
	list := b.index[season][normalizeCondition(wxCond)]
	if len(list) == 0 {
		return ""
	}
	perm := rand.Perm(len(list))
	for _, idx := range perm {
		candidate := list[idx]
		absolute := filepath.Join(b.publicDir, filepath.FromSlash(strings.TrimPrefix(candidate, "/")))
		if isPathInPublicDir(b.publicDir, absolute) && fileExists(absolute) {
			return candidate
		}
	}
	return ""
}

func buildBackgroundIndex(publicDir string) map[string]map[string][]string {
	root := filepath.Join(publicDir, "images", "bg_images")
	index := map[string]map[string][]string{}
	seasonEntries, err := os.ReadDir(root)
	if err != nil {
		return index
	}
	for _, seasonEntry := range seasonEntries {
		if !seasonEntry.IsDir() {
			continue
		}
		season := seasonEntry.Name()
		seasonPath := filepath.Join(root, season)
		conditionEntries, err := os.ReadDir(seasonPath)
		if err != nil {
			continue
		}
		if _, ok := index[season]; !ok {
			index[season] = map[string][]string{}
		}
		for _, conditionEntry := range conditionEntries {
			if !conditionEntry.IsDir() {
				continue
			}
			conditionKey := normalizeCondition(conditionEntry.Name())
			conditionPath := filepath.Join(seasonPath, conditionEntry.Name())
			files, err := os.ReadDir(conditionPath)
			if err != nil {
				continue
			}
			for _, file := range files {
				if file.IsDir() {
					continue
				}
				if !isBackgroundImage(file.Name()) {
					continue
				}
				absolute := filepath.Join(conditionPath, file.Name())
				relative, err := filepath.Rel(publicDir, absolute)
				if err != nil {
					continue
				}
				relative = filepath.ToSlash(relative)
				index[season][conditionKey] = append(index[season][conditionKey], "/"+relative)
			}
		}
	}
	return index
}

func isBackgroundImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".avif", ".webp", ".png", ".jpg", ".jpeg":
		return true
	}
	return false
}

func normalizeCondition(value string) string {
	cleaned := strings.ToLower(strings.ReplaceAll(value, "_", ""))
	switch {
	case strings.Contains(cleaned, "bad"):
		return "wxbad"
	case strings.Contains(cleaned, "good"):
		return "wxgood"
	default:
		return cleaned
	}
}

func isPathInPublicDir(publicDir string, absoluteImagePath string) bool {
	public := filepath.Clean(publicDir)
	candidate := filepath.Clean(absoluteImagePath)
	public = strings.ToLower(public)
	candidate = strings.ToLower(candidate)
	if !strings.HasSuffix(public, string(filepath.Separator)) {
		public += string(filepath.Separator)
	}
	return strings.HasPrefix(candidate, public)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
