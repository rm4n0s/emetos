package gitscm

import (
	"reflect"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The commit box and the action button of a repository (SPEC §10.30 "The input and the action button"): the text of the placeholder, the
// check of the message, the command Ctrl+Enter runs, the state of the box while a commit runs, and the button with its dropdown of commit
// commands and of the post-commit commands.
//
// Ported from VS Code: extensions/git/src/repository.ts (updateInputBoxPlaceholder, validateInput, the ProgressManager's input
// enablement), extensions/git/src/actionButton.ts, extensions/git/src/postCommitCommands.ts, extensions/git/src/diagnostics.ts
// (GitCommitInputBoxDiagnosticsManager) and extensions/git/src/branchProtection.ts

// actionButtonSettings is git.showActionButton: which of the three buttons are shown.
type actionButtonSettings struct {
	Commit, Publish, Sync bool
}

// commitUI is what a repository keeps for its box and its button.
type commitUI struct {
	// committing says that a commit command is running: the box and the button are disabled, git.commitInProgress is true.
	committing bool
	// commitQueue holds the commit commands that came meanwhile; they start once the commit ended and its status is in.
	commitQueue []func()
	// postStored is the post-commit command the action button chose; it stays while git.rememberPostCommitCommand says so (VS Code
	// keeps it in the global state of the window; here it lasts as long as the repository is open).
	postStored postCommitSpec
	// template is the text the box starts with (the commit template, or the message of a merge), as the last status read it.
	template string
	// rebaseCommit is the commit a rebase stopped at, as the last status read it ("" when none), and rebaseMessage its message: the box
	// shows it and warns when it is changed, since a rebase keeps the message of the commit it continues.
	rebaseCommit, rebaseMessage string
	// templateKey is what the template was read for: the commit it was read at and the state of the merge, so a status that changes
	// none of them does not read it again.
	templateKey string
	// syncing and checkingOut say that a sync (or push or pull) and a checkout are running, which disables the Sync and Publish buttons
	// and the branch item of the status bar. checkoutLabel is the ref the running checkout switches to, which the branch item shows.
	// TODO(step 24.15): the remote commands set syncing.
	syncing, checkingOut bool
	checkoutLabel        string
	// waiters run after the next status has been applied.
	waiters []func()
	// shown is the last placeholder and button given to the host, so that a status that changes neither does not send them again.
	shownPlaceholder string
	shownButton      *extension.ActionButton
	shownVisible     *bool
	shownEnabled     *bool
	// shownHuge is the limit of changes the check of the box was last told about.
	shownHuge int
}

// initCommitUI gives a new source control its accept command and, once the repository is open, its box and button.
func (r *repository) initCommitUI() {
	r.src.SetAcceptInputCommand(extension.Command{ID: "git.commit", Title: "Commit", Args: []any{r.sourceArg()}})
	r.src.Input().SetValidation(r.validateInput)
}

// sourceArg is the argument that names the source control of the repository, as the menus and buttons give it to a command.
func (r *repository) sourceArg() map[string]any {
	return map[string]any{extension.SCMArgSourceControl: r.handle}
}

// updateCommitUI gives the host the placeholder, the visibility and enablement of the box, and the button, as the state of the
// repository says; what has not changed is not sent again.
func (r *repository) updateCommitUI() {
	if r.disposed || r.src == nil || r.groups.merge == nil {
		return
	}
	in := r.src.Input()
	if p := r.placeholder(); p != r.shownPlaceholder {
		r.shownPlaceholder = p
		in.SetPlaceholder(p)
	}
	if visible := r.m.x.cfg.showCommitInput; r.shownVisible == nil || *r.shownVisible != visible {
		r.shownVisible = &visible
		in.SetVisible(visible)
	}
	if enabled := !r.committing; r.shownEnabled == nil || *r.shownEnabled != enabled {
		r.shownEnabled = &enabled
		in.SetEnabled(enabled)
	}
	if b := r.actionButton(); !reflect.DeepEqual(b, r.shownButton) {
		r.shownButton = b
		r.src.SetActionButton(b)
	}
}

// placeholder is the text shown in the empty box; {0} is where the host puts the key of the accept command. While an operation waits
// for the user it says that the key continues it (VS Code has no such text; SPEC §10.30 "In-progress states").
func (r *repository) placeholder() string {
	switch {
	case r.ops.rebaseCommit != "":
		return "Message ({0} to continue the rebase)"
	case r.ops.merge:
		return "Message ({0} to continue the merge)"
	case r.ops.cherryPick:
		return "Message ({0} to continue the cherry-pick)"
	case r.ops.revert:
		return "Message ({0} to continue the revert)"
	}
	if name := r.headShortName(); name != "" {
		return `Message ({0} to commit on "` + name + `")`
	}
	return "Message ({0} to commit)"
}

// headShortName is the name HEAD is shown with: the branch, else the tag on the commit, else the first 8 characters of the commit.
//
// Ported from VS Code: extensions/git/src/repository.ts (headShortName)
func (r *repository) headShortName() string {
	h := r.head
	switch {
	case h.name != "":
		return h.name
	case h.tag != "":
		return h.tag
	case len(h.commit) > 8:
		return h.commit[:8]
	}
	return h.commit
}

// setCommitting marks a commit as running or ended and shows it in the box, the button, the branch item and the context key
// git.commitInProgress.
func (r *repository) setCommitting(v bool) {
	r.committing = v
	r.updateCommitUI()
	r.updateStatusBar()
	r.m.updateContext()
}

// endCommit ends the commit and starts the commands that waited for it, once the status shows what the commit did.
func (r *repository) endCommit() {
	r.setCommitting(false)
	if len(r.commitQueue) == 0 {
		return
	}
	r.whenFresh(func() {
		queue := r.commitQueue
		r.commitQueue = nil
		for i, start := range queue {
			if r.committing {
				// the first of them began a commit: the rest wait for that one
				r.commitQueue = append(r.commitQueue, queue[i:]...)
				return
			}
			start()
		}
	})
}

// whenFresh calls fn once a status that was started now (or is running) has been applied. If the repository stops being open before, fn
// is not called.
func (r *repository) whenFresh(fn func()) {
	if r.disposed || r.repo == nil {
		return
	}
	r.waiters = append(r.waiters, fn)
	if !r.refreshing && !r.again {
		r.refresh()
	} else {
		r.again = true // the status that runs may have started before the change that is waited for
	}
}

// flushWaiters runs the functions that waited for a status.
func (r *repository) flushWaiters() {
	list := r.waiters
	r.waiters = nil
	for _, fn := range list {
		fn()
	}
}

// validateInput is the check of the text of the box (VS Code's validateInput, and the diagnostics of git.inputValidation): a repository with
// more changes than git.statusLimit says so, and with git.inputValidation a message that is only white space or has a line past the
// limits is reported as a warning.
func (r *repository) validateInput(text string) *extension.Validation {
	cfg := &r.m.x.cfg
	if r.hugeLimit > 0 {
		return &extension.Validation{
			Message: "Too many changes were detected. Only the first " + strconv.Itoa(r.hugeLimit) + " changes will be shown below.",
			Level:   extension.ValidationWarning,
		}
	}
	if r.rebaseCommit != "" && r.rebaseMessage != text {
		return &extension.Validation{
			Message: "It's not possible to change the commit message in the middle of a rebase. Please complete the rebase operation and use interactive rebase instead.",
			Level:   extension.ValidationWarning,
		}
	}
	if !cfg.inputValidation {
		return nil
	}
	if text != "" && strings.TrimSpace(text) == "" {
		return &extension.Validation{Message: "Current commit message only contains whitespace characters", Level: extension.ValidationWarning}
	}
	for i, line := range strings.Split(text, "\n") {
		threshold := cfg.inputValidationLength
		if i == 0 {
			threshold = cfg.inputValidationSubjectLength
		}
		over := len(utf16.Encode([]rune(strings.TrimSuffix(line, "\r")))) - threshold
		switch {
		case over == 1:
			return &extension.Validation{Message: "1 character over " + strconv.Itoa(threshold) + " in current line", Level: extension.ValidationWarning}
		case over > 1:
			return &extension.Validation{Message: strconv.Itoa(over) + " characters over " + strconv.Itoa(threshold) + " in current line", Level: extension.ValidationWarning}
		}
	}
	return nil
}

// revalidate makes the host check the text again: a setting or the state the check reads has changed.
func (r *repository) revalidate() {
	if r.disposed || r.src == nil {
		return
	}
	r.src.Input().SetValidation(r.validateInput)
}

// templateKeyOf is the key of the template a status reads: the template is read again when the commit,
// the merge or the rebase changed.
func templateKeyOf(headCommit string, ops operations) string {
	return headCommit + "|" + strconv.FormatBool(ops.merge) + "|" + strconv.FormatBool(ops.rebase) + "|" + strconv.FormatBool(ops.cherryPick) + "|" + strconv.FormatBool(ops.revert)
}

// applyTemplate puts a new template in the box when the box is empty or still holds the old template, as the Source Control view does
// with the commit template of a provider.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmInput.ts (the autorun of commitTemplate)
func (r *repository) applyTemplate(template string) {
	old := r.template
	r.template = template
	if template == old || r.disposed || r.src == nil {
		return
	}
	in := r.src.Input()
	if v := in.Value(); v == "" || v == old {
		in.SetValue(template)
	}
}

// ---- the action button ----

// hasChangesToCommit says whether there is something a commit would take: the staged changes, and the changes of the work tree when
// smart commit is on or suggested.
//
// Ported from VS Code: extensions/git/src/actionButton.ts (repositoryHasChangesToCommit)
func (r *repository) hasChangesToCommit() bool {
	cfg := &r.m.x.cfg
	n := len(r.res.index)
	if (cfg.enableSmartCommit && cfg.smartCommitChanges == "all") || (!cfg.enableSmartCommit && cfg.suggestSmartCommit) {
		n += len(r.res.working)
	}
	if cfg.enableSmartCommit && cfg.smartCommitChanges == "tracked" {
		for _, res := range r.res.working {
			if res.status != git.StatusUntracked {
				n++
			}
		}
	}
	return n != 0
}

// actionButton is the button under the box for the state of the repository: Commit when there is something to commit, else Publish Branch,
// else Sync Changes, else Commit again, disabled. It is nil before the first status and where the setting hides the button.
//
// Ported from VS Code: extensions/git/src/actionButton.ts (button)
func (r *repository) actionButton() *extension.ActionButton {
	if !r.refreshed || (r.head.name == "" && r.head.commit == "") {
		return nil
	}
	if r.hasChangesToCommit() {
		if b := r.commitButton(); b != nil {
			return b
		}
	}
	if b := r.publishButton(); b != nil {
		return b
	}
	if b := r.syncButton(); b != nil {
		return b
	}
	return r.commitButton()
}

// commitButton is the Commit button, Continue while an operation waits for the user.
//
// Ported from VS Code: extensions/git/src/actionButton.ts (getCommitActionButton; Continue for a cherry-pick and a revert is Emetos's)
func (r *repository) commitButton() *extension.ActionButton {
	cfg := &r.m.x.cfg
	if !cfg.showActionButton.Commit {
		return nil
	}
	unresolved := len(r.res.merge) > 0
	op, stopped := r.ops.current()
	enabled := (r.hasChangesToCommit() || (stopped && !unresolved)) && !r.committing
	b := &extension.ActionButton{Enabled: enabled}
	args := []any{r.sourceArg(), nil}
	switch {
	case stopped:
		what := map[git.Operation]string{
			git.OperationRebase: "Rebase", git.OperationMerge: "Merge", git.OperationCherryPick: "Cherry Pick", git.OperationRevert: "Revert",
		}[op]
		tooltip := "Continue " + what
		if r.committing {
			tooltip = "Continuing " + what + "..."
		}
		b.Command = extension.Command{ID: "git.commit", Title: "$(check) Continue", Tooltip: tooltip, Args: args}
	case r.head.name == "": // a tag or a detached HEAD
		tooltip := "Commit Changes"
		if r.committing {
			tooltip = "Committing Changes..."
		}
		b.Command = extension.Command{ID: "git.commit", Title: "$(check) Commit", Tooltip: tooltip, Args: args}
	default:
		b.Command = r.primaryPostCommit().command
		b.Secondary = [][]extension.Command{commandsOf(r.commitChoices()), commandsOf(r.providerChoices())}
	}
	return b
}

// publishButton is the Publish Branch button of a branch that has no upstream.
func (r *repository) publishButton() *extension.ActionButton {
	h := r.head
	if h.name == "" || h.upstream != nil || r.committing || r.ops.stopped() || !r.m.x.cfg.showActionButton.Publish {
		return nil
	}
	icon, tooltip := "$(cloud-upload)", `Publish Branch "`+h.name+`"`
	if r.syncing {
		icon, tooltip = "$(sync~spin)", `Publishing Branch "`+h.name+`"...`
	}
	return &extension.ActionButton{
		Command: extension.Command{ID: "git.publish", Title: icon + " Publish Branch", Tooltip: tooltip, Args: []any{r.sourceArg()}},
		Enabled: !r.checkingOut && !r.syncing,
	}
}

// syncButton is the Sync Changes button of a branch that is ahead of or behind its upstream.
func (r *repository) syncButton() *extension.ActionButton {
	h := r.head
	if h.upstream == nil || (h.ahead == 0 && h.behind == 0) || r.committing || r.ops.stopped() || !r.m.x.cfg.showActionButton.Sync {
		return nil
	}
	ahead, behind := "", ""
	if h.ahead > 0 {
		ahead = " " + strconv.Itoa(h.ahead) + "$(arrow-up)"
	}
	if h.behind > 0 {
		behind = " " + strconv.Itoa(h.behind) + "$(arrow-down)"
	}
	icon, tooltip := "$(sync)", r.syncTooltip()
	if r.syncing {
		icon, tooltip = "$(sync~spin)", "Synchronizing Changes..."
	}
	return &extension.ActionButton{
		Command: extension.Command{ID: "git.sync", Title: icon + " Sync Changes" + behind + ahead, Tooltip: tooltip, Args: []any{r.sourceArg()}},
		Enabled: !r.checkingOut && !r.syncing,
	}
}

// postCommitChoice is one command of the dropdown of the Commit button: a commit, with the command that follows it.
type postCommitChoice struct {
	spec    postCommitSpec
	command extension.Command
}

// commandsOf are the commands of the choices.
func commandsOf(list []postCommitChoice) []extension.Command {
	out := make([]extension.Command, len(list))
	for i, c := range list {
		out[i] = c.command
	}
	return out
}

// protection says how the commit button speaks of a protected branch: the lock when every commit asks, the branch when every commit goes
// to a new branch.
func (r *repository) protection() (alwaysPrompt, newBranch bool) {
	protected := r.isBranchProtected()
	prompt := r.m.x.cfg.branchProtectionPrompt
	return protected && prompt == "alwaysPrompt", protected && prompt == "alwaysCommitToNewBranch"
}

// commitChoices are Commit and Commit (Amend).
//
// Ported from VS Code: extensions/git/src/postCommitCommands.ts (getCommitCommands)
func (r *repository) commitChoices() []postCommitChoice {
	prompt, newBranch := r.protection()
	icon := "$(check)"
	switch {
	case prompt:
		icon = "$(lock)"
	case newBranch:
		icon = "$(git-branch)"
	}
	tooltip := "Commit Changes"
	switch {
	case newBranch:
		tooltip = "Commit Changes to New Branch"
	case r.head.name != "":
		tooltip = `Commit Changes on "` + r.head.name + `"`
	}
	if r.committing {
		tooltip = "Committing Changes..."
		if newBranch {
			tooltip = "Committing Changes to New Branch..."
		}
	}
	none := postCommitSpec{set: true}
	args := []any{r.sourceArg(), nil}
	return []postCommitChoice{
		{spec: none, command: extension.Command{ID: "git.commit", Title: icon + " Commit", Tooltip: tooltip, Args: args}},
		{spec: none, command: extension.Command{ID: "git.commitAmend", Title: icon + " Commit (Amend)", Tooltip: tooltip, Args: args}},
	}
}

// providerChoices are Commit & Push and Commit & Sync.
//
// Ported from VS Code: extensions/git/src/postCommitCommands.ts (GitPostCommitCommandsProvider.getCommands)
func (r *repository) providerChoices() []postCommitChoice {
	prompt, newBranch := r.protection()
	icon := ""
	switch {
	case r.committing:
		icon = "$(sync~spin)"
	case prompt:
		icon = "$(lock)"
	case newBranch:
		icon = "$(git-branch)"
	}
	pick := func(fallback string) string {
		if icon != "" {
			return icon
		}
		return fallback
	}
	push, sync := "Commit & Push Changes", "Commit & Sync Changes"
	switch {
	case r.committing && newBranch:
		push, sync = "Committing to New Branch & Pushing Changes...", "Committing to New Branch & Synchronizing Changes..."
	case r.committing:
		push, sync = "Committing & Pushing Changes...", "Committing & Synchronizing Changes..."
	case newBranch:
		push, sync = "Commit to New Branch & Push Changes", "Commit to New Branch & Synchronize Changes"
	}
	return []postCommitChoice{
		{spec: postCommitSpec{set: true, command: "git.push"}, command: extension.Command{
			ID: "git.commit", Title: pick("$(arrow-up)") + " Commit & Push", Tooltip: push, Args: []any{r.sourceArg(), "git.push"}}},
		{spec: postCommitSpec{set: true, command: "git.sync"}, command: extension.Command{
			ID: "git.commit", Title: pick("$(sync)") + " Commit & Sync", Tooltip: sync, Args: []any{r.sourceArg(), "git.sync"}}},
	}
}

// primaryPostCommit is the command the Commit button runs: the one chosen from its dropdown (while it is remembered), else the one
// git.postCommitCommand names, else Commit.
//
// Ported from VS Code: extensions/git/src/postCommitCommands.ts (getPrimaryCommand)
func (r *repository) primaryPostCommit() postCommitChoice {
	all := append(r.commitChoices(), r.providerChoices()...)
	if r.postStored.set {
		for _, c := range all {
			if c.spec == r.postStored {
				return c
			}
		}
	}
	if setting := r.m.x.cfg.postCommitCommand; setting == "push" || setting == "sync" {
		for _, c := range all {
			if c.spec.command == "git."+setting {
				return c
			}
		}
	}
	return all[0]
}

// ---- branch protection ----

// isBranchProtected says whether the branch HEAD is on matches a pattern of git.branchProtection.
//
// Ported from VS Code: extensions/git/src/repository.ts (isBranchProtected) and branchProtection.ts
func (r *repository) isBranchProtected() bool { return r.branchNameProtected(r.head.name) }

// branchNameProtected says whether a branch name matches a pattern of git.branchProtection.
func (r *repository) branchNameProtected(name string) bool {
	if name == "" {
		return false
	}
	var patterns []string
	for _, p := range r.m.x.cfg.branchProtection {
		if p = strings.TrimSpace(p); p != "" {
			patterns = append(patterns, p)
		}
	}
	return matchesAnyGlob(patterns, name)
}

// leaveRebase puts the box and the template right when the rebase that was stopped is over: the box is emptied (VS Code's setter of
// rebaseCommit) and the template is read again, which applyTemplate puts in the empty box.
//
// Ported from VS Code: extensions/git/src/repository.ts (set rebaseCommit)
func (r *repository) leaveRebase() {
	if r.rebaseCommit == "" || r.ops.rebaseCommit != "" || r.disposed || r.src == nil {
		return
	}
	r.rebaseCommit, r.rebaseMessage = "", ""
	r.template = ""
	r.src.Input().SetValue("")
	r.revalidate()
}

// enterRebase shows the message of the commit a rebase stopped at in the box when it is another commit than the last status saw.
//
// Ported from VS Code: extensions/git/src/repository.ts (set rebaseCommit)
func (r *repository) enterRebase(snap snapshot) {
	if r.ops.rebaseCommit == "" || r.disposed || r.src == nil {
		return
	}
	if r.ops.rebaseCommit == r.rebaseCommit {
		return
	}
	if !snap.rebaseRead {
		return
	}
	r.rebaseCommit, r.rebaseMessage = r.ops.rebaseCommit, snap.rebaseMessage
	r.src.Input().SetValue(r.rebaseMessage)
	r.revalidate()
}
