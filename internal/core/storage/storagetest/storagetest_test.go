package storagetest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var variables = []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR"}

func TestIsolate(t *testing.T) {
	before := map[string]string{}
	for _, v := range variables {
		before[v] = os.Getenv(v)
	}

	var runtimeDir, base string
	t.Run("inside", func(t *testing.T) {
		paths := Isolate(t)
		base = filepath.Dir(paths.ConfigHome)
		runtimeDir = paths.RuntimeDir

		for name, got := range map[string]string{
			"ConfigHome": paths.ConfigHome, "StateHome": paths.StateHome,
			"CacheHome": paths.CacheHome, "DataHome": paths.DataHome,
		} {
			if !strings.HasPrefix(got, base+string(filepath.Separator)) {
				t.Errorf("%s = %q, want it under %q", name, got, base)
			}
			if st, err := os.Stat(got); err != nil || !st.IsDir() {
				t.Errorf("%s = %q is not a folder (%v)", name, got, err)
			}
		}
		if home := os.Getenv("HOME"); !strings.HasPrefix(home, base+string(filepath.Separator)) {
			t.Errorf("HOME = %q, want it under %q", home, base)
		}
		st, err := os.Stat(paths.RuntimeDir)
		if err != nil || !st.IsDir() {
			t.Fatalf("RuntimeDir %q is not a folder (%v)", paths.RuntimeDir, err)
		}
		if mode := st.Mode().Perm(); mode != 0o700 {
			t.Errorf("RuntimeDir mode = %o, want 700", mode)
		}
		// A socket path must fit in a sockaddr_un (108 bytes).
		if n := len(paths.SocketPath(1000)); n > 60 {
			t.Errorf("socket path is %d bytes long, want a short one: %s", n, paths.SocketPath(1000))
		}
		for _, v := range variables {
			if os.Getenv(v) == before[v] && before[v] != "" {
				t.Errorf("%s still has its old value %q", v, before[v])
			}
		}
	})

	for _, v := range variables {
		if got := os.Getenv(v); got != before[v] {
			t.Errorf("%s = %q after the test, want it restored to %q", v, got, before[v])
		}
	}
	if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
		t.Errorf("runtime folder %q was not removed (%v)", runtimeDir, err)
	}
	if _, err := os.Stat(base); !os.IsNotExist(err) {
		t.Errorf("test folder %q was not removed (%v)", base, err)
	}
}
