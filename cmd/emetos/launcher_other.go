//go:build !darwin

package main

import (
	"context"

	"github.com/rm4n0s/emetos/assets"
	"github.com/rm4n0s/emetos/assets/icons"
	"github.com/rm4n0s/emetos/internal/core/desktopsetup"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/app"
)

// installLauncherFiles writes Emetos's own desktop entry and icons under $XDG_DATA_HOME and returns the files it wrote.
func installLauncherFiles(ctx context.Context, paths storage.Paths, exe string) ([]string, error) {
	res, err := desktopsetup.Install(ctx, paths, app.AppID, assets.DesktopEntry(), icons.PNG, exe)
	if err != nil {
		return nil, err
	}
	return append([]string{res.DesktopFile}, res.IconFiles...), nil
}

// uninstallLauncherFiles removes the files installLauncherFiles wrote and returns the ones that were there.
func uninstallLauncherFiles(ctx context.Context, paths storage.Paths) ([]string, error) {
	res, err := desktopsetup.Uninstall(ctx, paths, app.AppID)
	if err != nil {
		return nil, err
	}
	var removed []string
	if res.DesktopFile != "" {
		removed = append(removed, res.DesktopFile)
	}
	return append(removed, res.IconFiles...), nil
}
