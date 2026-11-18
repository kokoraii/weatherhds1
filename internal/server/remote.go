package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	remoteCheckTimeout   = 15 * time.Second
	imageDownloadTimeout = 2 * time.Minute
	imageDownloadLimit   = 64 << 20
)

// imageExtensions are the background formats a downloaded copy can be saved as, by content type.
var imageExtensions = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/avif": ".avif", "image/gif": ".gif",
}

// isURL reports whether a command's target is a web address rather than a file on the server.
func isURL(target string) bool {
	parsed, err := url.Parse(target)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// checkRemote makes sure a URL answers before the pages are told to load it, and returns its content
// type. It asks with HEAD, and with a one-byte GET for servers that don't answer HEAD.
func checkRemote(ctx context.Context, target string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, remoteCheckTimeout)
	defer cancel()
	response, err := remoteRequest(ctx, http.MethodHead, target)
	if err == nil && (response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusForbidden) {
		response, err = remoteRequest(ctx, http.MethodGet, target)
	}
	if err != nil {
		return "", fmt.Errorf("%s didn't answer: %w", target, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("%s answered %s", target, response.Status)
	}
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	return contentType, nil
}

func remoteRequest(ctx context.Context, method, target string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	if method == http.MethodGet {
		request.Header.Set("Range", "bytes=0-0")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	_ = response.Body.Close()
	return response, nil
}

// downloadImage saves a copy of a web image in dir and returns its file name, named by its content so a
// changed image gets a new name (and isn't served from a browser's cache). Backgrounds are shown from such
// copies: the pages' glass panels draw the background into a canvas, which browsers forbid for another
// site's image without CORS headers, and a copy also outlives the link.
func downloadImage(ctx context.Context, target, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, imageDownloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("%s didn't answer: %w", target, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("%s answered %s", target, response.Status)
	}
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	extension, isImage := imageExtensions[contentType]
	if !isImage {
		return "", fmt.Errorf("%s isn't a JPEG, PNG, WebP, AVIF or GIF image (it's %s)", target, contentType)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	out, err := os.CreateTemp(dir, "download-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(out.Name()) // a no-op once renamed
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, hash), io.LimitReader(response.Body, imageDownloadLimit+1))
	if err := errors.Join(copyErr, out.Close()); err != nil {
		return "", err
	}
	if written > imageDownloadLimit {
		return "", fmt.Errorf("%s is over %d MB", target, imageDownloadLimit>>20)
	}
	name := hex.EncodeToString(hash.Sum(nil)[:12]) + extension
	if err := os.Rename(out.Name(), filepath.Join(dir, name)); err != nil {
		return "", err
	}
	return name, nil
}
