package java

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheRequirementOfTheJavaRuntime(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	var java extension.Requirement
	for _, r := range m.Requirements {
		if r.ID == RequirementJava {
			java = r
		}
	}
	if java.Check == nil || java.PathSetting != JavaHomeSetting {
		t.Fatalf("requirement %+v", java)
	}
	javaScript := func(version string) string {
		return "#!/bin/sh\necho 'openjdk version \"" + version + "\" 2025-10-21' >&2\n"
	}
	tests := []struct {
		name    string
		setup   func(t *testing.T, bin string) map[string]any
		state   extension.RequirementState
		version string
		detail  string
	}{
		{"Java 21 in PATH", func(t *testing.T, bin string) map[string]any {
			writeScript(t, filepath.Join(bin, "java"), javaScript("21.0.9"))
			return nil
		}, extension.RequirementFound, "21", "found through PATH"},
		{"Java 17 is too old", func(t *testing.T, bin string) map[string]any {
			writeScript(t, filepath.Join(bin, "java"), javaScript("17.0.12"))
			return nil
		}, extension.RequirementUnusable, "17", ""},
		{"java.jdt.ls.java.home with a JDK", func(t *testing.T, _ string) map[string]any {
			jdk := t.TempDir()
			writeScript(t, filepath.Join(jdk, "bin", "java"), javaScript("25"))
			return map[string]any{JavaHomeSetting: jdk}
		}, extension.RequirementFound, "25", "found through java.jdt.ls.java.home"},
		{"a java home with no java", func(t *testing.T, _ string) map[string]any {
			return map[string]any{JavaHomeSetting: t.TempDir()}
		}, extension.RequirementUnusable, "", ""},
		{"no java", func(*testing.T, string) map[string]any { return nil }, extension.RequirementMissing, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			t.Setenv("JAVA_HOME", "")
			settings := tt.setup(t, bin)
			got := java.Check(context.Background(), extension.RequirementEnv{Folder: t.TempDir(), Setting: func(k string) (any, bool) { v, ok := settings[k]; return v, ok }})
			if got.State != tt.state || got.Version != tt.version || (tt.detail != "" && got.Detail != tt.detail) {
				t.Errorf("got %+v, want %s %q %q", got, tt.state, tt.version, tt.detail)
			}
		})
	}
}

func writeScript(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
}
