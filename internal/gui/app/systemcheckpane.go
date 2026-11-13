package app

import (
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/extensionsui"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The page "System Check" in the window (SPEC §10.36 "The Check System page", step 34.5): the kind of editor of a SystemCheckInput,
// the command emetos.checkSystem that opens it ("Help: Check System"), and the line that names the system. It shows the same results
// as the Extensions page: the window's checker (requirements.go).

// systemCheckKind is how the window handles SystemCheckInputs.
func systemCheckKind() editorKind {
	return editorKind{
		typeID: editorgroup.SystemCheckInputTypeID,
		icon:   fonts.CodiconChecklist,
		restore: func(value string) (editorgroup.EditorInput, bool) {
			in, err := editorgroup.ParseSystemCheckInput(value)
			return in, err == nil
		},
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			return &systemCheckPane{s: s, input: input.(*editorgroup.SystemCheckInput)}
		},
	}
}

// systemCheckPane is the editor of a SystemCheckInput.
type systemCheckPane struct {
	paneFocus
	s     *shellState
	input *editorgroup.SystemCheckInput
}

// build implements pane.
func (p *systemCheckPane) build(bc widgets.BuildContext, group *editorgroup.Group, _ bool) widgets.Widget {
	win := PeekWorkspace(bc)
	reqs := win.Process.Workspaces.requirements(widgets.WidgetsBindingOf(bc).Post)
	return p.wrap(p.s, group, extensionsui.NewSystemCheckPage(extensionsui.SystemCheckOptions{
		Requirements: reqs.Rows,
		Disabled:     win.disabledExtensions,
		// the turned-off extensions change with extensions.disabled, which the host follows
		Subscribe:    []func(func()) func(){reqs.OnChange, win.Process.Extensions.OnChange},
		OnShown:      reqs.Start,
		OnCheckAgain: reqs.CheckAgain,
		System:       win.Process.systemName(),
		Folder:       reqs.Folder,
		Actions:      p.s.requirementActions(),
	}))
}

// focus implements pane: the page has nothing that takes the keyboard.
func (p *systemCheckPane) focus() {}

// dispose implements pane.
func (p *systemCheckPane) dispose() {}

// openSystemCheckPage shows the page: the one a group has is revealed, else a new one opens in the active group.
func (s *shellState) openSystemCheckPage() {
	s.openSingletonPage(func(e editorgroup.EditorInput) bool { _, ok := e.(*editorgroup.SystemCheckInput); return ok },
		func() editorgroup.EditorInput { return editorgroup.NewSystemCheckInput() })
}

// requirementActions are what the buttons of the requirements do, on both pages: copy, run the install command, open the Settings
// editor on the path setting, open the instructions.
func (s *shellState) requirementActions() extensionsui.RequirementActions {
	win := PeekWorkspace(s.Context())
	return extensionsui.RequirementActions{
		Family:    win.Process.systemFamily(),
		OnCopy:    s.copyText,
		OnSetPath: s.setRequirementPath,
		OnInstall: func(id string) {
			if err := win.runCommand(id, nil); err != nil {
				win.Process.Logger.Warn("cannot run the install command of a requirement", "command", id, "err", err)
				win.Notify.Warn("Emetos", "Cannot install: "+describe(err))
			}
		},
		OnOpenURI: func(uri string) {
			if err := win.openURI(win.ctx, uri); err != nil {
				win.Process.Logger.Warn("cannot open the instructions of a requirement", "uri", uri, "err", err)
				win.Notify.Warn("Emetos", "Cannot open "+uri+": "+describe(err))
			}
		},
	}
}

// systemName is the line of the System Check page that names the system (ProcessOptions.SystemName in tests), read once.
func (p *Process) systemName() string {
	p.systemOnce.Do(func() {
		if p.system != "" {
			return
		}
		var osRelease, plist string
		switch runtime.GOOS {
		case "darwin":
			if data, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist"); err == nil {
				plist = string(data)
			}
		default:
			osRelease = extension.ReadOSRelease()
		}
		p.system = systemLine(runtime.GOOS, runtime.GOARCH, osRelease, plist, p.systemFamily())
	})
	return p.system
}

// plistVersion finds the ProductVersion of macOS's SystemVersion.plist.
var plistVersion = regexp.MustCompile(`<key>ProductVersion</key>\s*<string>([^<]+)</string>`)

// systemLine names a system: the PRETTY_NAME of its os-release (else its NAME, else GOOS), or "macOS" and its version, then its
// family when it has one, then GOOS/GOARCH: "Fedora Linux 45 (Workstation Edition), fedora, linux/amd64".
func systemLine(goos, goarch, osRelease, plist, family string) string {
	name := goos
	if goos == "darwin" {
		name = "macOS"
		if m := plistVersion.FindStringSubmatch(plist); m != nil {
			name += " " + strings.TrimSpace(m[1])
		}
	} else {
		fields := map[string]string{}
		for _, line := range strings.Split(osRelease, "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				continue
			}
			if u, err := strconv.Unquote(value); err == nil {
				value = u
			} else {
				value = strings.Trim(value, `"'`)
			}
			fields[key] = value
		}
		switch {
		case fields["PRETTY_NAME"] != "":
			name = fields["PRETTY_NAME"]
		case fields["NAME"] != "":
			name = fields["NAME"]
		}
	}
	parts := []string{name}
	if family != "" {
		parts = append(parts, family)
	}
	return strings.Join(append(parts, goos+"/"+goarch), ", ")
}
