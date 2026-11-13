package terminal

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheRequirementOfTheShell(t *testing.T) {
	look := func(have ...string) func(string) (string, error) {
		return func(p string) (string, error) {
			for _, h := range have {
				if h == p {
					return p, nil
				}
			}
			return "", errors.New("NotFound", "not found")
		}
	}
	tests := []struct {
		name     string
		env      environ
		settings map[string]any
		state    extension.RequirementState
		path     string
		detail   string
	}{
		{"$SHELL", environ{vars: []string{"SHELL=/usr/bin/zsh"}, lookPath: look("/usr/bin/zsh", "/bin/sh")}, nil, extension.RequirementFound, "/usr/bin/zsh", "zsh"},
		{"the default profile", environ{vars: []string{"SHELL=/usr/bin/zsh"}, lookPath: look("/usr/bin/zsh", "/usr/bin/fish")},
			map[string]any{settingDefaultProfile: "fish", settingProfiles: map[string]any{"fish": map[string]any{"path": "/usr/bin/fish"}}},
			extension.RequirementFound, "/usr/bin/fish", "fish"},
		{"a default profile whose program is missing", environ{vars: []string{"SHELL=/usr/bin/zsh"}, lookPath: look("/usr/bin/zsh")},
			map[string]any{settingDefaultProfile: "fish", settingProfiles: map[string]any{"fish": map[string]any{"path": "/usr/bin/fish"}}},
			extension.RequirementFound, "/usr/bin/zsh", "zsh (the profile fish of terminal.integrated.defaultProfile.linux was not found)"},
		{"no $SHELL: /bin/sh", environ{lookPath: look("/bin/sh")}, nil, extension.RequirementFound, "/bin/sh", "sh"},
		{"nothing at all", environ{lookPath: look()}, nil, extension.RequirementMissing, "/bin/sh", "/bin/sh cannot be run, and it is the last shell a terminal tries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := requirements(func() environ { return tt.env })[0]
			got := r.Check(context.Background(), extension.RequirementEnv{Setting: func(k string) (any, bool) { v, ok := tt.settings[k]; return v, ok }})
			if got.State != tt.state || got.Path != tt.path || got.Detail != tt.detail {
				t.Errorf("got %+v", got)
			}
		})
	}
	if err := extension.CheckManifest(New().Manifest()); err != nil {
		t.Fatal(err)
	}
}
