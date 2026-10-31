package gitscm

import (
	"context"
	"regexp"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The protected branch and the name of the branch a commit goes to instead (git.branchProtection, git.branchProtectionPrompt), and the
// prompt for the name of a new branch.
//
// TODO(step 24.13): the branch commands share this prompt; the random names (git.branchRandomName.*) and the regenerate button come with
// them.
//
// Ported from VS Code: extensions/git/src/commands.ts (smartCommit: the branch protection hook, promptForBranchName, sanitizeBranchName)

// reBranchUnsafe finds what a branch name may not have: a leading dot, "/.", "..", "~", "^", ":", a trailing slash, ".lock" at the end of a
// name or of a folder, a backslash, "*", white space, an empty name, a trailing dot, "[" and a trailing "]".
var reBranchUnsafe = regexp.MustCompile(`^\.|/\.|\.\.|~|\^|:|/$|\.lock$|\.lock/|\\|\*|\s|^\s*$|\.$|\[|\]$`)

// reLeadingDashes finds the dashes at the start of a name.
var reLeadingDashes = regexp.MustCompile(`^-+`)

// sanitizeBranchName makes a name that git accepts: it is trimmed, loses its leading dashes, and each part git refuses is replaced by the
// white space character of the settings.
func sanitizeBranchName(name, whitespace string) string {
	if name == "" {
		return name
	}
	name = reLeadingDashes.ReplaceAllString(strings.TrimSpace(name), "")
	return reBranchUnsafe.ReplaceAllString(name, whitespace)
}

// branchProtection is the step of the commit that asks what to do when the branch is protected.
func (c *commitRun) branchProtection(next func()) {
	x, r := c.x, c.r
	prompt := x.cfg.branchProtectionPrompt
	if !r.isBranchProtected() || (prompt != "alwaysPrompt" && prompt != "alwaysCommitToNewBranch") {
		next()
		return
	}
	toNewBranch := func() {
		x.askBranchName(r, func(name string) {
			r.mutate(mutation{
				name: "branch",
				run:  func(ctx context.Context, repo *git.Repo) error { return repo.CreateBranch(ctx, name) },
				after: func() {
					next()
				},
			})
		})
	}
	if prompt == "alwaysCommitToNewBranch" {
		toNewBranch()
		return
	}
	x.ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "You are trying to commit to a protected branch.", Detail: "How would you like to proceed?",
		Buttons: []extension.DialogButton{{Label: "Commit to a New Branch"}, {Label: "Commit Anyway"}, cancelButton},
	}, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || res.Button == 2:
		case res.Button == 1:
			next()
		default:
			toNewBranch()
		}
	})
}

// askBranchName asks for the name of a new branch in an input box that checks it as it is typed, and calls done with the name made safe.
// Nothing is called when the box is closed without a name.
func (x *gitscm) askBranchName(r *repository, done func(name string)) {
	whitespace := read(x.api, "git.branchWhitespaceChar", "-")
	validation := read(x.api, "git.branchValidationRegex", "")
	existing := map[string]bool{}
	for _, ref := range r.refs {
		if ref.Type == git.RefHead {
			existing[ref.Name] = true
		}
	}
	var valid *regexp.Regexp
	if validation != "" {
		if re, err := regexp.Compile(validation); err == nil {
			valid = re
		}
	}
	check := func(value string) string {
		name := sanitizeBranchName(value, whitespace)
		switch {
		case strings.TrimSpace(value) == "":
			return "Please provide a new branch name"
		case existing[name]:
			return `Branch "` + name + `" already exists`
		case valid != nil && !valid.MatchString(name):
			return "Branch name needs to match regex: " + validation
		}
		return ""
	}
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Branch name", Message: "Please provide a new branch name", Value: read(x.api, "git.branchPrefix", ""), IgnoreFocusOut: true, Validate: check,
	}, func(value string, ok bool) {
		if !ok {
			return
		}
		if strings.TrimSpace(value) != "" {
			done(sanitizeBranchName(value, whitespace))
		}
	})
	if err != nil {
		x.logUnavailable("cannot ask for the name of a branch", err)
	}
}
