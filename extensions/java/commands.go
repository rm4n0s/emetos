package java

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The commands of the Java pack (SPEC §10.34 "Java"), with the ids and titles of vscode-java where it has them. The ones that need the
// server ask it off the UI thread (LanguageServers().Request) and show the answer on it.

// The ids of the commands.
const (
	// RestartCommand restarts the language server ("Java: Restart Language Server"; vscode-java's id).
	RestartCommand = "java.server.restart"
	// CleanWorkspaceCommand deletes the workspace of the server (its index and the state of its projects) and starts it again.
	CleanWorkspaceCommand = "java.clean.workspace"
	// CompileWorkspaceCommand builds the projects again ("Java: Force Java Compilation"); a boolean argument says full (true) or incremental
	// without asking.
	CompileWorkspaceCommand = "java.workspace.compile"
	// UpdateProjectConfigurationCommand reads the build files of the project of the file in front (or of the URI the command is given) again.
	UpdateProjectConfigurationCommand = "java.projectConfiguration.update"
	// OrganizeImportsCommand sorts the imports of the file in front and removes the ones nothing uses.
	OrganizeImportsCommand = "java.action.organizeImports"
)

// What the commands ask of the server.
const (
	buildWorkspaceMethod        = "java/buildWorkspace"
	projectConfigUpdateMethod   = "java/projectConfigurationUpdate"
	classFileContentsMethod     = "java/classFileContents"
	kindOrganizeImports         = "source.organizeImports"
	javaWhen                    = "editorLangId == java"
	buildFileWhen               = "resourceFilename == pom.xml || resourceFilename == build.gradle || resourceFilename == build.gradle.kts"
	commandCategory             = "Java"
	compileIncremental          = "Incremental"
	compileFull                 = "Full"
	compileFailed, compileOK    = 0, 1
	compileErrors, compileAbort = 2, 3
)

// commandSpecs are the commands the manifest declares.
func commandSpecs() []extension.CommandSpec {
	return []extension.CommandSpec{
		{ID: RestartCommand, Title: "Restart Language Server", Category: commandCategory, Description: "Stops jdtls and starts it again."},
		{ID: CleanWorkspaceCommand, Title: "Clean Java Language Server Workspace", Category: commandCategory,
			Description: "Deletes the workspace of jdtls (its index and the state of the projects it imported) and starts it again. Nothing in the folder is touched."},
		{ID: CompileWorkspaceCommand, Title: "Force Java Compilation", Category: commandCategory, Description: "Builds the Java projects again, incrementally or in full."},
		{ID: UpdateProjectConfigurationCommand, Title: "Update Project Configuration", Category: commandCategory, When: javaWhen + " || " + buildFileWhen,
			Description: "Makes jdtls read the build files (pom.xml, build.gradle) of the project of the file in front again."},
		{ID: OrganizeImportsCommand, Title: "Organize Imports", Category: commandCategory, When: javaWhen, Description: "Sorts the imports of the file and removes the ones it does not use."},
	}
}

// keybindings are the default keys: vscode-java's Shift+Alt+U for the project configuration, and Shift+Alt+O (the editor's Organize
// Imports, which asks the same code action) for the pack's command in a Java file.
func keybindings() []extension.Keybinding {
	return []extension.Keybinding{
		{Command: UpdateProjectConfigurationCommand, Key: "shift+alt+u", When: "editorTextFocus && (" + javaWhen + " || " + buildFileWhen + ")"},
		{Command: OrganizeImportsCommand, Key: "shift+alt+o", When: "editorTextFocus && !editorReadonly && " + javaWhen},
	}
}

// handlers are the commands of the pack and what runs them.
func (j *java) handlers() map[string]extension.CommandHandler {
	return map[string]extension.CommandHandler{
		RestartCommand:                    func(context.Context, any) error { j.restart(); return nil },
		CleanWorkspaceCommand:             func(context.Context, any) error { j.cleanWorkspace(); return nil },
		CompileWorkspaceCommand:           func(_ context.Context, args any) error { j.compileWorkspace(args); return nil },
		UpdateProjectConfigurationCommand: func(_ context.Context, args any) error { j.updateProjectConfiguration(args); return nil },
		OrganizeImportsCommand:            func(ctx context.Context, _ any) error { j.organizeImports(ctx); return nil },
	}
}

// benign tells whether an error only says that the host has no such UI or the extension ended.
func benign(err error) bool {
	appErr, ok := errors.FromError(err)
	return ok && (strings.HasSuffix(appErr.Tag, "Unavailable") || appErr.Tag == "ExtensionDeactivated")
}

// describe is the text of an error for the user.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message
	}
	return err.Error()
}

// isRunning tells whether the server runs; when it does not, the user is told what state it is in.
func (j *java) isRunning(what string) bool {
	st, err := j.api.LanguageServers().Status(ServerID)
	if err != nil {
		j.api.Notifications().Error(what + ": " + describe(err))
		return false
	}
	if st.State == extension.ServerRunning {
		return true
	}
	j.api.Notifications().Warn(what + " needs jdtls, which is " + stateWord(st.State) + ". Its state is in the status bar.")
	return false
}

// stateWord says in words what state the server is in.
func stateWord(s extension.ServerState) string {
	switch s {
	case extension.ServerStarting:
		return "starting"
	case extension.ServerMissing:
		return "not available (see the notification, or the menu of the status bar item)"
	case extension.ServerFailed:
		return "stopped after crashing (restart it from the menu of the status bar item)"
	case extension.ServerStopped:
		return "stopped"
	}
	return "not running yet (open a Java file)"
}

// restart restarts the language server.
func (j *java) restart() {
	if err := j.api.LanguageServers().Restart(ServerID); err != nil && !benign(err) {
		j.api.Log().Warn("jdtls cannot be restarted", "err", err)
		j.api.Notifications().Error("The language server cannot be restarted: " + describe(err))
	}
}

// cleanWorkspace asks, then deletes the workspace of the server at its next start and restarts it.
func (j *java) cleanWorkspace() {
	err := j.api.Dialogs().Ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "Clean the Java language server workspace?",
		Detail:  "jdtls loses its index and the state of the projects it imported, and imports them again. Nothing in your folder changes.",
		Buttons: []extension.DialogButton{{Label: "Clean and Restart"}, {Label: "Cancel", Cancel: true}},
	}, func(r extension.DialogResult) {
		if r.Cancelled || r.Button != 0 {
			return
		}
		j.cleanNext.Store(true)
		j.restart()
	})
	if err != nil && !benign(err) {
		j.api.Log().Warn("the question about the workspace cannot be shown", "err", err)
		j.api.Notifications().Error("Clean Java Language Server Workspace: " + describe(err))
	}
}

// compileWorkspace is Force Java Compilation: it builds the projects, in full or incrementally. A boolean argument decides without
// asking.
func (j *java) compileWorkspace(args any) {
	const what = "Force Java Compilation"
	if !j.isRunning(what) {
		return
	}
	run := func(full bool) {
		progress := j.api.Notifications().Progress(extension.ProgressOptions{Title: "Compiling the Java projects"})
		j.work.Add(1)
		go func() {
			defer j.work.Done()
			result, err := j.api.LanguageServers().Request(j.ctx, ServerID, buildWorkspaceMethod, []any{full})
			j.api.Post(func() {
				progress.Done()
				if err != nil {
					j.api.Log().Warn("jdtls could not build the workspace", "err", err)
					j.api.Notifications().Error(what + ": the language server failed: " + describe(err))
					return
				}
				var status int
				if json.Unmarshal(result, &status) != nil {
					status = compileOK
				}
				switch status {
				case compileFailed:
					j.api.Notifications().Error("The Java build failed.")
				case compileErrors:
					j.api.Notifications().Warn("The Java build finished with errors; they are in the Problems view.")
				case compileAbort:
					j.api.Notifications().Info("The Java build was cancelled.")
				default:
					j.api.Notifications().Info("The Java build finished.")
				}
			})
		}()
	}
	if full, ok := args.(bool); ok {
		run(full)
		return
	}
	err := j.api.QuickPick().Show(extension.QuickPickOptions{
		Title: what, Placeholder: "Choose the kind of build",
		Items: []extension.QuickPickItem{
			{Label: compileIncremental, Description: "builds what changed"},
			{Label: compileFull, Description: "cleans the projects and builds them all"},
		},
	}, func(n int) {
		if n >= 0 {
			run(n == 1)
		}
	})
	if err != nil && !benign(err) {
		j.api.Log().Warn("the choice of the build cannot be shown", "err", err)
	}
}

// updateProjectConfiguration is Update Project Configuration: jdtls reads the build files of the project of the URI (the command's
// argument, else the file in front) again.
func (j *java) updateProjectConfiguration(args any) {
	const what = "Update Project Configuration"
	uri, _ := args.(string)
	if uri == "" {
		doc, ok := j.api.Documents().Active()
		if !ok {
			j.api.Notifications().Info(what + " needs a Java file or a build file: open one first.")
			return
		}
		uri = doc.URI
	}
	if !strings.HasPrefix(uri, "file://") {
		j.api.Notifications().Info(what + " needs a file of the project: " + filepath.Base(uri) + " is not one.")
		return
	}
	if !j.isRunning(what) {
		return
	}
	// jdtls declares this as a notification (vscode-java sends it as one); asked as a request it answers "Internal error"
	if err := j.api.LanguageServers().Notify(ServerID, projectConfigUpdateMethod, map[string]any{"uri": uri}); err != nil {
		j.api.Log().Warn("jdtls cannot be told to update a project", "err", err)
		j.api.Notifications().Error(what + ": the language server failed: " + describe(err))
		return
	}
	j.api.Notifications().Info("The project configuration of " + filepath.Base(uri) + " is being updated.")
}

// organizeImports asks the server for the source action of the kind for the file in front and applies it.
func (j *java) organizeImports(ctx context.Context) {
	const what = "Organize Imports"
	doc, ok := j.api.Documents().Active()
	if !ok || doc.LanguageID != "java" || !strings.HasPrefix(doc.URI, "file://") {
		j.api.Notifications().Info(what + " needs a Java file: open one first.")
		return
	}
	if !j.isRunning(what) {
		return
	}
	req := extension.CodeActionRequest{Only: []string{kindOrganizeImports}}
	if len(doc.Selections) > 0 {
		req.Range = extension.Range{Start: doc.Selections[0].Anchor, End: doc.Selections[0].Active}
	}
	name := filepath.Base(doc.URI)
	err := j.api.LanguageServers().ApplyCodeAction(ctx, ServerID, doc.URI, req, func(title string, err error) {
		switch {
		case err != nil:
			j.api.Notifications().Error(what + ": the language server failed: " + describe(err))
		case title == "":
			j.api.Notifications().Info(what + ": nothing to change in " + name + ".")
		}
	})
	if err != nil {
		j.api.Notifications().Error(what + ": " + describe(err))
	}
}
