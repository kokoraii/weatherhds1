package changeset

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanTargetKeepsFilesInsideTheirFolders(t *testing.T) {
	good := map[string]string{
		"public/config.js":          "public/config.js",
		`public\images\new.avif`:    "public/images/new.avif",
		"Videos/promo.mp4":          "Videos/promo.mp4",
		"public/./maps/borders.txt": "public/maps/borders.txt",
	}
	for input, want := range good {
		if got, err := CleanTarget(input); err != nil || got != want {
			t.Errorf("CleanTarget(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, bad := range []string{"", "config.js", "public", "public/../.env", "/etc/passwd", `C:\Windows\x`, "internal/server/server.go"} {
		if got, err := CleanTarget(bad); err == nil {
			t.Errorf("CleanTarget(%q) = %q, want an error", bad, got)
		}
	}
}

func TestProvisionSwapsFilesInAndBacksUpWhatTheyReplace(t *testing.T) {
	workspace := t.TempDir()
	existing := filepath.Join(workspace, "public", "config.js")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	stage := Open(workspace)
	if _, err := stage.StageFile(strings.NewReader("first try"), "public/config.js", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.StageFile(strings.NewReader("new"), "public/config.js", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.StageFile(strings.NewReader("video"), "Videos/promo.mp4", "test"); err != nil {
		t.Fatal(err)
	}
	if err := stage.SetPresControl("main", "rebuildAfter"); err != nil {
		t.Fatal(err)
	}
	if err := stage.SetPresControl("main", "explode"); err == nil {
		t.Fatal("an unknown action should be refused")
	}
	manifest, err := stage.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 2 {
		t.Fatalf("restaging a target replaces it: want 2 files, got %+v", manifest.Files)
	}

	result, err := stage.Provision(time.Date(2026, 11, 18, 2, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(existing); string(got) != "new" {
		t.Fatalf("config.js = %q, want the staged version", got)
	}
	if got, _ := os.ReadFile(filepath.Join(workspace, "Videos", "promo.mp4")); string(got) != "video" {
		t.Fatalf("new file not written: %q", got)
	}
	backup, err := os.ReadFile(filepath.Join(result.BackupDir, "public", "config.js"))
	if err != nil || string(backup) != "old" {
		t.Fatalf("backup = %q, %v; want the replaced file", backup, err)
	}
	if result.PresControl["main"] != "rebuildAfter" {
		t.Fatalf("presControl lost: %+v", result.PresControl)
	}
	if manifest, _ := stage.List(); len(manifest.Files) != 0 || manifest.PresControl != nil {
		t.Fatalf("the stage should be empty after provisioning, got %+v", manifest)
	}
	if _, err := stage.Provision(time.Now()); err == nil {
		t.Fatal("provisioning an empty stage should say so")
	}
}

func TestDiscardEmptiesTheStage(t *testing.T) {
	stage := Open(t.TempDir())
	if _, err := stage.StageFile(strings.NewReader("x"), "public/a.txt", "test"); err != nil {
		t.Fatal(err)
	}
	if err := stage.Discard(); err != nil {
		t.Fatal(err)
	}
	if manifest, _ := stage.List(); len(manifest.Files) != 0 {
		t.Fatalf("still staged: %+v", manifest.Files)
	}
}

func TestStageURLDownloadsIntoTheStage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("downloaded"))
	}))
	defer server.Close()

	workspace := t.TempDir()
	stage := Open(workspace)
	if _, err := stage.StageURL(context.Background(), server.URL+"/promo.mp4", "../escape.mp4", "test"); err == nil {
		t.Fatal("a bad target should be refused before downloading")
	}
	if _, err := stage.StageURL(context.Background(), server.URL+"/missing", "Videos/x.mp4", "test"); err == nil {
		t.Fatal("a 404 should be refused")
	}
	file, err := stage.StageURL(context.Background(), server.URL+"/promo.mp4", "Videos/promo.mp4", "test")
	if err != nil {
		t.Fatal(err)
	}
	if file.Size != int64(len("downloaded")) {
		t.Fatalf("size %d", file.Size)
	}
	if _, err := stage.Provision(time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(workspace, "Videos", "promo.mp4")); string(got) != "downloaded" {
		t.Fatalf("provisioned %q", got)
	}
}
