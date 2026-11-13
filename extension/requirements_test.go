package extension

import (
	"context"
	"runtime"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestCheckManifestRequirements(t *testing.T) {
	check := func(context.Context, RequirementEnv) RequirementResult {
		return RequirementResult{State: RequirementFound}
	}
	good := Requirement{ID: "gopls", Name: "gopls", Kind: RequirementProgram, Purpose: "the language features of Go", Check: check,
		Install: Install{Commands: []InstallCommand{{Family: FamilyAny, Command: "go install golang.org/x/tools/gopls@latest"}, {Family: FamilyMacOS, Command: "brew install gopls"}}}}
	with := func(f func(r *Requirement)) Requirement { r := good; f(&r); return r }
	tests := []struct {
		name  string
		reqs  []Requirement
		route string
	}{
		{"a program, a service and a device", []Requirement{good, with(func(r *Requirement) { r.ID, r.Kind = "docker-engine", RequirementService }), with(func(r *Requirement) { r.ID, r.Kind, r.Optional = "camera", RequirementDevice, true })}, ""},
		{"an id that is not lowercase", []Requirement{with(func(r *Requirement) { r.ID = "Gopls" })}, "checkRequirements.ExtensionRequirementInvalid"},
		{"no id", []Requirement{with(func(r *Requirement) { r.ID = "" })}, "checkRequirements.ExtensionRequirementInvalid"},
		{"two with the same id", []Requirement{good, good}, "checkRequirements.ExtensionRequirementDuplicate"},
		{"no name", []Requirement{with(func(r *Requirement) { r.Name = " " })}, "checkRequirements.ExtensionRequirementInvalid"},
		{"an unknown kind", []Requirement{with(func(r *Requirement) { r.Kind = "feeling" })}, "checkRequirements.ExtensionRequirementInvalid"},
		{"no purpose", []Requirement{with(func(r *Requirement) { r.Purpose = "" })}, "checkRequirements.ExtensionRequirementInvalid"},
		{"no check", []Requirement{with(func(r *Requirement) { r.Check = nil })}, "checkRequirements.ExtensionRequirementInvalid"},
		{"an unknown family", []Requirement{with(func(r *Requirement) {
			r.Install.Commands = []InstallCommand{{Family: "gentoo", Command: "emerge gopls"}}
		})}, "checkRequirements.ExtensionRequirementInvalid"},
		{"an empty command", []Requirement{with(func(r *Requirement) { r.Install.Commands = []InstallCommand{{Family: FamilyAny}} })}, "checkRequirements.ExtensionRequirementInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckManifest(Manifest{ID: "a", Name: "N", Requirements: tt.reqs})
			if tt.route == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("CheckManifest->" + tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestOSFamily(t *testing.T) {
	tests := []struct{ name, text, want string }{
		{"Fedora", "NAME=\"Fedora Linux\"\nID=fedora\nVERSION_ID=45\n", FamilyFedora},
		{"Rocky by ID_LIKE", "ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", FamilyFedora},
		{"Ubuntu", "ID=ubuntu\nID_LIKE=debian\n", FamilyDebian},
		{"Arch", "ID=arch\n", FamilyArch},
		{"openSUSE Tumbleweed", "ID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n", FamilySuse},
		{"an unknown system", "ID=plan9\n", ""},
		{"nothing", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OSFamily(tt.text); got != tt.want {
				t.Errorf("OSFamily = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSystemFamilyOfTheRunningSystem(t *testing.T) {
	got := SystemFamily()
	if runtime.GOOS == "darwin" {
		if got != FamilyMacOS {
			t.Errorf("SystemFamily on macOS = %q", got)
		}
		return
	}
	if want := OSFamily(ReadOSRelease()); got != want {
		t.Errorf("SystemFamily = %q, want %q", got, want)
	}
}

func TestInstallCommandFor(t *testing.T) {
	in := Install{Commands: []InstallCommand{{Family: FamilyFedora, Command: "dnf"}, {Family: FamilyAny, Command: "any"}, {Family: FamilyMacOS, Command: "brew"}}}
	tests := []struct {
		name, family, want string
		install            Install
		ok                 bool
	}{
		{"the family's", FamilyFedora, "dnf", in, true},
		{"macOS", FamilyMacOS, "brew", in, true},
		{"any for another family", FamilyArch, "any", in, true},
		{"any for no family", "", "any", in, true},
		{"the first without any", FamilyArch, "dnf", Install{Commands: in.Commands[:1]}, true},
		{"none", FamilyArch, "", Install{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, ok := tt.install.CommandFor(tt.family)
			if ok != tt.ok || c.Command != tt.want {
				t.Errorf("CommandFor = %q, %v; want %q, %v", c.Command, ok, tt.want, tt.ok)
			}
		})
	}
	if !(Install{}).Empty() || in.Empty() || (Install{Docs: "x"}).Empty() {
		t.Error("Empty is wrong")
	}
}

func TestRequirementEnvStringSetting(t *testing.T) {
	env := RequirementEnv{Setting: func(key string) (any, bool) {
		switch key {
		case "s":
			return "  /usr/bin/gopls ", true
		case "n":
			return 3.0, true
		}
		return nil, false
	}}
	if got := env.StringSetting("s"); got != "/usr/bin/gopls" {
		t.Errorf("s = %q", got)
	}
	if got := env.StringSetting("n"); got != "" {
		t.Errorf("n = %q", got)
	}
	if got := (RequirementEnv{}).StringSetting("s"); got != "" {
		t.Errorf("no reader = %q", got)
	}
}
