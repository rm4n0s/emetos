package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

const plistHead = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">`

func TestRun(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "a.tmLanguage")
	out := filepath.Join(dir, "a.json")
	if err := os.WriteFile(in, []byte(plistHead+`<dict><key>name</key><string>x</string></dict></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run(in, out); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(out); err != nil || len(data) == 0 {
		t.Errorf("the output is not written: %v", err)
	}

	cases := []struct {
		name, in, out, route string
	}{
		{"no input", filepath.Join(dir, "missing"), out, "run.PlistReadFailed"},
		{"no folder for the output", in, filepath.Join(dir, "none", "a.json"), "run.PlistWriteFailed"},
		{"input that is not a plist", out, out + "2", "run->Parse.PlistInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.in, tc.out)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tc.route)
			}
		})
	}
}
