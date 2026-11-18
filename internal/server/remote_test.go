package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestIsURL(t *testing.T) {
	for target, want := range map[string]bool{
		"https://example.com/a.jpg":        true,
		"http://cdn.example.com/v.mp4?x=1": true,
		"images/bg_images/winter/a.avif":   false,
		"file:///etc/passwd":               false,
		"ftp://example.com/a.jpg":          false,
		"https:///no-host":                 false,
		`C:\Users\rai\Videos\promo.mp4`:    false,
	} {
		if got := isURL(target); got != want {
			t.Errorf("isURL(%q) = %v, want %v", target, got, want)
		}
	}
}

func TestDownloadImageKeepsACopyNamedByContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sky.jpg", "/same-sky.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg bytes"))
		case "/page.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, dir := context.Background(), t.TempDir()
	name, err := downloadImage(ctx, server.URL+"/sky.jpg", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, name)); string(got) != "jpeg bytes" || filepath.Ext(name) != ".jpg" {
		t.Fatalf("saved %q as %s", got, name)
	}
	if again, _ := downloadImage(ctx, server.URL+"/same-sky.jpg", dir); again != name {
		t.Fatalf("the same image should get the same name: %s vs %s", again, name)
	}
	if _, err := downloadImage(ctx, server.URL+"/page.html", dir); err == nil {
		t.Fatal("a web page isn't an image")
	}
	if _, err := downloadImage(ctx, server.URL+"/missing.jpg", dir); err == nil {
		t.Fatal("a 404 should fail")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("failed downloads should leave nothing behind, got %d files", len(entries))
	}
}
