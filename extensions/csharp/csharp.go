// Package csharp is the C# language pack (SPEC §8.1, §10.15, §10.34): C#. The grammar and the language configuration are in assets/,
// copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE. Since step 24b.7 the pack also runs csharp-ls, the
// MIT-licensed Roslyn language server (no C# Dev Kit and no part with Microsoft's terms, D49): the server is found, started and shown by
// extensions/internal/langserver (the path setting csharp.languageServer.path, csharp-ls on PATH, then the folder of the .NET global
// tools, the status item, the notification for a missing server), the pack checks that the .NET SDK is there, chooses the solution or
// project the server loads (csharp.solution, the choice of "C#: Select Solution" kept per folder, else the one the folder holds), sends
// the settings of csharp-ls, and shows the decompiled and generated sources of the server (csharp/metadata) as read-only documents.
package csharp

import (
	"context"
	"embed"
	"sync"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The ids of the pack.
const (
	// ID is the id of the extension, and ServerID the id of the language server csharp-ls in its manifest.
	ID       = "emetos.csharp"
	ServerID = "csharp-ls"
)

//go:embed all:assets
var assets embed.FS

// New is the Factory of the pack.
func New() extension.Extension {
	c := &csharp{systemDotnet: systemDotnetDirs, installLocations: dotnetInstallLocations()}
	c.base = langpack.New(langpack.Spec{
		ID: ID, Name: "C#", Description: "C# language support: syntax, csharp-ls and its commands.",
		Assets: assets, Server: c.serverConfig(),
	})
	return c
}

// csharp is the extension of one scope. Everything in it runs on the UI thread unless a comment says otherwise.
type csharp struct {
	base extension.Extension
	api  extension.API
	ctx  context.Context
	ctl  *langserver.Controller

	// systemDotnet are the folders a .NET installation is looked for in, and installLocations the files that name one (dotnet.go); they are
	// fields so that a test gives its own and never reads the machine's.
	systemDotnet     []string
	installLocations []string

	// mu guards current and ambiguous, which the launcher's goroutine writes.
	mu sync.Mutex
	// current is the target the last start used, and ambiguous the files of the folder to choose from when the folder holds several
	// solutions and the user chose none (solution.go).
	current   target
	ambiguous []string
	// offered says that the user was told in this tab that there is a choice to make.
	offered bool
	// reg is the provider of the read-only documents; nil when the scope cannot show them.
	reg extension.ContentRegistration
	// work waits for the goroutines the pack started, so that a test can see that none is left.
	work sync.WaitGroup
}

// Manifest implements extension.Extension: the manifest of the pack with the commands and settings of csharp-ls.
func (c *csharp) Manifest() extension.Manifest {
	m := c.base.Manifest()
	m.Contributes.Commands = append(m.Contributes.Commands, commandSpecs()...)
	m.Contributes.Settings = append(m.Contributes.Settings, settingSpecs()...)
	return m
}

// Activate implements extension.Extension.
func (c *csharp) Activate(ctx context.Context, api extension.API) error {
	c.api, c.ctx = api, ctx
	if err := c.base.Activate(ctx, api); err != nil {
		return err
	}
	c.ctl = langpack.Controller(c.base)
	for id, handler := range c.handlers() {
		if err := api.Commands().Handle(id, handler); err != nil {
			return err
		}
	}
	c.provideContent()
	return nil
}

// wait waits for the work the pack started to end (the tests use it).
func (c *csharp) wait() { c.work.Wait() }
