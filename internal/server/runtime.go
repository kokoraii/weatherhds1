package server

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const defaultEnvTemplate = `# WeatherHDS local environment.
# These values override matching public/config.js serverConfig values when present.
TWC_API_KEY=
MAPBOX_API_KEY=
WEB_PORT=3000
UNITS=m
CACHE_VALID_TIME=720
`

type RuntimePaths struct {
	WorkspaceDir string
	PublicDir    string
	AdminDir     string
	ConfigPath   string
	EnvPath      string
}

func ensureRuntimeFiles(workspaceDir string, embedded fs.FS) (RuntimePaths, error) {
	paths := RuntimePaths{
		WorkspaceDir: workspaceDir,
		PublicDir:    filepath.Join(workspaceDir, "public"),
		AdminDir:     filepath.Join(workspaceDir, "admin"),
		ConfigPath:  filepath.Join(workspaceDir, "public", "config.js"),
		EnvPath:      filepath.Join(workspaceDir, ".env"),
	}

	if embedded != nil && !fileExists(paths.PublicDir) {
		if err := extractEmbeddedDir(embedded, "public", paths.PublicDir); err != nil {
			return RuntimePaths{}, err
		}
	}

	if embedded != nil && !fileExists(paths.AdminDir) {
		if err := extractEmbeddedDir(embedded, "admin", paths.AdminDir); err != nil {
			return RuntimePaths{}, err
		}
	}

	if !fileExists(paths.ConfigPath) {
		if err := copyEmbeddedFile(embedded, "public/config.js", paths.ConfigPath); err != nil {
			return RuntimePaths{}, err
		}
	}

	if !fileExists(paths.EnvPath) {
		if err := os.WriteFile(paths.EnvPath, []byte(defaultEnvTemplate), 0o644); err != nil {
			return RuntimePaths{}, err
		}
	}

	return paths, nil
}

func extractEmbeddedDir(source fs.FS, sourceDir string, targetDir string) error {
	cleanTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}

	return fs.WalkDir(source, sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if relativePath == "." {
			return os.MkdirAll(cleanTarget, 0o755)
		}

		targetPath := filepath.Clean(filepath.Join(cleanTarget, filepath.FromSlash(relativePath)))
		if !isPathInsideDir(cleanTarget, targetPath) {
			return fmt.Errorf("embedded path escapes target directory: %s", path)
		}

		if entry.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		return copyEmbeddedFile(source, path, targetPath)
	})
}

func copyEmbeddedFile(source fs.FS, sourcePath string, targetPath string) error {
	if source == nil {
		return fmt.Errorf("embedded asset %s is unavailable", sourcePath)
	}
	input, err := source.Open(sourcePath)
	if err != nil {
		return err
	}
	defer input.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	defer output.Close()

	_, err = io.Copy(output, input)
	return err
}

func isPathInsideDir(parent string, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (!strings.HasPrefix(relative, "..") && !filepath.IsAbs(relative))
}
