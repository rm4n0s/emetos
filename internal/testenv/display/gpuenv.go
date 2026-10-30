package display

import (
	"os"
	"path/filepath"
	"strings"
)

// GPUEnv is the environment that keeps an editor of the tests off the NVIDIA card (the user, 2026-10-30: "dont use my nvidia
// card to test"), as scripts/gpu-env.sh does for the scripts: EGL and GLX load Mesa only (glvnd would try NVIDIA's vendor
// library first), and the Vulkan loader sees every driver but NVIDIA's.
func GPUEnv() []string {
	env := []string{"__GLX_VENDOR_LIBRARY_NAME=mesa"}
	if mesa := "/usr/share/glvnd/egl_vendor.d/50_mesa.json"; fileExists(mesa) {
		env = append(env, "__EGL_VENDOR_LIBRARY_FILENAMES="+mesa)
	}
	var icds []string
	for _, dir := range []string{"/usr/share/vulkan/icd.d", "/etc/vulkan/icd.d"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			continue // the pattern is fixed and valid; Glob fails only on a bad pattern
		}
		for _, f := range files {
			if !strings.Contains(filepath.Base(f), "nvidia") {
				icds = append(icds, f)
			}
		}
	}
	if len(icds) > 0 {
		list := strings.Join(icds, string(os.PathListSeparator))
		env = append(env, "VK_DRIVER_FILES="+list, "VK_ICD_FILENAMES="+list)
	}
	return env
}

// fileExists is whether a regular file is at the path.
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular()
}
