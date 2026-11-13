package exthost

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// checker is a fake check that counts its calls and answers with the folder it was given.
type checker struct {
	calls atomic.Int32
	fn    func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult
}

func (c *checker) check(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
	c.calls.Add(1)
	if c.fn != nil {
		return c.fn(ctx, env)
	}
	return extension.RequirementResult{State: extension.RequirementFound, Path: env.Folder}
}

func req(id string, c *checker, pathSetting string) extension.Requirement {
	return extension.Requirement{ID: id, Name: id, Kind: extension.RequirementProgram, Purpose: "p", PathSetting: pathSetting, Check: c.check}
}

// settled waits until no row is checking and every row has a result.
func settled(t *testing.T, r *Requirements) []RequirementRow {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows := r.Rows()
		done := true
		for _, row := range rows {
			if row.Checking || !row.Checked() {
				done = false
			}
		}
		if done {
			return rows
		}
		if time.Now().After(deadline) {
			t.Fatalf("the checks did not end: %+v", rows)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRequirementsAreCheckedOnDemandAndKeptPerFolder(t *testing.T) {
	a, b := &checker{}, &checker{}
	r := NewRequirements(RequirementsOptions{Extensions: []extension.Manifest{
		{ID: "emetos.one", Name: "One", Requirements: []extension.Requirement{req("a", a, ""), req("b", b, "one.bPath")}},
		{ID: "emetos.none", Name: "None"},
	}})
	defer r.Close()
	var told atomic.Int32
	stop := r.OnChange(func() { told.Add(1) })
	defer stop()
	r.SetSource("/work/x", nil)
	time.Sleep(20 * time.Millisecond)
	if a.calls.Load() != 0 || b.calls.Load() != 0 {
		t.Fatal("a requirement was checked before a page asked")
	}
	rows := r.Rows()
	if len(rows) != 2 || rows[0].ExtensionID != "emetos.one" || rows[0].ExtensionName != "One" || rows[1].Requirement.ID != "b" || rows[0].Checked() {
		t.Fatalf("rows %+v", rows)
	}
	r.Start()
	rows = settled(t, r)
	if rows[0].Result.Path != "/work/x" || a.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Errorf("rows %+v, calls %d %d", rows, a.calls.Load(), b.calls.Load())
	}
	if told.Load() == 0 {
		t.Error("the listeners were not told")
	}
	r.Start() // a second page: nothing new
	r.SetSource("/work/y", nil)
	rows = settled(t, r)
	if rows[0].Result.Path != "/work/y" || a.calls.Load() != 2 {
		t.Errorf("another folder: rows %+v, calls %d", rows, a.calls.Load())
	}
	r.SetSource("/work/x", nil)
	rows = settled(t, r)
	if rows[0].Result.Path != "/work/x" || a.calls.Load() != 2 {
		t.Errorf("back to the first folder: rows %+v, calls %d", rows, a.calls.Load())
	}
	r.CheckAgain()
	settled(t, r)
	if a.calls.Load() != 3 || b.calls.Load() != 3 {
		t.Errorf("Check Again: calls %d %d", a.calls.Load(), b.calls.Load())
	}
	r.SettingsChanged([]string{"editor.tabSize", "one.bPath"})
	settled(t, r)
	if a.calls.Load() != 3 || b.calls.Load() != 4 {
		t.Errorf("a path setting changed: calls %d %d", a.calls.Load(), b.calls.Load())
	}
}

func TestRequirementsBeforeStartIgnoreSettingChanges(t *testing.T) {
	c := &checker{}
	r := NewRequirements(RequirementsOptions{Extensions: []extension.Manifest{{ID: "e", Name: "E", Requirements: []extension.Requirement{req("a", c, "e.path")}}}})
	defer r.Close()
	r.SettingsChanged([]string{"e.path"})
	time.Sleep(20 * time.Millisecond)
	if c.calls.Load() != 0 {
		t.Error("a setting change checked before a page asked")
	}
}

func TestRequirementsAResultNamesThePathSettingToFollow(t *testing.T) {
	c := &checker{fn: func(context.Context, extension.RequirementEnv) extension.RequirementResult {
		return extension.RequirementResult{State: extension.RequirementMissing, PathSetting: "pylsp.path"}
	}}
	r := NewRequirements(RequirementsOptions{Extensions: []extension.Manifest{{ID: "e", Name: "E", Requirements: []extension.Requirement{req("a", c, "basedpyright.path")}}}})
	defer r.Close()
	r.Start()
	settled(t, r)
	r.SettingsChanged([]string{"pylsp.path"})
	settled(t, r)
	if c.calls.Load() != 2 {
		t.Errorf("calls %d", c.calls.Load())
	}
}

func TestRequirementsRunAFewAtATime(t *testing.T) {
	var running, most atomic.Int32
	release := make(chan struct{})
	c := &checker{fn: func(ctx context.Context, _ extension.RequirementEnv) extension.RequirementResult {
		n := running.Add(1)
		for {
			m := most.Load()
			if n <= m || most.CompareAndSwap(m, n) {
				break
			}
		}
		<-release
		running.Add(-1)
		return extension.RequirementResult{State: extension.RequirementFound}
	}}
	var reqs []extension.Requirement
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		reqs = append(reqs, req(id, c, ""))
	}
	r := NewRequirements(RequirementsOptions{Limit: 3, Extensions: []extension.Manifest{{ID: "e", Name: "E", Requirements: reqs}}})
	defer r.Close()
	r.Start()
	time.Sleep(50 * time.Millisecond)
	if got := running.Load(); got != 3 {
		t.Errorf("%d checks run at once, want 3", got)
	}
	close(release)
	settled(t, r)
	if most.Load() != 3 || c.calls.Load() != 7 {
		t.Errorf("at most %d at once, %d calls", most.Load(), c.calls.Load())
	}
}

func TestRequirementsDefaults(t *testing.T) {
	r := NewRequirements(RequirementsOptions{})
	defer r.Close()
	if cap(r.sem) != DefaultRequirementChecks || r.timeout != DefaultRequirementTimeout || DefaultRequirementTimeout != 10*time.Second || DefaultRequirementChecks != 4 {
		t.Errorf("limit %d, timeout %s", cap(r.sem), r.timeout)
	}
}

func TestRequirementsAnUnusualCheck(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult
		detail string
	}{
		{"one that takes too long", func(ctx context.Context, _ extension.RequirementEnv) extension.RequirementResult {
			<-ctx.Done()
			return extension.RequirementResult{State: extension.RequirementFound}
		}, "the check took longer than 50ms"},
		{"one that never ends", func(context.Context, extension.RequirementEnv) extension.RequirementResult {
			time.Sleep(300 * time.Millisecond)
			return extension.RequirementResult{State: extension.RequirementFound}
		}, "the check took longer than 50ms"},
		{"one that panics", func(context.Context, extension.RequirementEnv) extension.RequirementResult { panic("boom") }, "the check failed"},
		{"one with no state", func(context.Context, extension.RequirementEnv) extension.RequirementResult {
			return extension.RequirementResult{Detail: "?"}
		}, "?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var log bytes.Buffer
			r := NewRequirements(RequirementsOptions{Timeout: 50 * time.Millisecond, Logger: slog.New(slog.NewJSONHandler(&log, nil)),
				Extensions: []extension.Manifest{{ID: "e", Name: "E", Requirements: []extension.Requirement{req("a", &checker{fn: tt.fn}, "")}}}})
			defer r.Close()
			r.Start()
			rows := settled(t, r)
			if rows[0].Result.State != extension.RequirementUnknown || rows[0].Result.Detail != tt.detail {
				t.Errorf("result %+v", rows[0].Result)
			}
			if tt.name == "one that panics" {
				var entry struct {
					Err errors.ErrorJson `json:"err"`
				}
				if err := json.Unmarshal(log.Bytes(), &entry); err != nil {
					t.Fatalf("log %q: %v", log.String(), err)
				}
				appErr := errors.FromJson(entry.Err)
				if !appErr.HasRoute("Requirements.run") || appErr.Tag != "ExtensionRequirementPanicked" || appErr.Metadata["panic"] != "boom" {
					t.Errorf("unexpected failure path: %s %v", appErr.Route(), appErr.Metadata)
				}
			}
		})
	}
}

func TestRequirementsCloseStopsTheChecksThatRun(t *testing.T) {
	var stopped sync.WaitGroup
	stopped.Add(1)
	started := make(chan struct{})
	c := &checker{fn: func(ctx context.Context, _ extension.RequirementEnv) extension.RequirementResult {
		close(started)
		<-ctx.Done()
		stopped.Done()
		return extension.RequirementResult{State: extension.RequirementFound}
	}}
	r := NewRequirements(RequirementsOptions{Extensions: []extension.Manifest{{ID: "e", Name: "E", Requirements: []extension.Requirement{req("a", c, "")}}}})
	r.Start()
	<-started
	done := make(chan struct{})
	go func() { r.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
	stopped.Wait()
	r.Start()
	r.CheckAgain()
	if c.calls.Load() != 1 {
		t.Errorf("checked after Close: %d calls", c.calls.Load())
	}
}

func TestRequirementsANewFolderStopsTheChecksOfTheOld(t *testing.T) {
	var cancelled atomic.Int32
	c := &checker{fn: func(ctx context.Context, env extension.RequirementEnv) extension.RequirementResult {
		if env.Folder == "/old" {
			<-ctx.Done()
			cancelled.Add(1)
		}
		return extension.RequirementResult{State: extension.RequirementFound, Path: env.Folder}
	}}
	r := NewRequirements(RequirementsOptions{Extensions: []extension.Manifest{{ID: "e", Name: "E", Requirements: []extension.Requirement{req("a", c, "")}}}})
	defer r.Close()
	r.SetSource("/old", nil)
	r.Start()
	time.Sleep(20 * time.Millisecond)
	r.SetSource("/new", nil)
	rows := settled(t, r)
	if rows[0].Result.Path != "/new" {
		t.Errorf("result %+v", rows[0].Result)
	}
	deadline := time.Now().Add(5 * time.Second)
	for cancelled.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if cancelled.Load() != 1 {
		t.Error("the check of the old folder was not stopped")
	}
}

func TestRequirementsCallTheListenersThroughPost(t *testing.T) {
	var posted atomic.Int32
	r := NewRequirements(RequirementsOptions{Post: func(f func()) { posted.Add(1); f() },
		Extensions: []extension.Manifest{{ID: "e", Name: "E", Requirements: []extension.Requirement{req("a", &checker{}, "")}}}})
	defer r.Close()
	var told atomic.Int32
	stop := r.OnChange(func() { told.Add(1) })
	r.Start()
	settled(t, r)
	if posted.Load() == 0 || told.Load() == 0 {
		t.Errorf("posted %d, told %d", posted.Load(), told.Load())
	}
	stop()
	n := told.Load()
	r.CheckAgain()
	settled(t, r)
	if told.Load() != n {
		t.Error("a listener that stopped was told")
	}
}

func TestSettingReaderKeepsAFolderFromChoosingAProgram(t *testing.T) {
	dir := t.TempDir()
	user, work := filepath.Join(dir, "user.json"), filepath.Join(dir, "work.json")
	write := func(p, text string) {
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(user, `{"a.registered": "u", "free.path": "/user/gopls", "only.user": 3}`)
	write(work, `{"a.registered": "w", "free.path": "/folder/evil", "only.work": true}`)
	reg := settings.NewRegistry()
	if err := reg.Register(settings.Setting{Key: "a.registered", Types: []settings.Type{settings.String}, Default: "", Scope: settings.ScopeWindow}); err != nil {
		t.Fatal(err)
	}
	st := settings.NewStore(reg, settings.StoreOptions{UserPath: user, WorkspacePath: work})
	if _, err := st.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	read := SettingReader(st)
	tests := []struct {
		key  string
		want any
		ok   bool
	}{
		{"a.registered", "w", true},
		{"free.path", "/user/gopls", true},
		{"only.user", 3.0, true},
		{"only.work", nil, false},
		{"nobody", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			v, ok := read(tt.key)
			if v != tt.want || ok != tt.ok {
				t.Errorf("%s = %v, %v", tt.key, v, ok)
			}
		})
	}
	if v, ok := SettingReader(nil)("a.registered"); v != nil || ok {
		t.Error("nil settings gave something")
	}
}
