package pdf

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/pdf/poppler"
)

// scriptPoppler is a folder with a pdfinfo script that prints the version, and the other programs beside it unless alone.
func scriptPoppler(t *testing.T, version string, alone bool) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'pdfinfo version " + version + "' >&2\necho 'Copyright 2005-2024 The Poppler Developers' >&2\nexit 99\n"
	if err := os.WriteFile(filepath.Join(dir, "pdfinfo"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if !alone {
		for _, n := range []string{"pdftoppm", "pdftotext", "pdftohtml"} {
			if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

func TestTheRequirementOfPoppler(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Requirements) != 1 || m.Requirements[0].ID != RequirementPoppler || m.Requirements[0].Optional ||
		m.Requirements[0].PathSetting != poppler.PathSetting {
		t.Fatalf("requirements %+v", m.Requirements)
	}
	if c, ok := m.Requirements[0].Install.CommandFor(extension.FamilyMacOS); !ok || c.Command != "brew install poppler" {
		t.Errorf("the command of macOS %+v", c)
	}
	good := scriptPoppler(t, "24.02.0", false)
	old := scriptPoppler(t, "21.12.0", false)
	alone := scriptPoppler(t, "24.02.0", true)
	empty := t.TempDir()
	tests := []struct {
		name, path, setting string
		state               extension.RequirementState
		version, detail     string
	}{
		{"in PATH", good, "", extension.RequirementFound, "24.02.0", ""},
		{"through the setting", empty, good, extension.RequirementFound, "24.02.0", "found through pdf.popplerPath"},
		{"missing", empty, "", extension.RequirementMissing, "", "pdfinfo was not found in PATH, /opt/homebrew/bin or /usr/local/bin"},
		{"too old", old, "", extension.RequirementUnusable, "21.12.0", "Poppler 21.12.0 is older than 22.02"},
		{"a setting that is not its folder", good, empty, extension.RequirementUnusable, "",
			"pdf.popplerPath is set to " + empty + ", which is not a folder with pdfinfo"},
		{"pdfinfo alone", alone, "", extension.RequirementUnusable, "", "pdftoppm, pdftotext, pdftohtml not beside pdfinfo in " + alone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", tt.path)
			env := extension.RequirementEnv{Setting: func(key string) (any, bool) {
				if key == poppler.PathSetting && tt.setting != "" {
					return tt.setting, true
				}
				return nil, false
			}}
			got := checkPopplerIn([]string{})(t.Context(), env)
			if got.State != tt.state || got.Version != tt.version || got.Detail != tt.detail {
				t.Errorf("got %+v, want %v %q %q", got, tt.state, tt.version, tt.detail)
			}
		})
	}
	t.Run("a check that is stopped", func(t *testing.T) {
		slow := t.TempDir()
		if err := os.WriteFile(filepath.Join(slow, "pdfinfo"), []byte("#!/bin/sh\nPATH=/bin:/usr/bin exec sleep 60\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, n := range []string{"pdftoppm", "pdftotext", "pdftohtml"} {
			if err := os.WriteFile(filepath.Join(slow, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("PATH", slow)
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		got := checkPopplerIn([]string{})(ctx, extension.RequirementEnv{})
		if got.State != extension.RequirementUnknown {
			t.Errorf("got %+v", got)
		}
	})
}
