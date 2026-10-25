package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/ipc"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The tests of the commands of the profiles (step 8.6b): create, switch, rename, delete, export and import, the
// Profiles submenu, the item of the status bar and the name in the title of the window.

// qiDriver drives the quick input of a window the way a user does: it picks a row, types in the input box and
// presses Enter.
type qiDriver struct {
	t    *testing.T
	w    *Workspace
	pump func()
	wait func(what string, cond func() bool)
}

func (d qiDriver) rows() []string {
	var out []string
	for _, r := range d.w.QuickInput.Picker().Rows() {
		out = append(out, r.Item.Label)
	}
	return out
}

// descriptions are the descriptions of the rows, by label.
func (d qiDriver) descriptions() map[string]string {
	out := map[string]string{}
	for _, r := range d.w.QuickInput.Picker().Rows() {
		out[r.Item.Label] = r.Item.Description
	}
	return out
}

func (d qiDriver) active() string {
	r, ok := d.w.QuickInput.Picker().Active()
	if !ok {
		return ""
	}
	return r.Item.Label
}

// choose makes the row with the label the active one and presses Enter.
func (d qiDriver) choose(label string) {
	d.t.Helper()
	qi := d.w.QuickInput
	if !qi.Visible() {
		d.t.Fatalf("the quick input is not shown (looking for %q)", label)
	}
	for i, r := range qi.Picker().Rows() {
		if r.Item.Label == label {
			qi.Picker().SetActive(i)
			qi.Accept(quickinput.KeyMods{})
			d.pump()
			return
		}
	}
	d.t.Fatalf("no row %q in %v", label, d.rows())
}

// prompting waits for the input box.
func (d qiDriver) prompting() {
	d.t.Helper()
	d.wait("the input box", func() bool { return d.w.QuickInput.Prompting() })
}

// typeName types a text in the input box and gives the message under it.
func (d qiDriver) typeName(text string) string {
	d.t.Helper()
	d.prompting()
	d.w.QuickInput.SetValue(text)
	return d.w.QuickInput.Picker().Message()
}

// enter presses Enter in the quick input.
func (d qiDriver) enter() {
	d.t.Helper()
	d.w.QuickInput.Accept(quickinput.KeyMods{})
	d.pump()
}

// pcApp is a window over the profiles of profileFolders (Default and Work) with two files open, and its fake desktop.
type pcApp struct {
	*profileApp
	fd *gluttertest.FakeDesktop
	qi qiDriver
}

func newPCApp(t *testing.T, profile string) *pcApp {
	t.Helper()
	a := newProfileApp(t, ProcessOptions{}, profile)
	return &pcApp{profileApp: a, fd: a.ts.FakeDesktop(), qi: qiDriver{t: t, w: a.win, pump: a.pump, wait: a.waitFor}}
}

func (a *pcApp) profiles() *storage.Profiles { return a.win.Process.Profiles }

// listed are the profiles that exist, the default one as "Default".
func (a *pcApp) listed() []string {
	a.t.Helper()
	list, err := a.profiles().List(context.Background())
	if err != nil {
		a.t.Fatal(err)
	}
	for i, p := range list {
		list[i] = storage.ProfileDisplayName(p)
	}
	return list
}

// tree lists the files of the configuration folder of a profile (not the folder of the profiles), by relative path.
func (a *pcApp) tree(profile string) map[string]string {
	a.t.Helper()
	dir, err := a.f.paths.ConfigDir(profile)
	if err != nil {
		a.t.Fatal(err)
	}
	out := map[string]string{}
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			if rel == "profiles" || rel == "themes" {
				return filepath.SkipDir
			}
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		a.t.Fatal(err)
	}
	return out
}

// noticed waits for a notification with the text; the work behind it runs on a goroutine, so the wait is by time.
func (a *pcApp) noticed(text string) {
	a.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !notified(a.win, text) {
		if time.Now().After(deadline) {
			a.t.Fatalf("timed out waiting for the notification %q; have %v", text, a.win.Notify.Items())
		}
		a.ts.Pump(10 * time.Millisecond)
		time.Sleep(2 * time.Millisecond)
	}
}

func TestTheProfileCommandsAreRegistered(t *testing.T) {
	a := newPCApp(t, "")
	titles := map[string]string{
		"workbench.profiles.actions.createProfile": "Create Profile...", "workbench.profiles.actions.switchProfile": "Switch Profile...",
		"workbench.profiles.actions.renameProfile": "Rename Profile...", "workbench.profiles.actions.deleteProfile": "Delete Profile...",
		"workbench.profiles.actions.exportProfile": "Export Profile...", "workbench.profiles.actions.importProfile": "Import Profile...",
	}
	for id, title := range titles {
		meta, ok := a.win.Commands.Lookup(id)
		if !ok || meta.Title != title || meta.Category != "Profiles" {
			t.Errorf("%s: %+v, registered %v", id, meta, ok)
		}
		if !a.win.Commands.HasHandler(id) {
			t.Errorf("%s has no handler", id)
		}
		if a.win.Keybindings != nil {
			if _, bound := a.win.Keybindings.LookupPrimaryKeybinding(id, a.win.Keys, false); bound {
				t.Errorf("%s has a default key", id)
			}
		}
	}
}

// ---- create ----

func TestCreateProfileAsksForANameThatIsChecked(t *testing.T) {
	a := newPCApp(t, "")
	a.run("workbench.profiles.actions.createProfile")
	if !a.win.QuickInput.Prompting() || a.win.QuickInput.Picker().Placeholder() != "Profile name" {
		t.Fatalf("prompting %v, placeholder %q", a.win.QuickInput.Prompting(), a.win.QuickInput.Picker().Placeholder())
	}
	tests := []struct {
		name string
		text string
		want string
	}{
		{"fine", "Fresh", ""},
		{"with a space and a dash", "My Work-2_x", ""},
		{"empty", "", ProfileNameInvalidText},
		{"a slash", "a/b", ProfileNameInvalidText},
		{"a dot", "a.b", ProfileNameInvalidText},
		{"a space at the start", " a", ProfileNameInvalidText},
		{"a space at the end", "a ", ProfileNameInvalidText},
		{"too long", strings.Repeat("x", 65), ProfileNameInvalidText},
		{"the default profile", "Default", ProfileNameReservedText},
		{"the default profile in lower case", "default", ProfileNameReservedText},
		{"a name that exists", "Work", "A profile with the name 'Work' exists already."},
		{"a name that exists in another case", "WORK", "A profile with the name 'WORK' exists already."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.qi.typeName(tt.text); got != tt.want {
				t.Errorf("message %q, want %q", got, tt.want)
			}
		})
	}
	// Enter on a name that is not valid does nothing: the box stays with its message
	a.qi.typeName("a/b")
	a.qi.enter()
	if !a.win.QuickInput.Prompting() || a.win.QuickInput.Picker().Message() != ProfileNameInvalidText {
		t.Errorf("prompting %v, message %q", a.win.QuickInput.Prompting(), a.win.QuickInput.Picker().Message())
	}
	a.press("escape")
	if a.win.QuickInput.Visible() || !slices.Equal(a.listed(), []string{"Default", "Work"}) {
		t.Errorf("visible %v, profiles %v", a.win.QuickInput.Visible(), a.listed())
	}
	a.noErrors()
}

func TestCreateProfileOffersEmptyAndACopyOfEveryProfile(t *testing.T) {
	tests := []struct {
		name    string
		current string
		want    []string
		marked  string
	}{
		{"in the default profile", "", []string{"Empty Profile", "Copy from Default", "Copy from Work"}, "Copy from Default"},
		{"in another profile the current one is first", "Work", []string{"Empty Profile", "Copy from Work", "Copy from Default"}, "Copy from Work"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newPCApp(t, tt.current)
			a.run("workbench.profiles.actions.createProfile")
			a.qi.typeName("Fresh")
			a.qi.enter()
			a.waitFor("the choice of the source", func() bool {
				return !a.win.QuickInput.Prompting() && a.win.QuickInput.Visible() && len(a.qi.rows()) > 0
			})
			if got := a.qi.rows(); !slices.Equal(got, tt.want) {
				t.Fatalf("rows %v, want %v", got, tt.want)
			}
			if got := a.win.QuickInput.Picker().Placeholder(); got != "Create profile from" {
				t.Errorf("placeholder %q", got)
			}
			for label, desc := range a.qi.descriptions() {
				if (desc == "Current") != (label == tt.marked) {
					t.Errorf("%q has the description %q", label, desc)
				}
			}
			if a.qi.active() != "Empty Profile" {
				t.Errorf("active %q", a.qi.active())
			}
			a.noErrors()
		})
	}
}

func TestCreateProfileFromEachSourceSwitchesTheWindowToIt(t *testing.T) {
	tests := []struct {
		name    string
		current string
		choice  string
		from    string // the profile that is copied; "-" for none
		snippet []string
		font    float64
	}{
		{"an empty profile", "", "Empty Profile", "-", nil, 14},
		{"a copy of Default", "Work", "Copy from Default", storage.DefaultProfile, []string{"d"}, 14},
		{"a copy of Work", "", "Copy from Work", "Work", []string{"w"}, 22},
		{"a copy of the current profile", "Work", "Copy from Work", "Work", []string{"w"}, 22},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newPCApp(t, tt.current)
			want := map[string]string{}
			if tt.from != "-" {
				want = a.tree(tt.from)
				if len(want) < 2 {
					t.Fatalf("the source has %v", want)
				}
			}
			vms := a.editors()
			tabs := a.tabNames()
			var heard []string
			stop := a.win.OnProfileChange(func(p string) { heard = append(heard, p) })
			defer stop()
			a.run("workbench.profiles.actions.createProfile")
			a.qi.typeName("Fresh")
			a.qi.enter()
			a.waitFor("the choice", func() bool { return !a.win.QuickInput.Prompting() && len(a.qi.rows()) > 0 })
			a.qi.choose(tt.choice)
			a.waitFor("the switch", func() bool { return a.win.ProfileName() == "Fresh" })

			if !slices.Equal(a.listed(), []string{"Default", "Fresh", "Work"}) {
				t.Errorf("profiles %v", a.listed())
			}
			got := a.tree("Fresh")
			if len(got) != len(want) {
				t.Errorf("files %v, want %v", got, want)
			}
			for name, text := range want {
				if got[name] != text {
					t.Errorf("%s = %q, want %q", name, got[name], text)
				}
			}
			if !slices.Equal(heard, []string{"Fresh"}) {
				t.Errorf("OnProfileChange heard %v", heard)
			}
			if got := snippetNames(a.win); !slices.Equal(got, tt.snippet) {
				t.Errorf("snippets %v, want %v", got, tt.snippet)
			}
			if user, _ := a.win.Settings.Paths(); !strings.Contains(user, filepath.Join("profiles", "Fresh")) {
				t.Errorf("the settings file is %q", user)
			}
			for _, vm := range a.editors() {
				a.waitFontSize(vm, tt.font)
			}
			// the editors were not touched
			for name, vm := range a.editors() {
				if vms[name] != vm {
					t.Errorf("the editor of %s was made again", name)
				}
			}
			wantNames(t, a.tabNames(), tabs...)
			a.noErrors()
		})
	}
}

func TestCreateProfileThatFailsShowsAnError(t *testing.T) {
	a := newPCApp(t, "")
	a.run("workbench.profiles.actions.createProfile")
	a.qi.typeName("Fresh")
	a.qi.enter()
	a.waitFor("the choice", func() bool { return !a.win.QuickInput.Prompting() && len(a.qi.rows()) > 0 })
	// the profile that is copied goes away while the user chooses
	dir, err := a.f.paths.ConfigDir("Work")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	a.qi.choose("Copy from Work")
	a.noticed("Cannot create the profile 'Fresh'")
	if a.win.ProfileName() != "Default" || slices.Contains(a.listed(), "Fresh") {
		t.Errorf("profile %q, profiles %v", a.win.ProfileName(), a.listed())
	}
}

// ---- switch ----

func TestSwitchProfileListsTheProfilesAndSwitchesLive(t *testing.T) {
	a := newPCApp(t, "")
	vms := a.editors()
	tabs := a.tabNames()
	var heard []string
	stop := a.win.OnProfileChange(func(p string) { heard = append(heard, p) })
	defer stop()

	a.run("workbench.profiles.actions.switchProfile")
	if got := a.qi.rows(); !slices.Equal(got, []string{"Default", "Work"}) {
		t.Fatalf("rows %v", got)
	}
	if a.qi.active() != "Default" || a.qi.descriptions()["Default"] != "Current" || a.qi.descriptions()["Work"] != "" {
		t.Errorf("active %q, descriptions %v", a.qi.active(), a.qi.descriptions())
	}
	// Enter on the current profile does nothing
	a.qi.enter()
	a.pump()
	if len(heard) != 0 || a.win.Profile() != storage.DefaultProfile {
		t.Errorf("heard %v, profile %q", heard, a.win.Profile())
	}
	a.run("workbench.profiles.actions.switchProfile")
	a.qi.choose("Work")
	a.waitFor("the switch", func() bool { return a.win.ProfileName() == "Work" })
	for _, vm := range vms {
		a.waitFontSize(vm, 22)
	}
	if got := themeLabel(a.win); got != "Light Modern" {
		t.Errorf("theme %q", got)
	}
	if got := tabSizeOf(a.win); got != 2 {
		t.Errorf("tab size %d", got)
	}
	for name, vm := range a.editors() {
		if vms[name] != vm {
			t.Errorf("the editor of %s was made again", name)
		}
	}
	wantNames(t, a.tabNames(), tabs...)
	if !slices.Equal(heard, []string{"Work"}) {
		t.Errorf("heard %v", heard)
	}
	// the picker of the other profile marks Work as the current one
	a.run("workbench.profiles.actions.switchProfile")
	if a.qi.active() != "Work" || a.qi.descriptions()["Work"] != "Current" {
		t.Errorf("active %q, descriptions %v", a.qi.active(), a.qi.descriptions())
	}
	a.press("escape")
	a.noErrors()
}

// ---- rename ----

func TestRenameProfileOffersOnlyTheProfilesThatCanBeRenamed(t *testing.T) {
	a := newPCApp(t, "")
	a.run("workbench.profiles.actions.renameProfile")
	if got := a.qi.rows(); !slices.Equal(got, []string{"Work"}) {
		t.Errorf("rows %v: the default profile cannot be renamed", got)
	}
	a.press("escape")

	// with no profile there is a notice
	if err := a.profiles().Delete(context.Background(), "Work"); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.profiles.actions.renameProfile")
	if a.win.QuickInput.Visible() {
		t.Error("a picker shows although there is nothing to rename")
	}
	a.noticed(NoProfilesToRenameText)
}

func TestRenameProfileChecksTheNameAndKeepsTheProfileOfTheWindow(t *testing.T) {
	a := newPCApp(t, "Work")
	if _, err := a.profiles().Create(context.Background(), "Other", storage.CreateEmpty); err != nil {
		t.Fatal(err)
	}
	vms := a.editors()
	a.run("workbench.profiles.actions.renameProfile")
	if got := a.qi.rows(); !slices.Equal(got, []string{"Other", "Work"}) || a.qi.descriptions()["Work"] != "Current" {
		t.Fatalf("rows %v %v", got, a.qi.descriptions())
	}
	a.qi.choose("Work")
	a.qi.prompting()
	if got := a.win.QuickInput.Picker().Value(); got != "Work" {
		t.Errorf("the input starts with %q", got)
	}
	for text, want := range map[string]string{
		"Work":    "", // its own name
		"work":    "", // a change of capitalization
		"Office":  "",
		"other":   "A profile with the name 'other' exists already.",
		"Default": ProfileNameReservedText,
		"a/b":     ProfileNameInvalidText,
		"":        ProfileNameInvalidText,
	} {
		if got := a.qi.typeName(text); got != want {
			t.Errorf("%q: message %q, want %q", text, got, want)
		}
	}
	// the unchanged name is accepted and does nothing
	a.qi.typeName("Work")
	a.qi.enter()
	a.waitFor("the end of the input", func() bool { return !a.win.QuickInput.Visible() })
	if !slices.Equal(a.listed(), []string{"Default", "Other", "Work"}) || a.win.Profile() != "Work" {
		t.Errorf("profiles %v, window %q", a.listed(), a.win.Profile())
	}

	// a new name: the window follows in place
	userBefore, _ := a.win.Settings.Paths()
	a.run("workbench.profiles.actions.renameProfile")
	a.qi.choose("Work")
	a.qi.typeName("Office")
	a.qi.enter()
	a.waitFor("the rename", func() bool { return a.win.ProfileName() == "Office" })
	if !slices.Equal(a.listed(), []string{"Default", "Office", "Other"}) {
		t.Errorf("profiles %v", a.listed())
	}
	user, _ := a.win.Settings.Paths()
	if user == userBefore || !strings.Contains(user, filepath.Join("profiles", "Office")) {
		t.Errorf("the settings file is %q (was %q)", user, userBefore)
	}
	if _, err := os.Stat(userBefore); !os.IsNotExist(err) {
		t.Errorf("the old settings file: %v", err)
	}
	if got := a.tree("Office"); got["keybindings.json"] != workKeys || got["snippets/plaintext.json"] != workSnippets {
		t.Errorf("the files of the renamed profile: %v", got)
	}
	for _, vm := range vms {
		a.waitFontSize(vm, 22)
	}
	if got := snippetNames(a.win); !slices.Equal(got, []string{"w"}) {
		t.Errorf("snippets %v", got)
	}
	if got := a.win.keyLabelOf("workbench.action.openGlobalKeybindings"); got != "Ctrl+Alt+F9" {
		t.Errorf("the key of the profile is %q", got)
	}
	// the settings still work and are written to the new folder
	if err := a.win.Settings.Update(context.Background(), "editor.fontSize", 26, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	for _, vm := range vms {
		a.waitFontSize(vm, 26)
	}
	if data, err := os.ReadFile(user); err != nil || !strings.Contains(string(data), "26") {
		t.Errorf("the settings file: %q, %v", data, err)
	}

	// a change of capitalization is a rename
	a.run("workbench.profiles.actions.renameProfile")
	a.qi.choose("Office")
	a.qi.typeName("office")
	a.qi.enter()
	a.waitFor("the change of capitalization", func() bool { return a.win.ProfileName() == "office" })
	if !slices.Equal(a.listed(), []string{"Default", "office", "Other"}) {
		t.Errorf("profiles %v", a.listed())
	}
	a.noErrors()
}

func TestRenamingAProfileTwoWindowsUseMovesBoth(t *testing.T) {
	env := newMultiEnv(t, nil)
	workProfile(t, env)
	one, two, three := env.folder("one", "a.go"), env.folder("two", "b.go"), env.folder("three", "c.go")
	a := env.run(Options{Folder: one, Open: &ipc.OpenArgs{Folder: one, Profile: "Work"}})
	if _, err := a.m.Open(WorkspaceSpec{Folder: two, Profile: "Work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.m.Open(WorkspaceSpec{Folder: three}); err != nil {
		t.Fatal(err)
	}
	a.settle()
	w1, w2, w3 := a.workspaceOf(one), a.workspaceOf(two), a.workspaceOf(three)
	if w1.ProfileName() != "Work" || w2.ProfileName() != "Work" || w3.ProfileName() != "Default" {
		t.Fatalf("profiles %s %s %s", w1.ProfileName(), w2.ProfileName(), w3.ProfileName())
	}
	// the windows write their state
	for _, w := range []*Workspace{w1, w2} {
		a.shellOf(w).flushState()
	}
	if err := a.p.flushStates(a.ctx); err != nil {
		t.Fatal(err)
	}
	oldState, _ := a.env.paths.StateDir("Work")
	if _, err := os.Stat(oldState); err != nil {
		t.Fatalf("the windows of the profile have no state folder: %v", err)
	}
	d := qiDriver{t: t, w: w1, pump: a.settle, wait: a.waitFor}
	if err := a.command(w1, "workbench.profiles.actions.renameProfile", nil); err != nil {
		t.Fatal(err)
	}
	d.choose("Work")
	d.typeName("Office")
	d.enter()
	a.waitFor("the rename", func() bool { return w1.ProfileName() == "Office" && w2.ProfileName() == "Office" })
	if w3.ProfileName() != "Default" {
		t.Errorf("the window of the default profile has %q", w3.ProfileName())
	}
	// the folders moved, the state with them
	oldConfig, _ := a.env.paths.ConfigDir("Work")
	newConfig, _ := a.env.paths.ConfigDir("Office")
	newState, _ := a.env.paths.StateDir("Office")
	for path, want := range map[string]bool{oldConfig: false, oldState: false, newConfig: true, newState: true} {
		if _, err := os.Stat(path); (err == nil) != want {
			t.Errorf("%s: exists %v, want %v", path, err == nil, want)
		}
	}
	// both windows read and write the new file
	for _, w := range []*Workspace{w1, w2} {
		if user, _ := w.Settings.Paths(); user != filepath.Join(newConfig, "settings.json") {
			t.Errorf("the settings file of %s is %q", w.Folder, user)
		}
		if got := settings.Get[float64](w.Settings, "editor.fontSize", ""); got != 22 {
			t.Errorf("font size %v", got)
		}
		if got := snippetNames(w); !slices.Equal(got, []string{"w"}) {
			t.Errorf("snippets %v", got)
		}
		if st := &w.watch; st.userDir != newConfig || st.userSettings != filepath.Join(newConfig, "settings.json") {
			t.Errorf("the watcher follows %q and %q", st.userDir, st.userSettings)
		}
	}
	if err := w1.Settings.Update(a.ctx, "editor.fontSize", 31, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if got := settings.Get[float64](w2.Settings, "editor.fontSize", ""); got != 31 {
		t.Errorf("the other window has font size %v", got)
	}
	if got := settings.Get[float64](w3.Settings, "editor.fontSize", ""); got != 14 {
		t.Errorf("the window of the default profile has font size %v", got)
	}
	// the open windows and the folders are noted with the new name
	g, err := a.p.State.LoadGlobal(a.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var noted []string
	for _, w := range g.WorkspaceTabs {
		noted = append(noted, filepath.Base(w.Folder)+":"+w.Profile)
	}
	slices.Sort(noted)
	if !slices.Equal(noted, []string{"one:Office", "three:", "two:Office"}) {
		t.Errorf("open windows in the state: %v", noted)
	}
	if got, ok, err := a.p.Profiles.LastProfile(a.ctx, one); err != nil || !ok || got != "Office" {
		t.Errorf("last profile of the first folder: %q %v %v", got, ok, err)
	}
	// the windows leave the profile without bringing the old folders back
	if err := w1.SwitchProfile(a.ctx, "Default"); err != nil {
		t.Fatal(err)
	}
	if err := a.p.flushStates(a.ctx); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{oldConfig, oldState} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s is back: %v", path, err)
		}
	}
}

func TestAWatchedWindowFollowsTheRenamedProfile(t *testing.T) {
	f := newProfileFolders(t)
	win := watchedProfileWindow(t, f, "Work")
	ctx := context.Background()
	if err := f.profiles().Rename(ctx, "Work", "Office"); err != nil {
		t.Fatal(err)
	}
	win.Process.forgetState("Work")
	if err := win.profileRenamed(ctx, "Office"); err != nil {
		t.Fatal(err)
	}
	if win.ProfileName() != "Office" {
		t.Fatalf("profile %q", win.ProfileName())
	}
	tab := func() int { return tabSizeOf(win) }
	if tab() != 2 {
		t.Fatalf("tab size %d", tab())
	}
	// an edit of the files in the folder it has now reaches the window
	f.write("Office", "settings.json", `{ "editor.tabSize": 7 }`)
	eventually(t, "the tab size of the renamed profile", func() bool { return tab() == 7 })
	f.write("Office", "keybindings.json", `[{ "key": "ctrl+alt+f8", "command": "workbench.action.openSettings" }]`)
	eventually(t, "the keys of the renamed profile", func() bool {
		return slices.Equal(userKeyLabels(win, "workbench.action.openSettings"), []string{"Ctrl+Alt+F8"})
	})
	f.write("Office", "snippets/plaintext.json", `{ "n": { "prefix": "nn", "body": "N" } }`)
	eventually(t, "the snippets of the renamed profile", func() bool { return slices.Equal(snippetNames(win), []string{"n"}) })
}

// ---- delete ----

func TestDeleteProfileOffersOnlyTheProfilesThatCanBeDeleted(t *testing.T) {
	a := newPCApp(t, "")
	a.run("workbench.profiles.actions.deleteProfile")
	if got := a.qi.rows(); !slices.Equal(got, []string{"Work"}) {
		t.Errorf("rows %v: the default profile cannot be deleted", got)
	}
	if got := a.win.QuickInput.Picker().Placeholder(); got != "Select Profile to Delete" {
		t.Errorf("placeholder %q", got)
	}
	a.press("escape")
	if err := a.profiles().Delete(context.Background(), "Work"); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.profiles.actions.deleteProfile")
	a.noticed(NoProfilesToDeleteText)
}

func TestDeleteProfileAsksAndCancelLeavesEverything(t *testing.T) {
	a := newPCApp(t, "")
	a.run("workbench.profiles.actions.deleteProfile")
	a.qi.choose("Work")
	a.waitFor("the question", func() bool { return a.win.Dialogs.IsShowing() })
	spec, _ := a.win.Dialogs.Current()
	if spec.Message != "Are you sure you want to delete the profile 'Work'? This cannot be undone." ||
		!slices.Equal(spec.Buttons, []string{"Delete", "Cancel"}) || spec.Type != kit.DialogWarning {
		t.Errorf("dialog %+v", spec)
	}
	before := a.tree("Work")
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText("Cancel")))
	a.pump()
	if a.win.Dialogs.IsShowing() || !slices.Equal(a.listed(), []string{"Default", "Work"}) {
		t.Errorf("dialog %v, profiles %v", a.win.Dialogs.IsShowing(), a.listed())
	}
	after := a.tree("Work")
	if len(after) != len(before) || len(after) == 0 {
		t.Errorf("the files changed: %v", after)
	}
	a.noErrors()
}

func TestDeleteProfileMovesTheWindowsThatUseItToDefault(t *testing.T) {
	a := newPCApp(t, "Work")
	p := a.win.Process
	ctx := context.Background()
	folder := t.TempDir()
	if err := p.Profiles.SetLastProfile(ctx, folder, "Work"); err != nil {
		t.Fatal(err)
	}
	if err := p.flushStates(ctx); err != nil {
		t.Fatal(err)
	}
	vms := a.editors()
	tabs := a.tabNames()
	stateDir, _ := a.f.paths.StateDir("Work")
	configDir, _ := a.f.paths.ConfigDir("Work")
	var heard []string
	stop := a.win.OnProfileChange(func(profile string) { heard = append(heard, profile) })
	defer stop()

	a.run("workbench.profiles.actions.deleteProfile")
	if a.qi.descriptions()["Work"] != "Current" {
		t.Errorf("descriptions %v", a.qi.descriptions())
	}
	a.qi.choose("Work")
	a.waitFor("the question", func() bool { return a.win.Dialogs.IsShowing() })
	a.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText("Delete")))
	a.waitFor("the deletion", func() bool { return !slices.Contains(a.listed(), "Work") })

	if a.win.Profile() != storage.DefaultProfile || !slices.Equal(heard, []string{storage.DefaultProfile}) {
		t.Errorf("profile %q, heard %v", a.win.Profile(), heard)
	}
	if err := p.flushStates(ctx); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{configDir, stateDir} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s: %v", dir, err)
		}
	}
	if _, ok, err := p.Profiles.LastProfile(ctx, folder); err != nil || ok {
		t.Errorf("the folder still has a last profile: %v %v", ok, err)
	}
	g, err := p.State.LoadGlobal(ctx)
	if err != nil || len(g.LastProfiles) != 0 {
		t.Errorf("last profiles %v, %v", g.LastProfiles, err)
	}
	// the window uses the default profile in place
	for _, vm := range a.editors() {
		a.waitFontSize(vm, 14)
	}
	for name, vm := range a.editors() {
		if vms[name] != vm {
			t.Errorf("the editor of %s was made again", name)
		}
	}
	wantNames(t, a.tabNames(), tabs...)
	if got := themeLabel(a.win); got != "Dark Modern" {
		t.Errorf("theme %q", got)
	}
	if st := a.status("status.profile"); st.Visible {
		t.Errorf("the item of the profile shows %q", st.Entry.Text)
	}
	a.noErrors()
}

func TestDeleteProfileThatFailsShowsAnError(t *testing.T) {
	f := newProfileFolders(t)
	p := f.process(ProcessOptions{})
	p.Profiles = storage.NewProfiles(f.paths, storage.ProfilesOptions{
		State: p.State, RemoveAll: func(string) error { return errors.New("InjectedFailure", "the disk is full") },
	})
	win := f.window(p, "", "")
	g := newGroupAppWith(t, win, testWindowSize, []string{"a.go"}, nil, "a.go")
	d := qiDriver{t: t, w: win, pump: g.pump, wait: g.waitFor}
	g.run("workbench.profiles.actions.deleteProfile")
	d.choose("Work")
	g.waitFor("the question", func() bool { return win.Dialogs.IsShowing() })
	g.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText("Delete")))
	g.waitFor("the error", func() bool { return notified(win, "Cannot delete the profile 'Work'") })
}

// ---- export and import ----

// readExport is the JSON object of an exported file.
func readExport(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s is not JSON: %v\n%s", path, err, data)
	}
	return out
}

// exportTo exports a profile through the dialog of the desktop to a new file.
func (a *pcApp) exportTo(profile string) string {
	a.t.Helper()
	target := filepath.Join(a.t.TempDir(), "profile.json")
	a.run("workbench.profiles.actions.exportProfile")
	a.qi.choose(profile)
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.AnswerFileChooser(target)
	a.noticed("exported to " + target)
	return target
}

func TestExportProfileWritesTheJsonOfTheSpec(t *testing.T) {
	a := newPCApp(t, "")
	a.run("workbench.profiles.actions.exportProfile")
	if got := a.qi.rows(); !slices.Equal(got, []string{"Default", "Work"}) || a.qi.active() != "Default" || a.qi.descriptions()["Default"] != "Current" {
		t.Fatalf("rows %v, active %q", got, a.qi.active())
	}
	a.qi.choose("Work")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	target := filepath.Join(t.TempDir(), "work.json")
	a.fd.AnswerFileChooser(target)
	a.noticed("Profile 'Work' exported to " + target)

	got := readExport(t, target)
	work := a.tree("Work")
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"keybindings", "name", "settings", "snippets"}) {
		t.Errorf("keys %v", keys)
	}
	if got["name"] != "Work" || got["settings"] != work["settings.json"] || got["keybindings"] != workKeys {
		t.Errorf("content %v", got)
	}
	snippets, _ := got["snippets"].(map[string]any)
	if len(snippets) != 1 || snippets["plaintext.json"] != workSnippets {
		t.Errorf("snippets %v", snippets)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("the file: %v, %v", info, err)
	}
	// the default profile has no keybindings: an empty text, and its own snippet
	def := a.exportTo("Default")
	got = readExport(t, def)
	if got["name"] != "Default" || got["keybindings"] != "" {
		t.Errorf("default export %v", got)
	}
	a.noErrors()
}

func TestExportProfileDialogCancelledDoesNothing(t *testing.T) {
	a := newPCApp(t, "")
	a.run("workbench.profiles.actions.exportProfile")
	a.qi.choose("Work")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.CancelFileChooser()
	for range 10 {
		a.pump()
		time.Sleep(2 * time.Millisecond)
	}
	if got := a.win.Notify.Items(); len(got) != 0 {
		t.Errorf("notifications %v", got)
	}
	a.noErrors()
}

func TestExportProfileToAFolderThatIsNotThereShowsAnError(t *testing.T) {
	a := newPCApp(t, "")
	a.run("workbench.profiles.actions.exportProfile")
	a.qi.choose("Work")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.AnswerFileChooser(filepath.Join(t.TempDir(), "no", "such", "work.json"))
	a.noticed("Cannot export the profile 'Work'")
}

func TestExportThenImportRoundTrips(t *testing.T) {
	a := newPCApp(t, "")
	// a second snippet file and a file the export leaves out
	a.f.write("Work", "snippets/all.code-snippets", "{ // comment\n}\n")
	a.f.write("Work", "snippets/notes.txt", "not a snippet")
	work := a.tree("Work")
	target := a.exportTo("Work")

	// the name is taken: the input box proposes a free one
	a.run("workbench.profiles.actions.importProfile")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.AnswerFileChooser(target)
	a.qi.prompting()
	if got := a.win.QuickInput.Picker().Value(); got != "Work 2" {
		t.Fatalf("the input proposes %q", got)
	}
	if got := a.qi.typeName("work"); got != "A profile with the name 'work' exists already." {
		t.Errorf("message %q", got)
	}
	a.qi.enter()
	if !a.win.QuickInput.Prompting() {
		t.Error("a name that is taken was accepted")
	}
	a.qi.typeName("Copy of Work")
	a.qi.enter()
	a.waitFor("the import", func() bool { return a.win.ProfileName() == "Copy of Work" })

	got := a.tree("Copy of Work")
	want := map[string]string{}
	for k, v := range work {
		if k != "snippets/notes.txt" {
			want[k] = v
		}
	}
	if len(got) != len(want) {
		t.Errorf("files %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if !slices.Equal(a.listed(), []string{"Default", "Copy of Work", "Work"}) {
		t.Errorf("profiles %v", a.listed())
	}
	// the window uses it
	for _, vm := range a.editors() {
		a.waitFontSize(vm, 22)
	}
	if got := a.win.keyLabelOf("workbench.action.openGlobalKeybindings"); got != "Ctrl+Alt+F9" {
		t.Errorf("the key is %q", got)
	}
	a.noErrors()
}

func TestImportProfileWithAFreeNameNeedsNoQuestion(t *testing.T) {
	a := newPCApp(t, "")
	target := filepath.Join(t.TempDir(), "fresh.json")
	if err := os.WriteFile(target, []byte(`{"name": "Fresh", "settings": "{ \"editor.tabSize\": 3 }", "unknown": [1], "snippets": {"go.json": "{}"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a.run("workbench.profiles.actions.importProfile")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.AnswerFileChooser(target)
	a.waitFor("the import", func() bool { return a.win.ProfileName() == "Fresh" })
	got := a.tree("Fresh")
	if len(got) != 2 || got["settings.json"] != `{ "editor.tabSize": 3 }` || got["snippets/go.json"] != "{}" {
		t.Errorf("files %v", got)
	}
	if got := tabSizeOf(a.win); got != 3 {
		t.Errorf("tab size %d", got)
	}
	a.noErrors()
}

func TestImportProfileCalledDefaultGetsAnotherName(t *testing.T) {
	a := newPCApp(t, "")
	def := a.exportTo("Default")
	a.run("workbench.profiles.actions.importProfile")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.AnswerFileChooser(def)
	a.qi.prompting()
	if got := a.win.QuickInput.Picker().Value(); got != "Default 2" {
		t.Fatalf("the input proposes %q", got)
	}
	if got := a.qi.typeName("default"); got != ProfileNameReservedText {
		t.Errorf("message %q", got)
	}
	a.qi.typeName("Default 2")
	a.qi.enter()
	a.waitFor("the import", func() bool { return a.win.ProfileName() == "Default 2" })
	if got := a.tree("Default 2"); got["settings.json"] != a.tree(storage.DefaultProfile)["settings.json"] {
		t.Errorf("settings %q", got["settings.json"])
	}
}

func TestImportProfileOfABadFileShowsAnErrorAndMakesNothing(t *testing.T) {
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tests := []struct {
		name string
		path func() string
	}{
		{"not JSON", func() string { return write("a.json", "not json") }},
		{"an array", func() string { return write("b.json", "[]") }},
		{"no name", func() string { return write("c.json", `{"settings": "{}"}`) }},
		{"a bad name", func() string { return write("d.json", `{"name": "../x"}`) }},
		{"a snippet that goes up a folder", func() string { return write("e.json", `{"name": "Fresh", "snippets": {"../evil.json": "x"}}`) }},
		{"settings of the wrong type", func() string { return write("f.json", `{"name": "Fresh", "settings": 3}`) }},
		{"no such file", func() string { return filepath.Join(dir, "missing.json") }},
		{"too large", func() string {
			return write("g.json", `{"name": "Fresh", "settings": "`+strings.Repeat("a", storage.MaxProfileExportSize)+`"}`)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newPCApp(t, "")
			a.run("workbench.profiles.actions.importProfile")
			a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
			a.fd.AnswerFileChooser(tt.path())
			a.noticed("Cannot import the profile")
			if !slices.Equal(a.listed(), []string{"Default", "Work"}) || a.win.Profile() != storage.DefaultProfile || a.win.QuickInput.Visible() {
				t.Errorf("profiles %v, window %q, quick input %v", a.listed(), a.win.ProfileName(), a.win.QuickInput.Visible())
			}
			entries, _ := os.ReadDir(filepath.Join(a.f.paths.ConfigHome, "emetos", "profiles"))
			if len(entries) != 1 {
				t.Errorf("the profiles folder has %d entries", len(entries))
			}
		})
	}
}

func TestImportProfileDialogCancelledDoesNothing(t *testing.T) {
	a := newPCApp(t, "")
	a.run("workbench.profiles.actions.importProfile")
	a.fd.WaitFor("the dialog", func() bool { return a.fd.PendingFileChoosers() == 1 })
	a.fd.CancelFileChooser()
	for range 10 {
		a.pump()
		time.Sleep(2 * time.Millisecond)
	}
	if got := a.win.Notify.Items(); len(got) != 0 || a.win.QuickInput.Visible() || a.win.Profile() != storage.DefaultProfile {
		t.Errorf("notifications %v, quick input %v, profile %q", got, a.win.QuickInput.Visible(), a.win.Profile())
	}
	a.noErrors()
}

func TestTheDialogsOfExportAndImportHaveTheirOwnTags(t *testing.T) {
	win := newTestWorkspace(t, "")
	ts := testutil.New(t, ui.Size{Width: 400, Height: 300})
	ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	ts.Pump(20 * time.Millisecond)
	bc := ts.Element(gluttertest.ByType[*shell]())
	// the dialog waits for the user, so it is refused on the UI thread, which is where this test runs
	_, exportErr := chooseProfileExportPath(context.Background(), bc, "Work")
	_, importErr := chooseProfileImportPath(context.Background(), bc)
	for _, tt := range []struct {
		err   error
		route string
	}{
		{exportErr, "chooseProfileExportPath.ProfileExportDialogFailed"},
		{importErr, "chooseProfileImportPath.ProfileImportDialogFailed"},
	} {
		appErr, ok := errors.FromError(tt.err)
		if !ok {
			t.Fatalf("expected *errors.Error, got %T", tt.err)
		}
		if !appErr.HasRoute(tt.route) {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
		if appErr.OriginalErr == nil {
			t.Error("the error of the dialog is gone")
		}
	}
}

// ---- menus, status bar, title ----

func TestTheProfilesSubmenuIsInTheManageMenuAndInPreferences(t *testing.T) {
	a := newPCApp(t, "")
	s := a.shell()
	items := s.menus.ManageItems()
	var profiles *kit.MenuItem
	for i := range items {
		if items[i].Label == "Profiles" {
			profiles = &items[i]
		}
	}
	if profiles == nil || !profiles.Submenu {
		t.Fatalf("the Manage menu has no Profiles submenu: %+v", items)
	}
	var rows []string
	for _, it := range profiles.Items {
		rows = append(rows, it.Label+"="+it.ID)
	}
	want := []string{
		"Create Profile...=workbench.profiles.actions.createProfile", "Switch Profile...=workbench.profiles.actions.switchProfile",
		"Rename Profile...=workbench.profiles.actions.renameProfile", "Delete Profile...=workbench.profiles.actions.deleteProfile",
		"=", "Export Profile...=workbench.profiles.actions.exportProfile", "Import Profile...=workbench.profiles.actions.importProfile",
	}
	if !slices.Equal(rows, want) {
		t.Errorf("rows %v", rows)
	}
	// a row runs its command
	for _, it := range profiles.Items {
		if it.ID == "workbench.profiles.actions.switchProfile" {
			s.menus.Run(it)
		}
	}
	a.waitFor("the picker", func() bool { return a.win.QuickInput.Visible() })
	if got := a.qi.rows(); !slices.Equal(got, []string{"Default", "Work"}) {
		t.Errorf("rows %v", got)
	}
	a.press("escape")
	// and the palette lists the commands under Profiles
	meta, _ := a.win.Commands.Lookup("workbench.profiles.actions.deleteProfile")
	if meta.Category != "Profiles" {
		t.Errorf("category %q", meta.Category)
	}
}

func TestTheStatusBarShowsTheProfileWhenItIsNotTheDefault(t *testing.T) {
	a := newPCApp(t, "")
	if it := a.status("status.profile"); it.Visible {
		t.Fatalf("the item shows %q for the default profile", it.Entry.Text)
	}
	a.switchTo("Work")
	it := a.status("status.profile")
	if !it.Visible || it.Entry.Text != "$(account) Work" || it.Entry.Tooltip != "Profile: Work" ||
		it.Entry.Command != "workbench.profiles.actions.switchProfile" || it.Alignment != workbench.StatusRight {
		t.Errorf("item %+v", it)
	}
	// it is at the right of the language item and before the bell
	var ids []string
	for _, it := range a.win.StatusBar.Items(workbench.StatusRight) {
		ids = append(ids, it.ID)
	}
	iProfile, iLang, iBell := slices.Index(ids, "status.profile"), slices.Index(ids, "status.editor.mode"), slices.Index(ids, "status.notifications")
	if iProfile < 0 || iLang < 0 || iBell < 0 || iLang >= iProfile || iProfile >= iBell {
		t.Errorf("right items %v", ids)
	}
	// a rename changes the text
	if err := a.profiles().Rename(context.Background(), "Work", "Office"); err != nil {
		t.Fatal(err)
	}
	a.win.Process.forgetState("Work")
	if err := a.win.profileRenamed(context.Background(), "Office"); err != nil {
		t.Fatal(err)
	}
	if it := a.status("status.profile"); !it.Visible || it.Entry.Text != "$(account) Office" || it.Entry.Tooltip != "Profile: Office" {
		t.Errorf("item after the rename %+v", it)
	}
	a.switchTo("Default")
	if it := a.status("status.profile"); it.Visible {
		t.Errorf("the item shows %q after the switch to the default profile", it.Entry.Text)
	}
	a.noErrors()
}

func TestAClickOnTheProfileItemOpensTheSwitchPicker(t *testing.T) {
	a := newPCApp(t, "Work")
	cmd := a.status("status.profile").Entry.Command
	if cmd != "workbench.profiles.actions.switchProfile" {
		t.Fatalf("command %q", cmd)
	}
	a.clickStatus("status.profile")
	if !a.win.QuickInput.Visible() {
		a.waitFor("the picker", func() bool { return a.win.QuickInput.Visible() })
	}
	if got := a.qi.rows(); !slices.Equal(got, []string{"Default", "Work"}) || a.qi.active() != "Work" {
		t.Errorf("rows %v, active %q", got, a.qi.active())
	}
	a.press("escape")
	a.noErrors()
}

func TestTheTitleHasTheNameOfTheProfileUnlessItIsTheDefault(t *testing.T) {
	a := newPCApp(t, "")
	a.pump()
	if strings.Contains(a.win.title, "Work") || !strings.HasSuffix(a.win.title, "Emetos") {
		t.Errorf("title %q", a.win.title)
	}
	a.switchTo("Work")
	a.waitFor("the title", func() bool { return strings.Contains(a.win.title, " - Work - Emetos") })
	a.switchTo("Default")
	a.waitFor("the title again", func() bool { return !strings.Contains(a.win.title, "Work") })
}

// profileNameProblem and the tags the messages are made from.
func TestProfileNameProblemTexts(t *testing.T) {
	a := newPCApp(t, "")
	s := a.shell()
	tests := []struct {
		name, text, except, want string
	}{
		{"fine", "Fresh", "", ""},
		{"invalid", "a/b", "", ProfileNameInvalidText},
		{"reserved", "DEFAULT", "", ProfileNameReservedText},
		{"taken", "work", "", "A profile with the name 'work' exists already."},
		{"the profile itself", "work", "Work", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.profileNameProblem(tt.text, tt.except); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
