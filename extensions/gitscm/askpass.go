package gitscm

import (
	"context"
	"os"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
)

// Passwords and editors (SPEC §10.30 "Passwords and editors", D35): git asks a program for a user name, a password or a passphrase
// (GIT_ASKPASS, SSH_ASKPASS) and for an editor (GIT_EDITOR). The programs are scripts that start Emetos's own binary, which sends the
// request over a Unix socket (extensions/gitscm/helper); this file is the server's side: it starts the server on the first command that
// can ask, hands the requests to the user interface of the window tab, one prompt at a time, and answers. The typed secret goes from
// the input box to the reply and to git; it is not stored, not cached and not logged.
//
// Ported in idea from VS Code: extensions/git/src/askpass.ts, askpassManager.ts, gitEditor.ts and ipc/ipcServer.ts.

// helperCommands are the git commands that may ask for a secret or start an editor, so that the first of them starts the server and
// carries its variables. A background command never asks; add, reset, status and the like neither.
var helperCommands = map[string]bool{
	"fetch": true, "pull": true, "push": true, "clone": true, "ls-remote": true, "remote": true, "submodule": true, "checkout": true,
	"switch": true, "commit": true, "merge": true, "rebase": true, "cherry-pick": true, "revert": true, "tag": true, "am": true, "stash": true,
}

// helperEnv gives the variables of the askpass and editor programs to a command that may need them; the runner calls it on the goroutine
// of the command (git.Config.ExtraFor). The server is started by the first call. It gives nothing when the settings turn the
// programs off (git.useIntegratedAskPass, git.useEditorAsCommitInput) or the server cannot start (which is logged once).
func (x *gitscm) helperEnv(name string, background bool) []string {
	if background || !helperCommands[name] {
		return nil
	}
	askpass, editor := x.askpassOn.Load(), x.editorOn.Load()
	if !askpass && !editor {
		return nil
	}
	srv := x.helperServer()
	if srv == nil {
		return nil
	}
	return srv.Environment(askpass, editor)
}

// helperServer starts the server of the window tab when it is not running and returns it; nil when it cannot start. It may be called
// from any goroutine.
func (x *gitscm) helperServer() *helper.Server {
	x.helperMu.Lock()
	defer x.helperMu.Unlock()
	if x.ctx.Err() != nil {
		return nil
	}
	if x.helperSrv != nil || x.helperFailed {
		return x.helperSrv
	}
	runtime := x.opts.RuntimeDir
	if runtime == "" {
		runtime = os.Getenv("XDG_RUNTIME_DIR")
	}
	srv, err := helper.Listen(helper.ServerOptions{
		Root: helper.Root(runtime, x.stateDir), Binary: x.opts.HelperBinary, Handler: x.handleHelper, Logger: x.log,
	})
	if err != nil {
		x.helperFailed = true
		x.log.Warn("the program that answers git's questions cannot start; git will not ask for passwords in the window", "err", err)
		x.logLine("The program that answers git's questions cannot start: " + describe(err))
		return nil
	}
	x.helperSrv = srv
	go func() {
		<-x.ctx.Done()
		_ = srv.Close()
	}()
	return srv
}

// stopHelper closes the server and removes its socket, and no command starts another one. The window calls it before the process
// ends (emetos.workspace.closed is delivered before the extension is deactivated, and the process does not wait for the goroutines of
// an extension), so that nothing of the helper stays in the runtime folder.
func (x *gitscm) stopHelper(extension.Event) {
	x.helperMu.Lock()
	srv := x.helperSrv
	x.helperSrv, x.helperFailed = nil, true
	x.helperMu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

// handleHelper answers a request of git's programs: it hands it to the user interface on the UI thread and waits for the answer, for as
// long as the user takes. ctx ends when git went away.
func (x *gitscm) handleHelper(ctx context.Context, req helper.Request) helper.Reply {
	done := make(chan helper.Reply, 1)
	reply := func(r helper.Reply) {
		select {
		case done <- r:
		default:
		}
	}
	x.api.Post(func() {
		if !x.cfg.enabled {
			reply(helper.Reply{Error: "Git is turned off in the settings of Emetos"})
			return
		}
		switch req.Kind {
		case helper.KindAskpass, helper.KindSSHAskpass:
			x.queuePrompt(ctx, req, reply)
		case helper.KindEditor:
			x.openEditor(req, reply)
		default:
			reply(helper.Reply{Error: "Emetos does not know this kind of request"})
		}
	})
	select {
	case r := <-done:
		return r
	case <-ctx.Done():
		return helper.Reply{}
	}
}

// promptJob is a question waiting for the user.
type promptJob struct {
	ctx   context.Context
	req   helper.Request
	reply func(helper.Reply)
}

// queuePrompt asks the questions one at a time: a second command that asks while the box is open waits for its turn, since the box of the
// window is one. A question whose command has gone away is dropped.
func (x *gitscm) queuePrompt(ctx context.Context, req helper.Request, reply func(helper.Reply)) {
	x.prompts = append(x.prompts, promptJob{ctx: ctx, req: req, reply: reply})
	x.nextPrompt()
}

// nextPrompt shows the next question when none is shown.
func (x *gitscm) nextPrompt() {
	for !x.prompting && len(x.prompts) > 0 {
		job := x.prompts[0]
		x.prompts = x.prompts[1:]
		if job.ctx.Err() != nil {
			continue
		}
		x.prompting = true
		var once bool
		answer := func(r helper.Reply) {
			if once {
				return
			}
			once = true
			job.reply(r)
			x.prompting = false
			x.nextPrompt()
		}
		x.showPrompt(helper.ParsePrompt(job.req.Kind, job.req.Argv, job.req.SSHPrompt), job.req.Kind, answer)
	}
}

// showPrompt puts a question in front of the user and calls answer once with what they decided: a text, a yes or no, or a cancel. A
// box the window cannot show (a host without a workbench) cancels, without a word in the log.
func (x *gitscm) showPrompt(p helper.Prompt, kind helper.Kind, answer func(helper.Reply)) {
	cancel := helper.Reply{}
	var err error
	switch p.Kind {
	case helper.PromptNotice:
		x.api.Notifications().Info(p.Text)
		answer(helper.Reply{OK: true})
		return
	case helper.PromptConfirm:
		err = x.api.QuickPick().Show(confirmPick(p), func(i int) {
			switch i {
			case 0:
				answer(helper.Reply{OK: true, Value: "yes"})
			case 1:
				answer(helper.Reply{OK: true, Value: "no"})
			default:
				answer(cancel)
			}
		})
	default:
		err = x.api.InputBox().Show(inputFor(p, kind), func(value string, ok bool) {
			if !ok {
				answer(cancel)
				return
			}
			answer(helper.Reply{OK: true, Value: value})
		})
	}
	if err != nil {
		switch errTag(err) {
		case "ExtensionInputBoxUnavailable", "ExtensionQuickPickUnavailable", "ExtensionDeactivated":
		default:
			x.log.Warn("cannot ask the question of git", "err", err)
		}
		answer(cancel)
	}
}

// inputFor is the input box of a question: the placeholder is the question as git or ssh wrote it, the line under the input says where the
// answer goes ("Git: https://host", "SSH Key: /home/u/.ssh/id_ed25519"), the answer of a password has dots, and the box stays when the
// focus leaves it, because a program waits for it.
func inputFor(p helper.Prompt, kind helper.Kind) extension.InputBoxOptions {
	o := extension.InputBoxOptions{Prompt: p.Text, Password: p.Secret, IgnoreFocusOut: true}
	switch p.Kind {
	case helper.PromptUsername, helper.PromptPassword:
		if p.Target != "" {
			o.Message = "Git: " + p.Target
			if kind == helper.KindSSHAskpass {
				o.Message = "SSH: " + p.Target
			}
		}
	case helper.PromptPassphrase:
		o.Prompt = "Passphrase"
		if p.File != "" {
			o.Message = "SSH Key: " + p.File
		}
	}
	return o
}

// confirmPick is the list of the question about a host key: Yes and No, with the host and its fingerprint under them.
func confirmPick(p helper.Prompt) extension.QuickPickOptions {
	detail := ""
	switch {
	case p.Target != "" && p.Fingerprint != "":
		detail = `"` + p.Target + `" has fingerprint "` + p.Fingerprint + `"`
	case p.Target != "":
		detail = p.Target
	}
	return extension.QuickPickOptions{
		Placeholder:    "Are you sure you want to continue connecting?",
		IgnoreFocusOut: true,
		Items: []extension.QuickPickItem{
			{Label: "Yes", Detail: detail},
			{Label: "No", Detail: detail},
		},
	}
}
