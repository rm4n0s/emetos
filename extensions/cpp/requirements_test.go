package cpp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheRequirementsOfCpp(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	var ctest extension.Requirement
	var ids []string
	for _, r := range m.Requirements {
		ids = append(ids, r.ID)
		if r.ID == RequirementCTest {
			ctest = r
		}
	}
	if len(ids) != 3 || ids[0] != "clangd" || ids[1] != "native-debugger" || ids[2] != RequirementCTest || !ctest.Optional {
		t.Fatalf("ids %v", ids)
	}
	for _, tt := range []struct {
		name  string
		have  bool
		state extension.RequirementState
	}{{"ctest in PATH", true, extension.RequirementFound}, {"no ctest", false, extension.RequirementMissing}} {
		t.Run(tt.name, func(t *testing.T) {
			bin := t.TempDir()
			t.Setenv("PATH", bin)
			if tt.have {
				if err := os.WriteFile(filepath.Join(bin, "ctest"), []byte("#!/bin/sh\necho 'ctest version 3.31.6'\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			got := ctest.Check(context.Background(), extension.RequirementEnv{Folder: t.TempDir()})
			if got.State != tt.state || (tt.have && got.Version != "3.31.6") || (!tt.have && got.Detail != "ctest was not found in PATH") {
				t.Errorf("got %+v", got)
			}
		})
	}
}
