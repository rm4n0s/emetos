//go:build darwin

package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/assets/icons"
	"github.com/rm4n0s/emetos/internal/core/desktopsetup"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/app"
)

// launcherName is the name of the application bundle, which Launchpad shows.
const launcherName = "Emetos"

// applicationsDir is the Applications folder of the user, where the bundle goes. It fails with the tag LauncherHomeUnknown when
// $HOME is not an absolute path.
func applicationsDir() (string, error) {
	home := os.Getenv("HOME")
	if !filepath.IsAbs(home) {
		return "", errors.New("LauncherHomeUnknown", "$HOME is not an absolute path, so the Applications folder is unknown", "home", home)
	}
	return filepath.Join(home, "Applications"), nil
}

// installLauncherFiles writes Emetos.app, an application bundle that runs the program at exe, in ~/Applications, and returns it.
func installLauncherFiles(ctx context.Context, _ storage.Paths, exe string) ([]string, error) {
	dir, err := applicationsDir()
	if err != nil {
		return nil, err
	}
	res, err := desktopsetup.InstallBundle(ctx, dir, app.AppID, launcherName, icons.PNG, exe)
	if err != nil {
		return nil, err
	}
	return []string{res.App}, nil
}

// uninstallLauncherFiles removes the bundle installLauncherFiles wrote and returns it when it was there.
func uninstallLauncherFiles(ctx context.Context, _ storage.Paths) ([]string, error) {
	dir, err := applicationsDir()
	if err != nil {
		return nil, err
	}
	res, err := desktopsetup.UninstallBundle(ctx, dir, launcherName)
	if err != nil {
		return nil, err
	}
	if res.App == "" {
		return nil, nil
	}
	return []string{res.App}, nil
}
