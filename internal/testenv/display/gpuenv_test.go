package display

import (
	"strings"
	"testing"
)

// The environment of an editor of the tests names no NVIDIA library or driver, and asks glvnd for Mesa only.
func TestGPUEnvLeavesTheNVIDIACardOut(t *testing.T) {
	env := map[string]string{}
	for _, kv := range GPUEnv() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("not KEY=VALUE: %q", kv)
		}
		env[k] = v
	}
	tests := []struct {
		key  string
		want func(v string) bool
		desc string
	}{
		{"__GLX_VENDOR_LIBRARY_NAME", func(v string) bool { return v == "mesa" }, "mesa"},
		{"__EGL_VENDOR_LIBRARY_FILENAMES", func(v string) bool { return v == "" || strings.HasSuffix(v, "50_mesa.json") }, "Mesa's vendor file, when it is installed"},
		{"VK_DRIVER_FILES", func(v string) bool { return !strings.Contains(strings.ToLower(v), "nvidia") }, "no NVIDIA driver"},
		{"VK_ICD_FILENAMES", func(v string) bool { return v == env["VK_DRIVER_FILES"] }, "the same list as VK_DRIVER_FILES"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if v := env[tt.key]; !tt.want(v) {
				t.Errorf("%s = %q, want %s", tt.key, v, tt.desc)
			}
		})
	}
}
