package server

import (
	"encoding/json"
	"net"
	"net/http"

	"weatherhds/internal/changeset"
	"weatherhds/internal/control"
)

// maxStagedFile caps one upload to the change set: videos are the large ones.
const maxStagedFile = 4 << 30

// newAdminHandler serves the admin panel and the command API. The API only answers this machine:
// remote control goes through the SSH console, which authenticates.
//
//	POST /api/command                   {"type": "reload", "command": "refresh", "target": "", "args": {}, "feed": "sd"}
//	GET  /api/feeds                     the connected pages
//	PUT  /api/changeset/files/<target>  stages the request body to go live at target (public/config.js, ...)
func newAdminHandler(adminWebroot string, dispatcher *control.Dispatcher, stage *changeset.Stage) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(adminWebroot)))
	mux.HandleFunc("GET /heartbeat", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /api/command", localOnly(func(w http.ResponseWriter, r *http.Request) {
		var request control.Request
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		request.From = "admin API (" + r.RemoteAddr + ")"
		outcome, err := dispatcher.Run(r.Context(), request)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, outcome)
	}))
	mux.HandleFunc("GET /api/feeds", localOnly(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, dispatcher.Hub().Pages())
	}))
	mux.HandleFunc("PUT /api/changeset/files/{target...}", localOnly(func(w http.ResponseWriter, r *http.Request) {
		file, err := stage.StageFile(http.MaxBytesReader(w, r.Body, maxStagedFile), r.PathValue("target"), "admin API ("+r.RemoteAddr+")")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, file)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		mux.ServeHTTP(w, r)
	})
}

// localOnly refuses requests from other machines.
func localOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "the command API only answers this machine; use the SSH console"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
