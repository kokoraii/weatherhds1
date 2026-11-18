// Package changeset stages file updates for the server's content (public/, Music/, Videos/) and swaps
// them in together on command, backing up what they replace.
package changeset

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Roots are the workspace folders a change set may write into.
var Roots = []string{"public", "Music", "Videos"}

// Actions are what a presentation can do when a change set is provisioned: reload the whole page,
// rebuild it now, let it finish its loop and then rebuild (rebuildAfter), or carry on (none).
var Actions = []string{"reload", "rebuild", "rebuildAfter", "none"}

// File is one staged file.
type File struct {
	Target   string    `json:"target"` // where it goes, relative to the workspace: public/config.js
	Size     int64     `json:"size"`
	SHA256   string    `json:"sha256"`
	StagedAt time.Time `json:"stagedAt"`
	StagedBy string    `json:"stagedBy"`
}

// Manifest is what's staged: the files, and what each presentation does when they go live. With no
// presControl set, the pages reload.
type Manifest struct {
	Files       []File            `json:"files"`
	PresControl map[string]string `json:"presControl,omitempty"`
}

// Provisioned is what provisionNow put in place.
type Provisioned struct {
	Files       []File
	PresControl map[string]string
	BackupDir   string // where the replaced files went, or "" if none were replaced
}

// Stage is the staging area, in persistentCache/changeset.
type Stage struct {
	workspace string
	dir       string
	mu        sync.Mutex
}

// Open uses the staging area under workspace/persistentCache/changeset.
func Open(workspace string) *Stage {
	return &Stage{workspace: workspace, dir: filepath.Join(workspace, "persistentCache", "changeset")}
}

func (s *Stage) stagedDir() string    { return filepath.Join(s.dir, "staged") }
func (s *Stage) manifestPath() string { return filepath.Join(s.dir, "manifest.json") }

// CleanTarget checks a target path and returns it in its canonical form ("public/config.js"). It must be
// relative, stay inside its folder and start with one of Roots.
func CleanTarget(target string) (string, error) {
	target = strings.TrimSpace(filepath.ToSlash(target))
	if target == "" || path.IsAbs(target) || filepath.IsAbs(target) || filepath.VolumeName(target) != "" {
		return "", fmt.Errorf("%q: give a path relative to the server's folder, e.g. public/config.js", target)
	}
	for _, part := range strings.Split(target, "/") {
		if part == ".." {
			return "", fmt.Errorf("%q: .. isn't allowed", target)
		}
	}
	cleaned := path.Clean(target)
	root, rest, _ := strings.Cut(cleaned, "/")
	if !slices.Contains(Roots, root) || rest == "" {
		return "", fmt.Errorf("%q: files can only go in %s", target, strings.Join(Roots, "/, ")+"/")
	}
	return cleaned, nil
}

// StageFile stages a file's content for target, replacing anything already staged there.
func (s *Stage) StageFile(content io.Reader, target, by string) (File, error) {
	target, err := CleanTarget(target)
	if err != nil {
		return File{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	staged := filepath.Join(s.stagedDir(), filepath.FromSlash(target))
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		return File{}, err
	}
	out, err := os.Create(staged)
	if err != nil {
		return File{}, err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(out, hash), content)
	if err := errors.Join(copyErr, out.Close()); err != nil {
		_ = os.Remove(staged)
		return File{}, err
	}
	file := File{Target: target, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil)), StagedAt: time.Now(), StagedBy: by}

	manifest, err := s.read()
	if err != nil {
		return File{}, err
	}
	manifest.Files = slices.DeleteFunc(manifest.Files, func(f File) bool { return f.Target == target })
	manifest.Files = append(manifest.Files, file)
	return file, s.write(manifest)
}

// StagePath stages a file already on this machine.
func (s *Stage) StagePath(source, target, by string) (File, error) {
	in, err := os.Open(source)
	if err != nil {
		return File{}, err
	}
	defer in.Close()
	if info, err := in.Stat(); err == nil && info.IsDir() {
		return File{}, fmt.Errorf("%s is a folder; stage files one at a time", source)
	}
	return s.StageFile(in, target, by)
}

// SetPresControl records what a presentation does when the change set is provisioned.
func (s *Stage) SetPresControl(presentation, action string) error {
	if !slices.Contains(Actions, action) {
		return fmt.Errorf("action must be one of %s", strings.Join(Actions, ", "))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.read()
	if err != nil {
		return err
	}
	if manifest.PresControl == nil {
		manifest.PresControl = map[string]string{}
	}
	manifest.PresControl[presentation] = action
	return s.write(manifest)
}

// List returns what's staged.
func (s *Stage) List() (Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read()
}

// Discard drops everything staged.
func (s *Stage) Discard() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clear()
}

// Provision puts every staged file in place and empties the stage. Each file it replaces is first copied
// to persistentCache/changeset/backup/<time>/. Each new file is written beside its destination and then
// renamed over it, so nothing ever reads a half-written file.
func (s *Stage) Provision(now time.Time) (Provisioned, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.read()
	if err != nil {
		return Provisioned{}, err
	}
	if len(manifest.Files) == 0 {
		return Provisioned{}, errors.New("nothing is staged")
	}
	result := Provisioned{Files: manifest.Files, PresControl: manifest.PresControl}
	backupDir := filepath.Join(s.dir, "backup", now.Format("20060102-150405"))
	for _, file := range manifest.Files {
		destination := filepath.Join(s.workspace, filepath.FromSlash(file.Target))
		if _, err := os.Stat(destination); err == nil {
			if err := copyFile(destination, filepath.Join(backupDir, filepath.FromSlash(file.Target))); err != nil {
				return Provisioned{}, fmt.Errorf("back up %s: %w", file.Target, err)
			}
			result.BackupDir = backupDir
		}
		staged := filepath.Join(s.stagedDir(), filepath.FromSlash(file.Target))
		temporary := destination + ".changeset-new"
		if err := copyFile(staged, temporary); err != nil {
			return Provisioned{}, fmt.Errorf("write %s: %w", file.Target, err)
		}
		if err := os.Rename(temporary, destination); err != nil {
			_ = os.Remove(temporary)
			return Provisioned{}, fmt.Errorf("put %s in place: %w", file.Target, err)
		}
	}
	return result, s.clear()
}

func (s *Stage) read() (Manifest, error) {
	var manifest Manifest
	payload, err := os.ReadFile(s.manifestPath())
	if errors.Is(err, os.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return manifest, err
	}
	return manifest, json.Unmarshal(payload, &manifest)
}

func (s *Stage) write(manifest Manifest) error {
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.manifestPath(), payload, 0o644)
}

func (s *Stage) clear() error {
	return errors.Join(os.RemoveAll(s.stagedDir()), removeIfExists(s.manifestPath()))
}

func removeIfExists(file string) error {
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}
