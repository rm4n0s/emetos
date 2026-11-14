package mediapreview

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/ffmpeg"
)

// scriptFFmpeg is a folder with an ffmpeg script that prints the version, and an ffprobe beside it unless probe is false.
func scriptFFmpeg(t *testing.T, version string, probe bool) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho 'ffmpeg version " + version + " Copyright (c) 2000-2024 the FFmpeg developers'\n"
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if probe {
		if err := os.WriteFile(filepath.Join(dir, "ffprobe"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTheRequirementOfFFmpeg(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Requirements) != 1 || m.Requirements[0].ID != RequirementFFmpeg || !m.Requirements[0].Optional ||
		m.Requirements[0].PathSetting != ffmpeg.PathSetting {
		t.Fatalf("requirements %+v", m.Requirements)
	}
	if c, ok := m.Requirements[0].Install.CommandFor(extension.FamilyFedora); !ok || c.Command != "sudo dnf install ffmpeg-free" {
		t.Errorf("Fedora's command %+v", c)
	}
	good := scriptFFmpeg(t, "6.1.1", true)
	old := scriptFFmpeg(t, "4.2.7", true)
	alone := scriptFFmpeg(t, "6.1.1", false)
	empty := t.TempDir()
	tests := []struct {
		name, path, setting string
		state               extension.RequirementState
		version, detail     string
	}{
		{"in PATH", good, "", extension.RequirementFound, "6.1.1", ""},
		{"through the setting", empty, filepath.Join(good, "ffmpeg"), extension.RequirementFound, "6.1.1", "found through mediaPreview.ffmpegPath"},
		{"missing", empty, "", extension.RequirementMissing, "", "ffmpeg was not found in PATH, /opt/homebrew/bin or /usr/local/bin"},
		{"too old", old, "", extension.RequirementUnusable, "4.2.7", "FFmpeg 4.2.7 is older than 4.4"},
		{"a setting that names nothing", good, filepath.Join(empty, "ffmpeg"), extension.RequirementUnusable, "",
			"mediaPreview.ffmpegPath is set to " + filepath.Join(empty, "ffmpeg") + ", which is not a program that can be run"},
		{"no ffprobe", alone, "", extension.RequirementUnusable, "", "ffprobe is not beside " + filepath.Join(alone, "ffmpeg")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", tt.path)
			env := extension.RequirementEnv{Setting: func(key string) (any, bool) {
				if key == ffmpeg.PathSetting && tt.setting != "" {
					return tt.setting, true
				}
				return nil, false
			}}
			got := checkFFmpegIn([]string{})(t.Context(), env)
			if got.State != tt.state || got.Version != tt.version || got.Detail != tt.detail {
				t.Errorf("result %+v", got)
			}
		})
	}
	// a check stopped while -version runs tells nothing
	hang := t.TempDir()
	if err := os.WriteFile(filepath.Join(hang, "ffmpeg"), []byte("#!/bin/sh\nPATH=/bin:/usr/bin exec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hang, "ffprobe"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", hang)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if got := checkFFmpegIn([]string{})(ctx, extension.RequirementEnv{}); got.State != extension.RequirementUnknown {
		t.Errorf("stopped: %+v", got)
	}
}
