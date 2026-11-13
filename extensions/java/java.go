// Package java is the Java language pack (SPEC §8.1, §10.15, §10.34): Java. The grammar and the language configuration are in assets/,
// copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE. Since step 24b.6 the pack also runs Eclipse JDT LS
// (jdtls): the server is found, started and shown by extensions/internal/langserver (the path setting java.jdt.ls.path, a jdtls on
// PATH, the status item, the notification for a missing server), java runs it (java.jdt.ls.java.home, JAVA_HOME or java on PATH, Java
// 21 or newer) with a workspace of its own in Emetos's cache folder, the settings of vscode-java that change the server are registered
// and sent to it, the classes of jars and of the JDK open as read-only documents (jdt:// locations, java/classFileContents), the status
// item follows the server's own notifications (language/status, language/progressReport), a question of the server (language/
// actionableNotification) is a notification with buttons, and the commands are Java: Restart Language Server, Clean Java Language
// Server Workspace, Force Java Compilation, Update Project Configuration and Organize Imports.
package java

import (
	"context"
	"embed"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

//go:embed all:assets
var assets embed.FS

// New is the Factory of the pack.
func New() extension.Extension {
	j := &java{}
	j.base = langpack.New(langpack.Spec{
		ID: ID, Name: "Java", Description: "Java language support: syntax, Eclipse JDT LS (jdtls) and its commands.",
		Assets: assets, Server: j.serverConfig(),
	})
	return j
}

// java is the extension of one scope. Everything in it runs on the UI thread unless a comment says otherwise.
type java struct {
	base extension.Extension
	api  extension.API
	ctx  context.Context
	ctl  *langserver.Controller

	// cleanNext says that the next start of the server deletes its workspace first (Clean Java Language Server Workspace).
	cleanNext atomic.Bool
	// mu guards workspace, which the launcher's goroutine writes (the folders of the last start), and trigger, which the UI thread
	// writes and the launcher reads.
	mu        sync.Mutex
	workspace workspacePaths
	// trigger is the Java file that was in front last, as a file: URI (workspace.go: initializationOptions.triggerFiles).
	trigger string
	// status follows the notifications of the server (status.go).
	status serverState
	// work waits for the requests in flight, so that a test can see that none is left.
	work sync.WaitGroup
}

// Manifest implements extension.Extension: the manifest of the pack with the commands, keybindings and settings of jdtls.
func (j *java) Manifest() extension.Manifest {
	m := j.base.Manifest()
	m.Contributes.Commands = append(m.Contributes.Commands, commandSpecs()...)
	m.Contributes.Keybindings = append(m.Contributes.Keybindings, keybindings()...)
	m.Contributes.Settings = append(m.Contributes.Settings, settingSpecs()...)
	m.Requirements = append(m.Requirements, requirements()...)
	return m
}

// Activate implements extension.Extension.
func (j *java) Activate(ctx context.Context, api extension.API) error {
	j.api, j.ctx = api, ctx
	if err := j.base.Activate(ctx, api); err != nil {
		return err
	}
	j.ctl = langpack.Controller(j.base)
	for id, handler := range j.handlers() {
		if err := api.Commands().Handle(id, handler); err != nil {
			return err
		}
	}
	j.provideContent()
	servers := api.LanguageServers()
	for method, fn := range map[string]func(json.RawMessage){
		statusMethod:            j.onStatus,
		progressMethod:          j.onProgress,
		actionableMethod:        j.onActionable,
		eventNotificationMethod: j.onEvent,
	} {
		if _, err := servers.OnNotification(ServerID, method, fn); err != nil {
			return err
		}
	}
	if _, err := servers.OnChange(ServerID, j.serverChanged); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicEditorActiveChanged, j.editorEvent); err != nil {
		return err
	}
	return nil
}

// editorEvent remembers the Java file that is in front: jdtls reads a folder with no build file as an invisible project and finds its
// source folders from the files it is told to look at (triggerFiles).
func (j *java) editorEvent(ev extension.Event) {
	var ae extension.ActiveEditor
	if ev.Decode(&ae) != nil || ae.LanguageID != "java" || !strings.HasPrefix(ae.URI, "file://") {
		return
	}
	j.mu.Lock()
	j.trigger = ae.URI
	j.mu.Unlock()
}

// wait waits for the requests the pack sent to end (the tests use it).
func (j *java) wait() { j.work.Wait() }
