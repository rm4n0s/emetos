package typescript

import (
	"context"
	"maps"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/tsconfig"
)

// Debugging TypeScript programs (SPEC §10.31 "The debuggers", JavaScript and TypeScript; step 25.10). The debug types node and pwa-node
// are declared and served by the JavaScript pack (a debug type has one owner; js-debug debugs both languages and reads source maps
// itself); this pack adds to them what TypeScript needs: a program that is a TypeScript file is run through the JavaScript the compiler
// made of it, so js-debug is told where that is, from the outDir of the tsconfig.json of the program (outFiles), the way a user writes
// it in launch.json. The type names are written here, not imported, so that the extensions stay apart.

// Ported from vscode-js-debug: src/ui/configuration/nodeDebugConfigurationResolver.ts (createLaunchConfigFromContext: the outFiles of a
// TypeScript program come from the outDir of its tsconfig.json).

// jsDebugTypes are the debug types of the JavaScript pack's debugger.
var jsDebugTypes = []string{"node", "pwa-node"}

// tsExtensions are the extensions of the files the TypeScript compiler turns into JavaScript.
var tsExtensions = []string{".ts", ".tsx", ".mts", ".cts"}

// registerDebug registers the provider that completes the configurations of the JavaScript pack's debug types. A host with no debug
// service has none: the extension then adds nothing to the debugger.
func (t *typescript) registerDebug() {
	d := t.api.Debug()
	for _, typ := range jsDebugTypes {
		provider := extension.DebugConfigurationProvider{ResolveSubstituted: t.resolveOutFiles}
		if _, err := d.RegisterConfigurationProvider(typ, provider, extension.DebugTriggerInitial); err != nil {
			t.api.Log().Debug("the TypeScript pack adds nothing to the debugger", "type", typ, "err", err)
			return
		}
	}
}

// resolveOutFiles gives a launch of a TypeScript program the outFiles of its project: where the tsconfig.json nearest to the program
// puts its output, when the configuration names no outFiles and does not turn the source maps off. Anything else is left as it is.
func (t *typescript) resolveOutFiles(_ context.Context, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
	if _, child := cfg["__pendingTargetId"]; child || cfg["request"] != "launch" || cfg["outFiles"] != nil || cfg["sourceMaps"] == false {
		return cfg, nil
	}
	program, _ := cfg["program"].(string)
	if program == "" || !isTSFile(program) {
		return cfg, nil
	}
	if folder == "" {
		folder = t.api.Scope().Folder
	}
	if !filepath.IsAbs(program) {
		base, _ := cfg["cwd"].(string)
		if base == "" {
			base = folder
		}
		program = filepath.Join(base, program)
	}
	config, ok := tsconfig.Nearest(program, folder)
	if !ok {
		return cfg, nil
	}
	out := tsconfig.OutDir(config)
	if out == "" {
		return cfg, nil
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(filepath.Dir(config), out)
	}
	cfg = maps.Clone(cfg)
	cfg["outFiles"] = []any{filepath.ToSlash(filepath.Clean(out)) + "/**/*.js", "!**/node_modules/**"}
	return cfg, nil
}

// isTSFile tells whether the path is a file the TypeScript compiler compiles (a declaration file is not).
func isTSFile(path string) bool {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".d.ts") || strings.HasSuffix(lower, ".d.mts") || strings.HasSuffix(lower, ".d.cts") {
		return false
	}
	for _, ext := range tsExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}
