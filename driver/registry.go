package driver

import (
	"net/http"
	"os/exec"
	"runtime"
	"strings"
)

// HandlerRegistrar is called to register a driver's HTTP handlers; apiBase e.g. "/api/l2tp", dataDir is the root for driver-specific files (e.g. config).
type HandlerRegistrar func(mux *http.ServeMux, apiBase string, dataDir string)

// Driver describes a VPN/protocol: metadata and optional HTTP handler registration.
type Driver struct {
	ID               string
	DisplayName      string
	Packages         []string
	Services         []string // systemd service names to stop before uninstall (e.g. strongswan-starter, xl2tpd)
	CheckBinary      string
	RegisterHandlers HandlerRegistrar
}

var registry []Driver

// Register adds a driver to the registry.
func Register(d Driver) {
	registry = append(registry, d)
}

// All returns all registered drivers with Installed set from the current system.
func All() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(registry))
	for _, d := range registry {
		out = append(out, map[string]interface{}{
			"id":           d.ID,
			"display_name": d.DisplayName,
			"packages":     d.Packages,
			"installed":    installed(d),
		})
	}
	return out
}

// installed reports whether the driver is installed by checking dpkg for its packages (not by binary path).
func installed(d Driver) bool {
	if runtime.GOOS != "linux" || len(d.Packages) == 0 {
		return false
	}
	for _, pkg := range d.Packages {
		cmd := exec.Command("dpkg-query", "-W", "-f", "${Status}", pkg)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		if strings.Contains(string(out), "installed") {
			return true
		}
	}
	return false
}

// AnyInstalled returns true if at least one registered driver is installed.
func AnyInstalled() bool {
	for _, d := range registry {
		if installed(d) {
			return true
		}
	}
	return false
}

// PackagesFor returns the union of packages for the given driver IDs (no duplicates).
func PackagesFor(ids []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, id := range ids {
		for _, d := range registry {
			if d.ID != id {
				continue
			}
			for _, pkg := range d.Packages {
				if !seen[pkg] {
					seen[pkg] = true
					out = append(out, pkg)
				}
			}
			break
		}
	}
	return out
}

// ValidIDs returns only IDs that exist in the registry.
func ValidIDs(ids []string) []string {
	valid := make(map[string]bool)
	for _, d := range registry {
		valid[d.ID] = true
	}
	var out []string
	for _, id := range ids {
		if valid[id] {
			out = append(out, id)
		}
	}
	return out
}

// PackageList returns a comma-separated list of packages for the given driver IDs.
func PackageList(ids []string) string {
	return strings.Join(PackagesFor(ids), ", ")
}

// ServicesFor returns the union of systemd service names for the given driver IDs (no duplicates).
func ServicesFor(ids []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, id := range ids {
		for _, d := range registry {
			if d.ID != id {
				continue
			}
			for _, svc := range d.Services {
				if svc != "" && !seen[svc] {
					seen[svc] = true
					out = append(out, svc)
				}
			}
			break
		}
	}
	return out
}

// ForEach calls f for each registered driver (e.g. to register HTTP handlers).
func ForEach(f func(Driver)) {
	for _, d := range registry {
		f(d)
	}
}
