package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

func TestMain(m *testing.M) {
	gittest.MaybeRunFakeGit()
	os.Exit(m.Run())
}

func TestTheProgramBuildsReproducibleFixtures(t *testing.T) {
	gittest.Require(t)
	for _, kind := range gittest.FixtureKinds {
		t.Run(kind, func(t *testing.T) {
			var outputs []string
			for i := 0; i < 2; i++ {
				dir := filepath.Join(t.TempDir(), kind)
				var stdout, stderr bytes.Buffer
				if status := run([]string{kind, dir, "-files", "200"}, &stdout, &stderr); status != 0 {
					t.Fatalf("status %d: %s", status, stderr.String())
				}
				fields := strings.Split(strings.TrimSpace(stdout.String()), "\t")
				if len(fields) != 4 || fields[0] != kind || fields[1] != dir {
					t.Fatalf("output = %q", stdout.String())
				}
				outputs = append(outputs, fields[2]+" "+fields[3])
				if _, err := os.Stat(filepath.Join(dir, "src", "loader.go")); err != nil {
					t.Error(err)
				}
			}
			if outputs[0] != outputs[1] {
				t.Errorf("two runs differ: %q", outputs)
			}
		})
	}
}

func TestTheProgramRefusesWhatItCannotDo(t *testing.T) {
	gittest.Require(t)
	nonEmpty := t.TempDir()
	if err := os.WriteFile(filepath.Join(nonEmpty, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		args       []string
		wantStatus int
		wantStderr string
	}{
		{"no arguments", nil, 64, "usage"},
		{"one argument", []string{"small"}, 64, "usage"},
		{"an unknown kind", []string{"huge", t.TempDir()}, 64, "usage"},
		{"an unknown flag", []string{"-nope", "small", t.TempDir()}, 64, "flag provided but not defined"},
		{"a folder that is not empty", []string{"small", nonEmpty}, 1, "is not empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if status := run(tc.args, &stdout, &stderr); status != tc.wantStatus || !strings.Contains(stderr.String(), tc.wantStderr) || stdout.Len() != 0 {
				t.Errorf("status %d, stdout %q, stderr %q", status, stdout.String(), stderr.String())
			}
		})
	}
}
