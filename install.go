package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"netgate/driver"
)

func driverIDsContain(ids []string, id string) bool {
	for _, s := range ids {
		if s == id {
			return true
		}
	}
	return false
}

// RunInstallWithLog runs the full install flow for the given driver IDs: mirror, apt update, apt install (their packages), restore. mirror is the mirror base URL (e.g. https://mirror.arvancloud.ir/); empty means default. Writes progress to w. Cancels when ctx is done.
func RunInstallWithLog(ctx context.Context, w io.Writer, driverIDs []string, mirror string) error {
	driverIDs = driver.ValidIDs(driverIDs)
	if len(driverIDs) == 0 {
		return fmt.Errorf("no valid drivers selected")
	}
	packages := driver.PackagesFor(driverIDs)
	if len(packages) == 0 {
		return fmt.Errorf("no packages to install for selected drivers")
	}
	log := func(s string) { fmt.Fprintln(w, s) }
	if ctx.Err() != nil {
		return ctx.Err()
	}
	log("Detecting Ubuntu release...")
	release, err := detectUbuntuRelease()
	if err != nil {
		return err
	}
	log("Release: " + release)
	log("Using mirror (HTTPS)...")
	if err := useMirrorOnly(release, mirror, w); err != nil {
		return err
	}
	defer func() {
		log("Restoring sources.list...")
		_ = restoreSourcesList(w)
	}()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	log("Repos set. Running apt-get update...")
	if err := runAptUpdate(ctx, w); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	log("Installing " + driver.PackageList(driverIDs) + "...")
	if err := runAptInstall(ctx, packages, w); err != nil {
		return err
	}
	if driverIDsContain(driverIDs, "l2tp") {
		log("Opening UFW ports for L2TP (500/udp, 4500/udp, 1701/udp)...")
		if err := runUfwAllowL2TP(w); err != nil {
			log("Warning: " + err.Error())
		} else {
			log("UFW rules added.")
		}
	}
	log("Install completed successfully.")
	return nil
}

// RunUninstallWithLog removes the given drivers: sets mirror (if provided), stops services, purges packages, restores sources. mirror is the mirror base URL; empty means default. Writes progress to w.
func RunUninstallWithLog(w io.Writer, driverIDs []string, mirror string) error {
	driverIDs = driver.ValidIDs(driverIDs)
	if len(driverIDs) == 0 {
		return fmt.Errorf("no valid drivers selected")
	}
	packages := driver.PackagesFor(driverIDs)
	if len(packages) == 0 {
		return fmt.Errorf("no packages to remove for selected drivers")
	}
	log := func(s string) { fmt.Fprintln(w, s) }
	release, err := detectUbuntuRelease()
	if err != nil {
		return err
	}
	log("Using mirror for uninstall...")
	if err := useMirrorOnly(release, mirror, w); err != nil {
		return err
	}
	defer func() {
		log("Restoring sources.list...")
		_ = restoreSourcesList(w)
	}()
	services := driver.ServicesFor(driverIDs)
	if len(services) > 0 {
		log("Stopping services: " + strings.Join(services, ", "))
		if err := runSystemctlStop(services, w); err != nil {
			log("Warning: " + err.Error())
		}
	}
	if driverIDsContain(driverIDs, "l2tp") {
		log("Removing UFW allow rules for L2TP ports...")
		runUfwDeleteAllowL2TP(w)
	}
	log("Purging " + driver.PackageList(driverIDs) + "...")
	if err := runAptPurge(context.Background(), packages, w); err != nil {
		return err
	}
	log("Uninstall completed successfully.")
	return nil
}
