package rust

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The Run lenses (SPEC §10.34 "Rust"): "Run", "Run Test", "Run Tests", "Run Bench" and "Run Doctest" above the functions and modules
// rust-analyzer says can be run (the request experimental/runnables). Clicking one runs the cargo command line of the runnable through
// the Tasks extension (emetos.tasks.run), in its panel, until M25 and M26 take the lenses to the debugger and the Testing view. They are
// the pack's own and not the lenses rust-analyzer would send: those name the commands rust-analyzer.runSingle and debugSingle, which a
// command of an extension could run only by running the task itself.

// The settings that turn the lenses on and off, with rust-analyzer's names.
const (
	lensEnableSetting    = SettingPrefix + "lens.enable"
	lensRunEnableSetting = SettingPrefix + "lens.run.enable"
)

// RunTaskCommand is the command of the Tasks extension that runs a command line.
const RunTaskCommand = "emetos.tasks.run"

// readLensSettings reads lens.enable and lens.run.enable.
func (r *rust) readLensSettings() {
	on := true
	for _, key := range []string{lensEnableSetting, lensRunEnableSetting} {
		v, err := extension.SettingValue[bool](r.api.Settings(), key)
		if err == nil && !v {
			on = false
		}
	}
	r.lensesOn.Store(on)
}

// lensSettingsChanged asks the editors for the lenses again when they were turned on or off.
func (r *rust) lensSettingsChanged() {
	before := r.lensesOn.Load()
	r.readLensSettings()
	if r.lensesOn.Load() != before {
		r.api.Languages().RefreshCodeLenses()
	}
}

// lenses is the provider of the Run lenses.
type lenses struct{ r *rust }

// runnable is an entry of the answer of experimental/runnables.
type runnable struct {
	Label    string `json:"label"`
	Location *struct {
		TargetURI            string            `json:"targetUri"`
		TargetRange          langserver.Range  `json:"targetRange"`
		TargetSelectionRange *langserver.Range `json:"targetSelectionRange"`
	} `json:"location"`
	Kind string `json:"kind"`
	Args struct {
		// the arguments of kind "cargo"
		CWD            string            `json:"cwd"`
		WorkspaceRoot  string            `json:"workspaceRoot"`
		CargoArgs      []string          `json:"cargoArgs"`
		ExecutableArgs []string          `json:"executableArgs"`
		Environment    map[string]string `json:"environment"`
		OverrideCargo  string            `json:"overrideCargo"`
		// the arguments of kind "shell"
		Program string   `json:"program"`
		Args    []string `json:"args"`
	} `json:"args"`
}

// ProvideCodeLenses implements extension.CodeLensProvider. It runs off the UI thread: it asks rust-analyzer for the runnables of the
// file, and gives no lenses while the server does not run.
func (l lenses) ProvideCodeLenses(ctx context.Context, doc extension.TextDocument) ([]extension.CodeLens, error) {
	r := l.r
	if !r.lensesOn.Load() {
		return nil, nil
	}
	params := struct {
		TextDocument document `json:"textDocument"`
		Position     *int     `json:"position"`
	}{TextDocument: document{doc.URI}}
	result, err := r.api.LanguageServers().Request(ctx, ServerID, "experimental/runnables", params)
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ExtensionServerNotRunning" {
			return nil, nil
		}
		return nil, err
	}
	var runnables []runnable
	if isNull(result) || json.Unmarshal(result, &runnables) != nil {
		return nil, nil
	}
	enc, err := r.api.LanguageServers().PositionEncoding(ServerID)
	if err != nil {
		return nil, err
	}
	folder := r.api.Scope().Folder
	var out []extension.CodeLens
	for _, run := range runnables {
		if run.Location == nil || run.Location.TargetURI != doc.URI {
			continue
		}
		cmdline, ok := run.commandLine(folder)
		if !ok {
			continue
		}
		cmd, err := extension.NewCommand(run.title(), RunTaskCommand, cmdline)
		if err != nil {
			return nil, err
		}
		at := run.Location.TargetRange
		if run.Location.TargetSelectionRange != nil {
			at = *run.Location.TargetSelectionRange
		}
		start := langserver.FromProtocol(doc.Text, at.Start, enc)
		out = append(out, extension.CodeLens{Range: extension.Range{Start: start, End: start}, Command: cmd})
	}
	return out, nil
}

// title is the text of the lens.
func (run runnable) title() string {
	args := run.Args.CargoArgs
	if len(args) == 0 {
		return "Run"
	}
	switch args[0] {
	case "test":
		for _, a := range args {
			if a == "--doc" {
				return "Run Doctest"
			}
		}
		for _, a := range run.Args.ExecutableArgs {
			if a == "--exact" {
				return "Run Test"
			}
		}
		return "Run Tests"
	case "bench":
		return "Run Bench"
	}
	return "Run"
}

// commandLine is the shell command line of the runnable: the environment it asks for, a cd into its directory when that is not the
// folder of the tab, cargo (or the program it names) with its arguments, and the arguments of the program it builds after "--". It is
// false for a runnable of a kind the pack does not run.
func (run runnable) commandLine(folder string) (string, bool) {
	var words []string
	switch run.Kind {
	case "cargo":
		program := "cargo"
		if run.Args.OverrideCargo != "" {
			program = run.Args.OverrideCargo
		}
		words = append(words, program)
		words = append(words, run.Args.CargoArgs...)
		if len(run.Args.ExecutableArgs) > 0 {
			words = append(words, "--")
			words = append(words, run.Args.ExecutableArgs...)
		}
	case "shell":
		if run.Args.Program == "" {
			return "", false
		}
		words = append([]string{run.Args.Program}, run.Args.Args...)
	default:
		return "", false
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = shellQuote(w)
	}
	var sb strings.Builder
	if cwd := run.Args.CWD; cwd != "" && cwd != folder {
		sb.WriteString("cd " + shellQuote(cwd) + " && ")
	}
	names := make([]string, 0, len(run.Args.Environment))
	for name := range run.Args.Environment {
		if validEnvName(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		sb.WriteString(name + "=" + shellQuote(run.Args.Environment[name]) + " ")
	}
	sb.WriteString(strings.Join(quoted, " "))
	return sb.String(), true
}

// shellQuote quotes a word for sh -c: left as it is when it has only characters that mean nothing to the shell.
func shellQuote(w string) string {
	if w != "" && !strings.ContainsFunc(w, func(r rune) bool { return !plainShellRune(r) }) {
		return w
	}
	return "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
}

// plainShellRune tells whether the shell takes the character as it is in a word.
func plainShellRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r)
}

// validEnvName tells whether the name can stand before = in a shell command line.
func validEnvName(name string) bool {
	for i, r := range name {
		letter := r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !letter && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return name != ""
}
