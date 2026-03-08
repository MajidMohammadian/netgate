package l2tp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"netgate/driver"
)

var dataDir string

func init() {
	driver.Register(driver.Driver{
		ID:          "l2tp",
		DisplayName: "L2TP",
		Packages:    []string{"strongswan", "xl2tpd", "ppp"},
		Services:    []string{"strongswan-starter", "xl2tpd"},
		CheckBinary: "ipsec",
		RegisterHandlers: func(mux *http.ServeMux, base string, dir string) {
			dataDir = dir
			mux.HandleFunc(base+"/config", handleConfig)
			mux.HandleFunc(base+"/connect", handleConnect)
			mux.HandleFunc(base+"/disconnect", handleDisconnect)
		},
	})
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	switch r.Method {
	case http.MethodGet:
		list, err := LoadConfigs()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []VPNConfig{}
		}
		json.NewEncoder(w).Encode(list)
		return
	case http.MethodPost:
		var list []VPNConfig
		if err := json.NewDecoder(r.Body).Decode(&list); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		if err := SaveConfigs(list); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if os.Geteuid() != 0 {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "run server with sudo to use Connect"})
		return
	}
	var c VPNConfig
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if r.URL.Query().Get("stream") == "1" {
		handleConnectStream(w, c)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := EnsureL2TPServices(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if err := ApplyConfig(c); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	log, err := RunConnectScript()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "log": log})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "log": log})
}

func handleConnectStream(w http.ResponseWriter, c VPNConfig) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		flusher = nil
	}
	send := func(v interface{}) {
		data, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	onStep := func(stepID StepID, path string) {
		send(map[string]string{"type": "step", "id": string(stepID), "path": path})
	}
	if err := EnsureL2TPServices(); err != nil {
		send(map[string]string{"type": "done", "ok": "false", "error": err.Error()})
		return
	}
	onStep(StepEnsureServices, "strongswan-starter, xl2tpd")
	if err := ApplyConfigWithSteps(c, onStep); err != nil {
		send(map[string]string{"type": "done", "ok": "false", "error": err.Error()})
		return
	}
	log, err := RunConnectScript()
	if err != nil {
		send(map[string]interface{}{"type": "done", "ok": false, "error": err.Error(), "log": log})
		return
	}
	onStep(StepRunScript, pathConnectScript)
	send(map[string]interface{}{"type": "done", "ok": true, "log": log})
}

func handleDisconnect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if os.Geteuid() != 0 {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "run server with sudo to use Disconnect"})
		return
	}
	if err := RunDisconnect(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
