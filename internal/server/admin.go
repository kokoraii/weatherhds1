package server

import (
	"net/http"
)

const adminPort = 1005

func newAdminHandler(adminWebroot string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(adminWebroot)))
	mux.HandleFunc("GET /heartbeat", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		mux.ServeHTTP(w, r)
	})
}
