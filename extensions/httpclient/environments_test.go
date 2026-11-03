package httpclient

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

func (r *rig) active() string {
	raw, ok := r.api.State().Get(StateActiveEnvironment)
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		r.t.Fatal(err)
	}
	return s
}

func (r *rig) statusText() string {
	it := r.api.StatusItem(StatusEnvironmentID)
	if it == nil {
		r.t.Fatal("no status item")
	}
	return it.Text
}

func TestTheStatusItemNamesTheActiveEnvironment(t *testing.T) {
	r := newRig(t)
	it := r.api.StatusItem(StatusEnvironmentID)
	if it.Hidden || it.Command != CmdSelectEnvironment || it.Text != "$(globe) HTTP: No Environment" {
		t.Fatalf("item = %+v", it)
	}
	r.run(CmdSelectEnvironment, "dev")
	if got := r.statusText(); got != "$(globe) HTTP: Dev" {
		t.Errorf("text = %q", got)
	}
	if r.active() != "dev" {
		t.Errorf("state = %q", r.active())
	}
}

func TestSelectingAnEnvironmentByArgument(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   any
		want   string
		warned bool
	}{
		{"by key", "staging", "staging", false},
		{"by name, in any case", "STAGING", "staging", false},
		{"by a field", map[string]any{"environment": "Dev"}, "dev", false},
		{"by key in a field", map[string]any{"key": "dev"}, "dev", false},
		{"none", map[string]any{"none": true}, "", false},
		{"unknown", "production", "dev", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.run(CmdSelectEnvironment, "dev")
			r.run(CmdSelectEnvironment, tc.args)
			if r.active() != tc.want {
				t.Errorf("active = %q, want %q", r.active(), tc.want)
			}
			if warned := len(r.notes()) > 0; warned != tc.warned {
				t.Errorf("notes = %v", r.notes())
			}
		})
	}
}

func TestThePickerListsTheEnvironmentsAndMarksTheActiveOne(t *testing.T) {
	r := newRig(t)
	r.run(CmdSelectEnvironment, "staging")
	r.run(CmdSelectEnvironment, nil)
	p := r.api.LastPick()
	labels := p.Labels()
	if len(labels) != 3 || !strings.Contains(labels[0], "No Environment") || !strings.Contains(labels[1], "Dev") || !strings.Contains(labels[2], "Staging") {
		t.Fatalf("labels = %v", labels)
	}
	items := p.Options().Items
	if !strings.HasPrefix(items[2].Description, "active") || items[0].Description != "" || items[1].Description != "3 variables" {
		t.Errorf("descriptions = %q, %q, %q", items[0].Description, items[1].Description, items[2].Description)
	}
	r.choose("Dev")
	if r.active() != "dev" || r.statusText() != "$(globe) HTTP: Dev" {
		t.Errorf("active %q, text %q", r.active(), r.statusText())
	}
	r.run(CmdSelectEnvironment, nil)
	r.choose("No Environment")
	if r.active() != "" || r.statusText() != "$(globe) HTTP: No Environment" {
		t.Errorf("active %q, text %q", r.active(), r.statusText())
	}
}

func TestABrokenEnvironmentIsStillListed(t *testing.T) {
	r := newRig(t, func(folder string) {
		writeFile(t, folder+"/.emetos/http/environments/dev.jsonc", "{")
	})
	// a file that cannot be read is still an environment in the list
	r.run(CmdSelectEnvironment, nil)
	if got := len(r.api.LastPick().Labels()); got != 3 {
		t.Errorf("%d items", got)
	}
}

func TestAnEnvironmentThatWasDeletedIsNoLongerActive(t *testing.T) {
	r := newRig(t)
	r.run(CmdSelectEnvironment, "dev")
	if err := removeFile(r.env("dev")); err != nil {
		t.Fatal(err)
	}
	r.run(CmdRefresh, nil)
	if got := r.statusText(); got != "$(globe) HTTP: No Environment" {
		t.Errorf("text = %q", got)
	}
	if r.ext.activeEnvironment() != nil {
		t.Error("a deleted environment is active")
	}
}

func TestRenamingTheActiveEnvironmentKeepsItActiveAndKeepsItsCurrentValues(t *testing.T) {
	r := newRig(t)
	r.run(CmdSelectEnvironment, "dev")
	if err := r.ext.values.Set(context.Background(), variables.EnvironmentOwner("dev"), "baseUrl", "http://127.0.0.1:9", false); err != nil {
		t.Fatal(err)
	}
	r.run(CmdRename, r.env("dev"))
	r.answer("Development")
	if r.active() != "development" {
		t.Errorf("active = %q", r.active())
	}
	if got, ok := r.ext.values.Get(variables.EnvironmentOwner("development"), "baseUrl"); !ok || got != "http://127.0.0.1:9" {
		t.Errorf("current value = %q, %v", got, ok)
	}
	if _, ok := r.ext.values.Get(variables.EnvironmentOwner("dev"), "baseUrl"); ok {
		t.Error("the old owner still has the value")
	}
	if got := r.statusText(); got != "$(globe) HTTP: Development" {
		t.Errorf("text = %q", got)
	}
}

func TestTheEnvironmentsViewMarksTheActiveRow(t *testing.T) {
	r := newRig(t)
	r.run(CmdSelectEnvironment, "staging")
	rows := r.ext.environments.tree.Rows()
	if len(rows) != 2 || rows[0].Label != "Dev" || rows[1].Label != "Staging" {
		t.Fatalf("rows = %+v", rows)
	}
	if v := r.api.View(EnvironmentsViewID); v == nil {
		t.Fatal("no view")
	}
}

func TestEditingAnEnvironmentOpensOneTabPerFile(t *testing.T) {
	r := newRig(t)
	r.run(CmdEditEnvironment, r.env("dev"))
	r.run(CmdEditEnvironment, r.env("dev"))
	r.run(CmdEditEnvironment, r.env("staging"))
	tabs := r.api.EditorTabs()
	if len(tabs) != 2 || tabs[0].Title() != "Dev" || tabs[1].Title() != "Staging" {
		t.Fatalf("tabs = %d", len(tabs))
	}
	if !tabs[0].Revealed {
		t.Error("the tab is not shown")
	}
}

func TestThePickerOffersToMakeAnEnvironmentWhenThereAreNone(t *testing.T) {
	r := newRig(t, func(folder string) {
		for _, k := range []string{"dev", "staging"} {
			if err := removeFile(folder + "/.emetos/http/environments/" + k + ".jsonc"); err != nil {
				t.Fatal(err)
			}
		}
	})
	r.run(CmdSelectEnvironment, nil)
	items := r.api.LastPick().Options().Items
	if len(items) != 2 || items[1].Command != CmdNewEnvironment {
		t.Errorf("items = %+v", items)
	}
}
