package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"netgate/driver"
)

//go:embed static/index.html
var indexHTML []byte

//go:embed static/fonts/*.woff2
var fontFiles embed.FS

const defaultPort = "8080"

var (
	installMu     sync.Mutex
	installCancel context.CancelFunc
	panelMu       sync.Mutex
	panelCancel   context.CancelFunc
	serverDataDir string
)

var defaultMirrors = []string{"https://mirror.arvancloud.ir/", "https://ubuntu.pishgaman.net/", "https://mirrors.pardisco.co/"}

// runServer starts the config UI HTTP server on port; dataDir is the root for driver configs (e.g. l2tp/config.json).
func runServer(port, dataDir string) error {
	serverDataDir = dataDir
	mux := http.NewServeMux()
	fontFS, _ := fs.Sub(fontFiles, "static/fonts")
	mux.Handle("/fonts/", http.StripPrefix("/fonts/", http.FileServer(http.FS(fontFS))))
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/api/ready", handleReady)
	mux.HandleFunc("/api/drivers", handleDrivers)
	mux.HandleFunc("/api/release", handleRelease)
	mux.HandleFunc("/api/mirrors", handleMirrors)
	mux.HandleFunc("/api/mirrors/check", handleMirrorsCheck)
	mux.HandleFunc("/api/install", handleInstall)
	mux.HandleFunc("/api/install/cancel", handleInstallCancel)
	mux.HandleFunc("/api/uninstall", handleUninstall)
	mux.HandleFunc("/api/panel/install", handlePanelInstall)
	mux.HandleFunc("/api/panel/install/cancel", handlePanelInstallCancel)
	mux.HandleFunc("/api/panel/status", handlePanelStatus)
	mux.HandleFunc("/api/panel/uninstall", handlePanelUninstall)
	driver.ForEach(func(d driver.Driver) {
		if d.RegisterHandlers != nil {
			d.RegisterHandlers(mux, "/api/"+d.ID, dataDir)
		}
	})
	return http.ListenAndServe("0.0.0.0:"+port, mux)
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexHTML)
}

func handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	ready := driver.AnyInstalled()
	json.NewEncoder(w).Encode(map[string]bool{"ready": ready})
}

func handleDrivers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	json.NewEncoder(w).Encode(driver.All())
}

func handleRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	release, err := detectUbuntuRelease()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"release": release})
}

func handleMirrors(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	path := filepath.Join(serverDataDir, "mirrors.json")
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				json.NewEncoder(w).Encode(map[string]interface{}{"mirrors": defaultMirrors})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		var out struct {
			Mirrors []string `json:"mirrors"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"mirrors": defaultMirrors})
			return
		}
		if out.Mirrors == nil {
			out.Mirrors = defaultMirrors
		}
		json.NewEncoder(w).Encode(out)
		return
	case http.MethodPost:
		var body struct {
			Mirrors []string `json:"mirrors"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid body: mirrors array required"})
			return
		}
		if body.Mirrors == nil {
			body.Mirrors = []string{}
		}
		data, err := json.MarshalIndent(map[string]interface{}{"mirrors": body.Mirrors}, "", "  ")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := os.MkdirAll(serverDataDir, 0755); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleMirrorsCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Mirrors []string `json:"mirrors"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid body: mirrors array required"})
		return
	}
	if len(body.Mirrors) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "at least one mirror URL required"})
		return
	}
	release, err := detectUbuntuRelease()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	type result struct {
		URL    string `json:"url"`
		Active bool   `json:"active"`
		Error  string `json:"error,omitempty"`
	}
	results := make([]result, 0, len(body.Mirrors))
	ctx := r.Context()
	for _, u := range body.Mirrors {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		ok, err := ProbeMirror(ctx, u, release)
		if err != nil {
			results = append(results, result{URL: u, Active: false, Error: err.Error()})
		} else {
			results = append(results, result{URL: u, Active: ok})
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{"release": release, "results": results})
}

func handleInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if os.Geteuid() != 0 {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "run server with sudo to use Install"})
		return
	}
	var body struct {
		Drivers []string `json:"drivers"`
		Mirror  string   `json:"mirror"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid body: drivers array required"})
		return
	}
	driverIDs := driver.ValidIDs(body.Drivers)
	if len(driverIDs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "select at least one driver"})
		return
	}
	installMu.Lock()
	if installCancel != nil {
		installMu.Unlock()
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "install already in progress"})
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	installCancel = cancel
	installMu.Unlock()
	defer func() {
		installMu.Lock()
		installCancel = nil
		installMu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	sse := &sseWriter{w: w, flusher: flusher}
	mirror := body.Mirror
	if err := RunInstallWithLog(ctx, sse, driverIDs, mirror); err != nil {
		if ctx.Err() != nil {
			sse.send("Install cancelled.")
		} else {
			sse.send("error: " + err.Error())
		}
		return
	}
	sse.send("[DONE]")
	flusher.Flush()
}

func handleInstallCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	installMu.Lock()
	fn := installCancel
	installCancel = nil
	installMu.Unlock()
	if fn != nil {
		fn()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"ok": "cancelled"})
}

func handleUninstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if os.Geteuid() != 0 {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "run server with sudo to use Uninstall"})
		return
	}
	var body struct {
		Drivers []string `json:"drivers"`
		Mirror  string   `json:"mirror"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid body: drivers array required"})
		return
	}
	driverIDs := driver.ValidIDs(body.Drivers)
	if len(driverIDs) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "select at least one driver"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	sse := &sseWriter{w: w, flusher: flusher}
	if err := RunUninstallWithLog(sse, driverIDs, body.Mirror); err != nil {
		sse.send("error: " + err.Error())
		return
	}
	sse.send("[DONE]")
	flusher.Flush()
}

func handlePanelInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if os.Geteuid() != 0 {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "run server with sudo to install 3x-ui panel"})
		return
	}
	var body struct {
		Proxy string `json:"proxy"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	proxyURL := strings.TrimSpace(body.Proxy)
	panelMu.Lock()
	if panelCancel != nil {
		panelMu.Unlock()
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "panel install already in progress"})
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	panelCancel = cancel
	panelMu.Unlock()
	defer func() {
		panelMu.Lock()
		panelCancel = nil
		panelMu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	sse := &sseWriter{w: w, flusher: flusher}
	if err := RunPanelInstallWithLog(ctx, sse, proxyURL); err != nil {
		if ctx.Err() != nil {
			sse.send("Panel install cancelled.")
		} else {
			sse.send("error: " + err.Error())
		}
		return
	}
	sse.send("[DONE]")
	flusher.Flush()
}

func handlePanelInstallCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	panelMu.Lock()
	fn := panelCancel
	panelCancel = nil
	panelMu.Unlock()
	if fn != nil {
		fn()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{"ok": "cancelled"})
}

func handlePanelStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]bool{"installed": PanelInstalled()})
}

func handlePanelUninstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if os.Geteuid() != 0 {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "run server with sudo to uninstall 3x-ui panel"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	sse := &sseWriter{w: w, flusher: flusher}
	if err := RunPanelUninstallWithLog(r.Context(), sse); err != nil {
		sse.send("error: " + err.Error())
		return
	}
	sse.send("[DONE]")
	flusher.Flush()
}

type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	buf     []byte
}

func (s *sseWriter) Write(p []byte) (n int, err error) {
	s.buf = append(s.buf, p...)
	for {
		i := strings.IndexByte(string(s.buf), '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSuffix(string(s.buf[:i]), "\r")
		s.buf = s.buf[i+1:]
		if line != "" {
			s.send(line)
		}
	}
	return len(p), nil
}

func (s *sseWriter) send(line string) {
	s.w.Write([]byte("data: " + line + "\n\n"))
	s.flusher.Flush()
}
