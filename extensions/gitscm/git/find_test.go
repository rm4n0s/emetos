package git

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
)

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in        string
		want      Version
		wantRoute string
	}{
		{"git version 2.30.0", Version{2, 30, 0}, ""},
		{"git version 2.30", Version{2, 30, 0}, ""},
		{"2.30", Version{2, 30, 0}, ""},
		{"git version 2.43.0.windows.1", Version{2, 43, 0}, ""},
		{"git version 2.56.0.rc1", Version{2, 56, 0}, ""},
		{"git version 2.30.1 (Apple Git-130)", Version{2, 30, 1}, ""},
		{"git version 2.39.3 (Apple Git-146)\n", Version{2, 39, 3}, ""},
		{"  git version 10.2.14  ", Version{10, 2, 14}, ""},
		{"git version 1.9.5", Version{1, 9, 5}, ""},
		{"", Version{}, "ParseVersion.GitVersionUnreadable"},
		{"git: command not found", Version{}, "ParseVersion.GitVersionUnreadable"},
		{"git version x.y", Version{}, "ParseVersion.GitVersionUnreadable"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseVersion(tc.in)
			if tc.wantRoute != "" {
				wantRoute(t, err, tc.wantRoute)
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("ParseVersion(%q) = %v, %v; want %v", tc.in, got, err, tc.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"2.30", "2.30.0", 0},
		{"2.29.9", "2.30", -1},
		{"2.30.1", "2.30", 1},
		{"2.43.0.windows.1", "2.43.0", 0},
		{"2.56.0.rc1", "2.55.9", 1},
		{"3.0", "2.99.99", 1},
		{"1.9.5", "2.30", -1},
	} {
		a, _ := ParseVersion(tc.a)
		b, _ := ParseVersion(tc.b)
		if got := a.Compare(b); got != tc.want {
			t.Errorf("%s vs %s = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	v, _ := ParseVersion("2.35.1")
	if !v.AtLeast(2, 35) || v.AtLeast(2, 36) || !v.AtLeast(2, 30) || v.String() != "2.35.1" {
		t.Errorf("AtLeast or String: %v", v)
	}
	if MinVersion.String() != "2.30.0" {
		t.Errorf("the minimum is %s", MinVersion)
	}
}

func TestFind(t *testing.T) {
	fake := func(t *testing.T, script string) (path string, env []string) {
		p, e := gittest.FakeGit(t, script)
		return p, []string{"PATH=" + os.Getenv("PATH"), e}
	}
	ctx := context.Background()

	t.Run("the first usable candidate wins, and its version is read", func(t *testing.T) {
		path, env := fake(t, "version 2.43.0.windows.1")
		inst, err := Find(ctx, []string{path}, FindOptions{Env: env})
		if err != nil || inst.Path != path || inst.Version != (Version{2, 43, 0}) || inst.Raw != "git version 2.43.0.windows.1" {
			t.Errorf("got %+v, %v", inst, err)
		}
	})
	t.Run("a candidate that is missing is skipped and logged", func(t *testing.T) {
		path, env := fake(t, "version 2.31.0")
		var logged []string
		inst, err := Find(ctx, []string{gittest.MissingGit(t), path}, FindOptions{Env: env, Log: func(l string) { logged = append(logged, l) }})
		if err != nil || inst.Path != path {
			t.Fatalf("got %+v, %v", inst, err)
		}
		if len(logged) != 1 || !strings.Contains(logged[0], "Unable to find git") {
			t.Errorf("log = %q", logged)
		}
	})
	t.Run("the minimum is 2.30", func(t *testing.T) {
		path, env := fake(t, "version 2.30.0")
		if _, err := Find(ctx, []string{path}, FindOptions{Env: env}); err != nil {
			t.Errorf("2.30.0: %v", err)
		}
	})
	t.Run("a git older than 2.30 is GitTooOld with the path and the version", func(t *testing.T) {
		path, env := fake(t, "version 2.29.3")
		t.Setenv("PATH", t.TempDir())          // no other git
		env = append(env, "PATH="+t.TempDir()) // the PATH of the commands decides too
		_, err := Find(ctx, []string{path}, FindOptions{Env: env, Log: nil})
		appErr := routeOf(t, err, "Find.GitTooOld")
		if appErr != nil && (appErr.Metadata["path"] != path || appErr.Metadata["version"] != "2.29.3" || appErr.Metadata["minimum"] != "2.30.0") {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})
	t.Run("an old candidate does not hide a good one", func(t *testing.T) {
		gittest.Require(t)
		old, env := fake(t, "version 2.20.0")
		inst, err := Find(ctx, []string{old}, FindOptions{Env: env}) // the real git on the PATH is the next candidate
		if err != nil || inst.Path == old || inst.Version.Compare(MinVersion) < 0 {
			t.Errorf("got %+v, %v", inst, err)
		}
	})
	t.Run("no candidate is GitNotFound with the candidates tried", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir()) // no git on the path
		missing := gittest.MissingGit(t)
		_, err := Find(ctx, []string{missing}, FindOptions{})
		appErr := routeOf(t, err, "Find.GitNotFound")
		tried, _ := appErr.Metadata["candidates"].([]string)
		if appErr != nil && (len(tried) != 2 || tried[0] != missing || tried[1] != "git") {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})
	t.Run("a program that does not answer --version", func(t *testing.T) {
		path, env := fake(t, "stderr \"boom\"\nexit 3")
		t.Setenv("PATH", t.TempDir())
		env = append(env, "PATH="+t.TempDir()) // the PATH of the commands decides too
		_, err := Find(ctx, []string{path}, FindOptions{Env: env})
		wantRoute(t, err, "Find.GitNotFound") // the failure of the probe is logged and the candidate skipped
	})
	t.Run("the probe itself reports its failure", func(t *testing.T) {
		path, env := fake(t, "exit 3")
		_, err := probe(ctx, path, env)
		wantRoute(t, err, "probe.GitVersionFailed")
	})
	t.Run("the probe reports a version it cannot read", func(t *testing.T) {
		path, env := fake(t, "stdout \"hello\\n\"")
		_, err := probe(ctx, path, env)
		wantRoute(t, err, "probe->ParseVersion.GitVersionUnreadable")
	})
	t.Run("the real git is found on the path", func(t *testing.T) {
		gittest.Require(t)
		inst, err := Find(ctx, nil, FindOptions{})
		if err != nil || inst.Version.Compare(MinVersion) < 0 || !strings.HasPrefix(inst.Raw, "git version") {
			t.Errorf("got %+v, %v", inst, err)
		}
	})
	t.Run("git --version runs outside any repository", func(t *testing.T) {
		rec := t.TempDir() + "/calls"
		path, env := fake(t, "record "+rec+"\nversion 2.40.0")
		if _, err := Find(ctx, []string{path}, FindOptions{Env: env}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(rec)
		if err != nil || !strings.Contains(string(data), `"dir":"/"`) || !strings.Contains(string(data), `"args":["--version"]`) {
			t.Errorf("record = %q, %v", data, err)
		}
	})
}
