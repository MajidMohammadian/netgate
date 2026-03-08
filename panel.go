package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const panelInstallScriptURL = "https://raw.githubusercontent.com/MHSanaei/3x-ui/master/install.sh"

// RunPanelInstallWithLog runs the 3x-ui panel install script (curl | bash), streaming output to w.
// If proxyURL is non-empty (e.g. "http://127.0.0.1:7890"), HTTP_PROXY and HTTPS_PROXY are set so the script can reach GitHub. Requires root.
func RunPanelInstallWithLog(ctx context.Context, w io.Writer, proxyURL string) error {
	script := fmt.Sprintf("curl -Ls %s | bash", panelInstallScriptURL)
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Env = os.Environ()
	if proxyURL != "" {
		cmd.Env = append(cmd.Env, "HTTP_PROXY="+proxyURL, "HTTPS_PROXY="+proxyURL, "http_proxy="+proxyURL, "https_proxy="+proxyURL)
	}
	if w != nil {
		cmd.Stdout = w
		cmd.Stderr = w
	}
	return cmd.Run()
}

// PanelInstalled reports whether the x-ui binary is available (3x-ui panel installed).
func PanelInstalled() bool {
	return exec.Command("sh", "-c", "command -v x-ui 2>/dev/null").Run() == nil
}

// RunPanelUninstallWithLog runs x-ui uninstall, streaming output to w. Caller should confirm. Requires root.
func RunPanelUninstallWithLog(ctx context.Context, w io.Writer) error {
	cmd := exec.CommandContext(ctx, "x-ui", "uninstall")
	cmd.Stdin = strings.NewReader("y\n")
	if w != nil {
		cmd.Stdout = w
		cmd.Stderr = w
	}
	return cmd.Run()
}
