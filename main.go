// Package main provides a CLI to install VPN stacks (e.g. L2TP) on Ubuntu and a web UI to manage configs.
package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "netgate/driver/l2tp"
)

const (
	sourcesListPath   = "/etc/apt/sources.list"
	sourcesListBackup = "/etc/apt/sources.list.bak.netgate"
	osReleasePath     = "/etc/os-release"
	arvanMirrorHost   = "https://mirror.arvancloud.ir/ubuntu"
	mirrorProbeTimeout = 10 * time.Second
)

func main() {
	serveCmd := flag.NewFlagSet("serve", flag.ExitOnError)
	port := serveCmd.String("port", defaultPort, "HTTP port for config UI")
	dataDir := serveCmd.String("data", ".", "Data directory for driver configs (e.g. l2tp/config.json)")

	if len(os.Args) >= 2 && os.Args[1] == "serve" {
		_ = serveCmd.Parse(os.Args[2:])
		absData, _ := filepath.Abs(*dataDir)
		fmt.Printf("Config UI at http://0.0.0.0:%s (data: %s)\n", *port, absData)
		if err := runServer(*port, absData); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if os.Geteuid() != 0 {
		fmt.Println("This program must be run with sudo.")
		os.Exit(1)
	}

	if err := RunInstallWithLog(context.Background(), os.Stdout, []string{"l2tp"}, ""); err != nil {
		fmt.Fprintf(os.Stderr, "Error during install: %v\n", err)
		os.Exit(1)
	}
}

func detectUbuntuRelease() (string, error) {
	data, err := os.ReadFile(osReleasePath)
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "VERSION_CODENAME=") {
			s := strings.TrimPrefix(line, "VERSION_CODENAME=")
			s = strings.Trim(s, "\"")
			return s, nil
		}
	}
	return "", fmt.Errorf("VERSION_CODENAME not found in %s", osReleasePath)
}

// ProbeMirror checks if a mirror base URL (e.g. https://mirror.arvancloud.ir/) is reachable for the given Ubuntu release.
func ProbeMirror(ctx context.Context, mirrorBase, release string) (bool, error) {
	base := strings.TrimSuffix(strings.TrimSpace(mirrorBase), "/")
	if base == "" {
		return false, fmt.Errorf("empty mirror URL")
	}
	repoURL := base + "/ubuntu"
	probeURL := repoURL + "/dists/" + release + "/InRelease"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return false, err
	}
	client := &http.Client{Timeout: mirrorProbeTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// NormalizeMirrorBase returns the full repo URL (base/ubuntu) for use in sources.list.
func NormalizeMirrorBase(mirrorBase string) string {
	base := strings.TrimSuffix(strings.TrimSpace(mirrorBase), "/")
	if base == "" {
		return arvanMirrorHost
	}
	return base + "/ubuntu"
}

func useMirrorOnly(release, mirrorBase string, log io.Writer) error {
	logln := func(s string) {
		if log != nil {
			fmt.Fprintln(log, s)
		} else {
			fmt.Println(s)
		}
	}
	content, err := os.ReadFile(sourcesListPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(sourcesListBackup, content, 0644); err != nil {
		return fmt.Errorf("backing up sources.list: %w", err)
	}
	repoURL := NormalizeMirrorBase(mirrorBase)
	body := fmt.Sprintf("# Temporary L2TP install: mirror only (main + universe)\ndeb %s %s main universe\n", repoURL, release)
	if err := os.WriteFile(sourcesListPath, []byte(body), 0644); err != nil {
		return err
	}
	logln("Using mirror (HTTPS): " + repoURL + " " + release + " main universe")
	return nil
}

func restoreSourcesList(log io.Writer) error {
	logln := func(s string) {
		if log != nil {
			fmt.Fprintln(log, s)
		} else {
			fmt.Println(s)
		}
	}
	content, err := os.ReadFile(sourcesListBackup)
	if err != nil {
		return err
	}
	if err := os.WriteFile(sourcesListPath, content, 0644); err != nil {
		return err
	}
	logln("Restored sources.list from backup.")
	return nil
}

func waitForAptLock(ctx context.Context, log io.Writer, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	script := `while fuser /var/lib/apt/lists/lock >/dev/null 2>&1 || fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do sleep 2; done`
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	if log != nil {
		cmd.Stdout = log
		cmd.Stderr = log
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("apt lock wait timed out after %v", timeout)
		}
		return err
	}
	return nil
}

func runAptUpdate(ctx context.Context, log io.Writer) error {
	logln := func(s string) {
		if log != nil {
			fmt.Fprintln(log, s)
		} else {
			fmt.Println(s)
		}
	}
	const maxRetries = 10
	const lockWait = 2 * time.Minute
	for attempt := 0; attempt < maxRetries; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt > 0 {
			logln("Waiting for apt lock to be released...")
			if err := waitForAptLock(ctx, log, lockWait); err != nil {
				return err
			}
			logln("Retrying apt-get update...")
		}
		var out bytes.Buffer
		cmd := exec.CommandContext(ctx, "apt-get", "update", "-y")
		if log != nil {
			cmd.Stdout = io.MultiWriter(log, &out)
			cmd.Stderr = io.MultiWriter(log, &out)
		} else {
			cmd.Stdout = io.MultiWriter(os.Stdout, &out)
			cmd.Stderr = io.MultiWriter(os.Stderr, &out)
		}
		err := cmd.Run()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := out.String()
		if (strings.Contains(msg, "Could not get lock") || strings.Contains(msg, "Unable to lock")) && attempt < maxRetries-1 {
			logln("Apt lock held by another process; waiting 5s before retry...")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
			continue
		}
		return err
	}
	return fmt.Errorf("apt-get update failed after %d retries", maxRetries)
}

func runAptInstall(ctx context.Context, packages []string, log io.Writer) error {
	if len(packages) == 0 {
		return nil
	}
	logln := func(s string) {
		if log != nil {
			fmt.Fprintln(log, s)
		} else {
			fmt.Println(s)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := waitForAptLock(ctx, log, 2*time.Minute); err != nil {
		logln("Warning: " + err.Error())
	}
	args := append([]string{"install", "-y"}, packages...)
	cmd := exec.CommandContext(ctx, "apt-get", args...)
	if log != nil {
		cmd.Stdout = log
		cmd.Stderr = log
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

func runAptPurge(ctx context.Context, packages []string, log io.Writer) error {
	if len(packages) == 0 {
		return nil
	}
	logln := func(s string) {
		if log != nil {
			fmt.Fprintln(log, s)
		} else {
			fmt.Println(s)
		}
	}
	if err := waitForAptLock(ctx, log, 2*time.Minute); err != nil {
		logln("Warning: " + err.Error())
	}
	args := []string{
		"purge", "-y",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
		"-o", "Dpkg::Options::=--force-confmiss",
	}
	args = append(args, packages...)
	cmd := exec.CommandContext(ctx, "apt-get", args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if log != nil {
		cmd.Stdout = log
		cmd.Stderr = log
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

func runSystemctlStop(services []string, log io.Writer) error {
	for _, svc := range services {
		cmd := exec.Command("systemctl", "stop", svc)
		if log != nil {
			cmd.Stdout = log
			cmd.Stderr = log
		} else {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("systemctl stop %s: %w", svc, err)
		}
	}
	return nil
}

// runUfwAllowL2TP opens UFW ports required for L2TP/IPsec: 500/udp, 4500/udp, 1701/udp.
func runUfwAllowL2TP(log io.Writer) error {
	for _, rule := range []string{"500/udp", "4500/udp", "1701/udp"} {
		cmd := exec.Command("ufw", "allow", rule)
		if log != nil {
			cmd.Stdout = log
			cmd.Stderr = log
		} else {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ufw allow %s: %w", rule, err)
		}
	}
	return nil
}

// runUfwDeleteAllowL2TP removes UFW allow rules for L2TP ports; logs and continues on missing-rule errors.
func runUfwDeleteAllowL2TP(log io.Writer) error {
	for _, rule := range []string{"500/udp", "4500/udp", "1701/udp"} {
		cmd := exec.Command("ufw", "delete", "allow", rule)
		cmd.Stdin = strings.NewReader("y\n")
		if log != nil {
			cmd.Stdout = log
			cmd.Stderr = log
		} else {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
		if err := cmd.Run(); err != nil {
			if log != nil {
				fmt.Fprintf(log, "Warning: ufw delete allow %s: %v\n", rule, err)
			}
		}
	}
	return nil
}
