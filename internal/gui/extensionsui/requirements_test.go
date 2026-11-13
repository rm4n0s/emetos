package extensionsui

import (
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// rowOf is a checked requirement of the tests.
func rowOf(req extension.Requirement, res extension.RequirementResult) exthost.RequirementRow {
	return exthost.RequirementRow{ExtensionID: "emetos.x", ExtensionName: "X", Requirement: req, Result: res}
}

var testInstall = extension.Install{Docs: "https://example.com/gopls", Commands: []extension.InstallCommand{
	{Family: extension.FamilyFedora, Command: "sudo dnf install gopls"},
	{Family: extension.FamilyAny, Command: "go install golang.org/x/tools/gopls@latest"},
}}

func TestTheLineOfARequirement(t *testing.T) {
	gopls := extension.Requirement{ID: "gopls", Name: "gopls", Install: testInstall, PathSetting: "go.goplsPath", InstallAction: "emetos.golang.installTools"}
	dlv := extension.Requirement{ID: "dlv", Name: "dlv", Optional: true, Install: testInstall}
	tests := []struct {
		name string
		row  exthost.RequirementRow
		look RequirementLook
		text string
	}{
		{"not checked yet", exthost.RequirementRow{Requirement: gopls}, LookChecking, "gopls  •  Not checked yet"},
		{"checking", exthost.RequirementRow{Requirement: gopls, Checking: true, Result: extension.RequirementResult{State: extension.RequirementFound}}, LookChecking, "gopls  •  Checking..."},
		{"found with path and version", rowOf(gopls, extension.RequirementResult{State: extension.RequirementFound, Path: "/usr/bin/gopls", Version: "0.20.0"}), LookFound, "gopls  •  /usr/bin/gopls 0.20.0"},
		{"found with a detail", rowOf(gopls, extension.RequirementResult{State: extension.RequirementFound, Path: "/x/gopls", Detail: "found through go.goplsPath"}), LookFound, "gopls  •  /x/gopls (found through go.goplsPath)"},
		{"found with a detail and no path", rowOf(gopls, extension.RequirementResult{State: extension.RequirementFound, Detail: "a Secret Service runs on the session bus"}), LookFound, "gopls  •  a Secret Service runs on the session bus"},
		{"a required one missing", rowOf(gopls, extension.RequirementResult{State: extension.RequirementMissing, Detail: "gopls was not found in PATH"}), LookError, "gopls  •  gopls was not found in PATH"},
		{"an optional one missing", rowOf(dlv, extension.RequirementResult{State: extension.RequirementMissing}), LookWarning, "dlv (optional)  •  missing"},
		{"unusable", rowOf(gopls, extension.RequirementResult{State: extension.RequirementUnusable, Detail: "gopls 0.9 is older than 0.12"}), LookError, "gopls  •  gopls 0.9 is older than 0.12"},
		{"unknown", rowOf(dlv, extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check took longer than 10s"}), LookUnknown, "dlv (optional)  •  the check took longer than 10s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LookOf(tt.row); got != tt.look {
				t.Errorf("LookOf = %d, want %d", got, tt.look)
			}
			if got := RequirementText(tt.row); got != tt.text {
				t.Errorf("RequirementText = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestTheHelpOfARequirement(t *testing.T) {
	gopls := extension.Requirement{ID: "gopls", Name: "gopls", Install: testInstall, PathSetting: "go.goplsPath", InstallAction: "emetos.golang.installTools"}
	chosen := extension.Install{Docs: "https://example.com/pylsp", Commands: []extension.InstallCommand{{Family: extension.FamilyDebian, Command: "sudo apt install python3-pylsp"}}}
	tests := []struct {
		name   string
		row    exthost.RequirementRow
		family string
		want   RequirementHelp
	}{
		{"found has no buttons", rowOf(gopls, extension.RequirementResult{State: extension.RequirementFound}), extension.FamilyFedora, RequirementHelp{}},
		{"unknown has no buttons", rowOf(gopls, extension.RequirementResult{State: extension.RequirementUnknown}), extension.FamilyFedora, RequirementHelp{}},
		{"checking has no buttons", exthost.RequirementRow{Requirement: gopls, Checking: true}, extension.FamilyFedora, RequirementHelp{}},
		{"missing on Fedora", rowOf(gopls, extension.RequirementResult{State: extension.RequirementMissing}), extension.FamilyFedora,
			RequirementHelp{InstallCommand: "sudo dnf install gopls", InstallAction: "emetos.golang.installTools", PathSetting: "go.goplsPath", Docs: "https://example.com/gopls"}},
		{"unusable on an unknown system takes the command for any", rowOf(gopls, extension.RequirementResult{State: extension.RequirementUnusable}), "",
			RequirementHelp{InstallCommand: "go install golang.org/x/tools/gopls@latest", InstallAction: "emetos.golang.installTools", PathSetting: "go.goplsPath", Docs: "https://example.com/gopls"}},
		{"the program the settings chose wins", rowOf(gopls, extension.RequirementResult{State: extension.RequirementMissing, Install: chosen, PathSetting: "python.pylspPath"}), extension.FamilyArch,
			RequirementHelp{InstallCommand: "sudo apt install python3-pylsp", InstallAction: "emetos.golang.installTools", PathSetting: "python.pylspPath", Docs: "https://example.com/pylsp"}},
		{"nothing to offer", rowOf(extension.Requirement{ID: "camera", Name: "Camera", Optional: true}, extension.RequirementResult{State: extension.RequirementMissing}), extension.FamilyFedora, RequirementHelp{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HelpOf(tt.row, tt.family); got != tt.want {
				t.Errorf("HelpOf = %+v, want %+v", got, tt.want)
			}
		})
	}
}
