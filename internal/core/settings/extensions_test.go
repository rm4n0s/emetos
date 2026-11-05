package settings

import (
	"github.com/rm4n0s/errors"

	"context"
	"slices"
	"testing"
)

// The rows of the extensions in the settings editor (step 31.1, SPEC §10.15).

var testExtensions = []ExtensionInfo{
	{ID: "emetos.git", Name: "Git", Description: "Source control with git."},
	{ID: "emetos.terminal", Name: "Terminal", Description: "Your shell in the panel."},
	{ID: "emetos.redis", Name: "Redis", Description: "Browse the keys of a Redis server."},
}

// installed are the ids of the group Extensions > Installed of a search, and whether the group is there.
func installed(res SearchResult) ([]string, bool) {
	for _, c := range res.Categories {
		if f := c.Find(InstalledExtensionsID); f != nil {
			return f.Extensions, true
		}
	}
	return nil, false
}

func TestTheExtensionRowsFollowExtensionsDisabled(t *testing.T) {
	f, m := uiFixture(t)
	m.SetExtensions(testExtensions)
	if err := f.store.Update(context.Background(), DisabledExtensionsKey, []any{"emetos.terminal"}, TargetUser); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		id      string
		enabled bool
		ok      bool
	}{
		{"emetos.git", true, true},
		{"emetos.terminal", false, true},
		{"emetos.unknown", false, false},
	}
	for _, tt := range tests {
		r, ok := m.ExtensionRow(tt.id)
		if ok != tt.ok || (ok && r.Enabled != tt.enabled) {
			t.Errorf("%s: got %+v, %v", tt.id, r, ok)
		}
	}
}

func TestTheInstalledGroupIsFirstUnderExtensions(t *testing.T) {
	_, m := uiFixture(t)
	m.SetExtensions(testExtensions)
	res := m.Search(TargetUser, "", languages)
	var ext *Category
	for _, c := range res.Categories {
		if c.ID == extensionsID {
			ext = c
		}
	}
	if ext == nil || len(ext.Children) == 0 || ext.Children[0].ID != InstalledExtensionsID {
		t.Fatalf("the group Extensions is %+v", ext)
	}
	if got := ext.Children[0].Extensions; !slices.Equal(got, []string{"emetos.git", "emetos.terminal", "emetos.redis"}) {
		t.Errorf("the rows are %v", got)
	}
	if ext.Children[0].Label != "Installed" {
		t.Errorf("the label is %q", ext.Children[0].Label)
	}
}

func TestSearchingTheExtensionRows(t *testing.T) {
	f, m := uiFixture(t)
	m.SetExtensions(testExtensions)
	if err := f.store.Update(context.Background(), DisabledExtensionsKey, []any{"emetos.redis"}, TargetUser); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		target Target
		query  string
		want   []string
		group  bool
	}{
		{"by name", TargetUser, "terminal", []string{"emetos.terminal"}, true},
		{"by description", TargetUser, "source control", []string{"emetos.git"}, true},
		{"by id", TargetUser, "emetos.redis", []string{"emetos.redis"}, true},
		{"@ext:", TargetUser, "@ext:emetos.git", []string{"emetos.git"}, true},
		{"@ext: whatever the case", TargetUser, "@ext:Emetos.Terminal", []string{"emetos.terminal"}, true},
		{"@modified lists the disabled ones", TargetUser, "@modified", []string{"emetos.redis"}, true},
		{"nothing matches", TargetUser, "nonsense-word", nil, false},
		{"@id: is about settings", TargetUser, "@id:editor.fontSize", nil, false},
		{"@lang: is about settings", TargetUser, "@lang:go", nil, false},
		{"the Workspace tab has none", TargetWorkspace, "", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := m.Search(tt.target, tt.query, languages)
			got, ok := installed(res)
			if ok != tt.group || !slices.Equal(got, tt.want) {
				t.Errorf("got %v (group %v), want %v (group %v)", got, ok, tt.want, tt.group)
			}
		})
	}
}

func TestAnExtFilterShowsNoSettings(t *testing.T) {
	_, m := uiFixture(t)
	m.SetExtensions(testExtensions)
	res := m.Search(TargetUser, "@ext:emetos.git", languages)
	n := 0
	for _, c := range res.Categories {
		c.Walk(func(c *Category) { n += len(c.Keys) })
	}
	if n != 0 {
		t.Errorf("%d settings are listed with @ext:", n)
	}
	if res.Count != 1 {
		t.Errorf("the count is %d, want the one row", res.Count)
	}
}

func TestTheRowOfAnExtensionIsACheckBox(t *testing.T) {
	f, m := uiFixture(t)
	m.SetExtensions(testExtensions)
	if err := f.store.Update(context.Background(), DisabledExtensionsKey, []any{"emetos.git"}, TargetUser); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		id       string
		checked  bool
		modified bool
	}{
		{"emetos.git", false, true},
		{"emetos.terminal", true, false},
	}
	for _, tt := range tests {
		r, ok := m.ExtensionSettingRow(tt.id)
		if !ok {
			t.Fatalf("%s: no row", tt.id)
		}
		if r.Kind != KindBool || r.Checked() != tt.checked || r.Modified != tt.modified || r.Label != "Enabled" {
			t.Errorf("%s: %+v", tt.id, r)
		}
		if id, ok := ExtensionOfRowKey(r.Key); !ok || id != tt.id {
			t.Errorf("%s: the key %q", tt.id, r.Key)
		}
	}
	if r, _ := m.ExtensionSettingRow("emetos.git"); r.Title() != "Git: Enabled" || r.Description != "Source control with git. `emetos.git`" {
		t.Errorf("the title %q and the description %q", r.Title(), r.Description)
	}
	if _, ok := m.ExtensionSettingRow("nope"); ok {
		t.Error("a row for an extension that is not listed")
	}
	if _, ok := ExtensionOfRowKey("editor.fontSize"); ok {
		t.Error("a setting's key is the row of an extension")
	}
}

func TestTheRowsOfExtensionsWriteExtensionsDisabled(t *testing.T) {
	f, m := uiFixture(t)
	m.SetExtensions(testExtensions)
	ctx := context.Background()
	disabled := func() []string { return Get[[]string](f.store, DisabledExtensionsKey, "") }
	// another id the user wrote by hand is kept, and the list stays sorted
	if err := f.store.Update(ctx, DisabledExtensionsKey, []any{"other.ext"}, TargetUser); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name string
		do   func() error
		want []string
	}{
		{"turn Git off", func() error { return m.SetValueFor(ctx, ExtensionRowPrefix+"emetos.git", TargetUser, "", false) }, []string{"emetos.git", "other.ext"}},
		{"turn the Terminal off", func() error { return m.SetValueFor(ctx, ExtensionRowPrefix+"emetos.terminal", TargetUser, "", false) }, []string{"emetos.git", "emetos.terminal", "other.ext"}},
		{"off twice changes nothing", func() error { return m.SetValueFor(ctx, ExtensionRowPrefix+"emetos.git", TargetUser, "", false) }, []string{"emetos.git", "emetos.terminal", "other.ext"}},
		{"turn Git on", func() error { return m.SetValueFor(ctx, ExtensionRowPrefix+"emetos.git", TargetUser, "", true) }, []string{"emetos.terminal", "other.ext"}},
		{"Reset Setting turns it on", func() error { return m.ResetFor(ctx, ExtensionRowPrefix+"emetos.terminal", TargetUser, "") }, []string{"other.ext"}},
		{"the last one on removes the setting", func() error { return SetExtensionEnabled(ctx, f.store, "other.ext", true) }, nil},
	}
	for _, st := range steps {
		if err := st.do(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		if got := disabled(); !slices.Equal(got, st.want) {
			t.Errorf("%s: extensions.disabled is %v, want %v", st.name, got, st.want)
		}
	}
}

func TestAnExtensionRowTakesOnlyTrueOrFalse(t *testing.T) {
	_, m := uiFixture(t)
	m.SetExtensions(testExtensions)
	err := m.SetValueFor(context.Background(), ExtensionRowPrefix+"emetos.git", TargetUser, "", "yes")
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("UIModel.SetValueFor.SettingExtensionValueInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
