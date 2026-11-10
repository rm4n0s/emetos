//go:build !linux

package terminal

import (
	"os"
	"testing"
)

func TestParseLsofCwd(t *testing.T) {
	tests := []struct {
		name, out, want string
		wantOK          bool
	}{
		{"a folder", "p123\nfcwd\nn/Users/ann/my project\n", "/Users/ann/my project", true},
		{"nothing", "", "", false},
		{"no path", "p123\nfcwd\n", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseLsofCwd(tt.out)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("parseLsofCwd = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestTheProcessOfThisTestIsKnown(t *testing.T) {
	if name, ok := processName(os.Getpid()); !ok || name == "" {
		t.Errorf("processName(self) = %q, %v", name, ok)
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := processCwd(os.Getpid())
	if !ok {
		t.Skip("lsof cannot tell the folder of this process here")
	}
	if got != want {
		resolved, _ := os.Readlink(want)
		if got != resolved {
			t.Errorf("processCwd(self) = %q, want %q", got, want)
		}
	}
}
