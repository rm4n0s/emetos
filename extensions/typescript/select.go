package typescript

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The choice of the TypeScript (SPEC §10.34 "As built (step 24b.5)"): "TypeScript: Select TypeScript Version..." lists the TypeScript
// installed beside the language server (or named by typescript.tsdk) and the one in the node_modules of the folder, and keeps the choice
// for the folder in Emetos's state. A choice restarts the server, which is then told which tsserver.js to run.

// env is the Env of this machine for the tab.
func (t *typescript) env() langserver.Env {
	return langserver.DefaultEnv(t.api.Scope().Folder, nil)
}

// candidates are the TypeScript of the user's own installation (typescript.tsdk, else the one beside the server found at its last start)
// and the folder's own, nil when there is none; nothing is started. UI thread.
func (t *typescript) candidates() (global, workspace *Version) {
	env := t.env()
	server := ""
	if t.ctl != nil {
		server = t.ctl.Found().Path
	}
	if tsdk, err := extension.SettingValue[string](t.api.Settings(), TsdkSetting); err == nil && tsdk != "" {
		if v, err := TsdkVersion(env, tsdk); err == nil {
			global = &v
		}
	} else if v, ok := BesideServer(server); ok && server != "" {
		global = &v
	}
	root := langserver.ResolveRoot(env.Folder, rootMarkers, env.Home)
	if v, ok := WorkspaceVersion(root, env.Home); ok {
		workspace = &v
	}
	return global, workspace
}

// chosen is the TypeScript the user chose for the folder, "" when none was.
func (t *typescript) chosen() string {
	v, _ := extension.StateValue[string](t.api.State(), StateKey)
	return v
}

// choose keeps the choice (ChoiceGlobal, ChoiceWorkspace, or "" to forget it) and restarts the server. It fails with the error of State().Set.
func (t *typescript) choose(choice string) error {
	var value any
	if choice != "" {
		value = choice
	}
	if err := t.api.State().Set(StateKey, value); err != nil {
		return err
	}
	if err := t.api.LanguageServers().Restart(ServerID); err != nil && !benign(err) {
		t.api.Log().Warn("the language server cannot be restarted after another TypeScript was chosen", "err", err)
	}
	return nil
}

// fail tells the user why a choice did not work.
func (t *typescript) fail(err error) {
	t.api.Notifications().Error("The TypeScript version cannot be chosen: " + describe(err))
}

// selectVersionCommand is typescript.selectTypeScriptVersion: with a choice as its argument it is made at once, else the picker is shown.
func (t *typescript) selectVersionCommand(_ context.Context, args any) error {
	choice, _ := args.(string)
	if choice == "" {
		t.selectVersion()
		return nil
	}
	if t.api.Origins().Current() != extension.HostOrigin {
		t.api.Notifications().Info("The TypeScript version is a choice of the host and cannot be changed from here.")
		return nil
	}
	if choice != ChoiceGlobal && choice != ChoiceWorkspace {
		t.api.Notifications().Error("The TypeScript version cannot be chosen: " + choice + " is neither " + ChoiceGlobal + " nor " + ChoiceWorkspace + ".")
		return nil
	}
	if err := t.choose(choice); err != nil {
		t.fail(err)
	}
	return nil
}

// versionRow is a row of the picker: the choice it makes, or an action.
type versionRow struct {
	item   extension.QuickPickItem
	choice string
	forget bool
}

// versionRows makes the rows: the global and the workspace version, the one in use marked, and the way to forget the choice.
func (t *typescript) versionRows() []versionRow {
	env := t.env()
	global, workspace := t.candidates()
	inUse := ""
	if v, ok := t.Version(); ok {
		inUse = v.Lib
	}
	var rows []versionRow
	add := func(v *Version, choice, title string, star bool) {
		if v == nil {
			return
		}
		icon := "$(versions) "
		switch {
		case v.Lib == inUse:
			icon = "$(check) "
		case star:
			icon = "$(star-full) "
		}
		rows = append(rows, versionRow{choice: choice, item: extension.QuickPickItem{
			Label: icon + title + " (" + v.Label() + ")", Description: short(env, v.Lib),
		}})
	}
	add(global, ChoiceGlobal, "Use Global Version", false)
	add(workspace, ChoiceWorkspace, "Use Workspace Version", true)
	if t.chosen() != "" {
		rows = append(rows, versionRow{forget: true, item: extension.QuickPickItem{Label: "$(discard) Forget the choice", Description: "use the global version, and offer the workspace's again"}})
	}
	return rows
}

// selectVersion shows the picker "Select TypeScript Version". A client of a hosted workspace is told the version is the host's choice
// (SPEC §10.18). UI thread.
func (t *typescript) selectVersion() {
	if t.api.Origins().Current() != extension.HostOrigin {
		t.api.Notifications().Info("The TypeScript version is a choice of the host and cannot be changed from here.")
		return
	}
	rows := t.versionRows()
	if len(rows) == 0 {
		t.api.Notifications().Warn("No TypeScript was found beside the language server, in the node_modules of the folder, or in " + TsdkSetting + ". Install it with npm install -g typescript@6.")
		return
	}
	items := make([]extension.QuickPickItem, len(rows))
	for i, r := range rows {
		items[i] = r.item
	}
	err := t.api.QuickPick().Show(extension.QuickPickOptions{Title: "Select TypeScript Version", Placeholder: "Select the TypeScript the language server uses", Items: items, MatchOnDescription: true}, func(n int) {
		if n < 0 || n >= len(rows) {
			return
		}
		choice := rows[n].choice
		if rows[n].forget {
			choice = ""
		}
		if err := t.choose(choice); err != nil {
			t.fail(err)
		}
	})
	if err != nil && !benign(err) {
		t.api.Log().Warn("the list of TypeScript versions cannot be shown", "err", err)
	}
}

// maybeOffer asks, once in each activation, whether to use the TypeScript the folder has when the user has chosen nothing: it is code that
// came with the folder, so it runs only when the user says so (decision 981). The answer is kept like any choice. UI thread.
func (t *typescript) maybeOffer() {
	if t.offered || t.chosen() != "" {
		return
	}
	if prompt, err := extension.SettingValue[bool](t.api.Settings(), PromptWorkspaceVersionSetting); err != nil || !prompt {
		return
	}
	t.offered = true
	_, workspace := t.candidates()
	if workspace == nil {
		return
	}
	if v, ok := t.Version(); ok && v.Lib == workspace.Lib {
		return
	}
	t.api.Notifications().Info("This folder has its own TypeScript, "+workspace.Number+". Use it for the language server? Emetos then runs the TypeScript code in "+short(t.env(), workspace.Lib)+", which came with the folder.",
		extension.NotificationAction{Label: "Use Workspace Version", Run: func() {
			if err := t.choose(ChoiceWorkspace); err != nil {
				t.fail(err)
			}
		}},
		extension.NotificationAction{Label: "Select Another...", Run: t.selectVersion},
	)
}
