package gitscm

import (
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The guided setting of the name and the e-mail (git.requireGitUserConfig): a commit that git refuses because it does not know who
// the author is leads to two questions and to a confirmation that shows the exact commands. Emetos changes nothing in the global
// configuration of the user before they have said yes to those commands.
//
// Ported from VS Code: extensions/git/src/commands.ts (createCommand: the message for NoUserNameConfigured and NoUserEmailConfigured);
// the guided setting is Emetos's own (SPEC §10.30).

// identityMissing tells the user that git does not know the author, and offers to set the name and the e-mail. again runs the command
// that failed once they are set.
func (x *gitscm) identityMissing(r *repository, again func()) {
	x.api.Notifications().Error(`Make sure you configure your "user.name" and "user.email" in git.`,
		extension.NotificationAction{Label: "Set Name and E-mail...", Run: func() { x.askIdentity(r, again) }},
		extension.NotificationAction{Label: "Show Git Output", Run: func() { x.channel.Show(false) }})
}

// askIdentity asks for the name and the e-mail that git does not have, then shows the commands and asks before it runs them.
func (x *gitscm) askIdentity(r *repository, again func()) {
	if r.disposed || r.repo == nil {
		return
	}
	repo, ctx := r.repo, r.ctx
	go func() {
		name, hasName, nameErr := repo.ConfigValue(ctx, "user.name")
		email, hasEmail, emailErr := repo.ConfigValue(ctx, "user.email")
		x.api.Post(func() {
			if r.disposed {
				return
			}
			if nameErr != nil || emailErr != nil {
				x.logLine("cannot read the user of git: " + describe(firstErr(nameErr, emailErr)))
			}
			c := &identityAsk{x: x, again: again}
			if hasName && strings.TrimSpace(name) != "" {
				c.name = strings.TrimSpace(name)
			}
			if hasEmail && strings.TrimSpace(email) != "" {
				c.email = strings.TrimSpace(email)
			}
			c.askName()
		})
	}()
}

// firstErr is the first error that is not nil.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// identityAsk is the questions of the guided setting; name and email are what git has already, or what the user typed.
type identityAsk struct {
	x           *gitscm
	again       func()
	name, email string
	setName     bool
	setEmail    bool
}

// askName asks for the name when git has none, then goes to the e-mail.
func (c *identityAsk) askName() {
	if c.name != "" {
		c.askEmail()
		return
	}
	err := c.x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Your name", Message: "Git: the name for your commits (user.name)", IgnoreFocusOut: true,
		Validate: func(v string) string {
			if strings.TrimSpace(v) == "" {
				return "Please provide your name"
			}
			return ""
		},
	}, func(value string, ok bool) {
		if !ok {
			return
		}
		c.name, c.setName = strings.TrimSpace(value), true
		c.askEmail()
	})
	if err != nil {
		c.x.logUnavailable("cannot ask for the name of the user", err)
	}
}

// askEmail asks for the e-mail when git has none, then goes to the confirmation.
func (c *identityAsk) askEmail() {
	if c.email != "" {
		c.confirm()
		return
	}
	err := c.x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Your e-mail address", Message: "Git: the e-mail address for your commits (user.email)", IgnoreFocusOut: true,
		Validate: func(v string) string {
			if v = strings.TrimSpace(v); v == "" || !strings.Contains(v, "@") {
				return "Please provide an e-mail address"
			}
			return ""
		},
	}, func(value string, ok bool) {
		if !ok {
			return
		}
		c.email, c.setEmail = strings.TrimSpace(value), true
		c.confirm()
	})
	if err != nil {
		c.x.logUnavailable("cannot ask for the e-mail of the user", err)
	}
}

// confirm shows the commands and runs them when the user agrees.
func (c *identityAsk) confirm() {
	x := c.x
	var commands []string
	var args [][]string
	if c.setName {
		commands = append(commands, "git config --global user.name "+shellQuote(c.name))
		args = append(args, []string{"config", "--global", "--", "user.name", c.name})
	}
	if c.setEmail {
		commands = append(commands, "git config --global user.email "+shellQuote(c.email))
		args = append(args, []string{"config", "--global", "--", "user.email", c.email})
	}
	if len(commands) == 0 {
		return // git has both: it was something else
	}
	x.ask(extension.DialogSpec{
		Type:    extension.DialogQuestion,
		Message: "Set your name and e-mail for Git?",
		Detail:  "Emetos will run these commands, which change your global Git configuration:\n\n" + strings.Join(commands, "\n"),
		Buttons: []extension.DialogButton{{Label: "Run the Commands"}, cancelButton},
	}, func(res extension.DialogResult) {
		if res.Cancelled || res.Button != 0 || x.runner == nil {
			return
		}
		runner := x.runner
		go func() {
			var err error
			for _, a := range args {
				if _, err = runner.Run(x.ctx, git.Command{Args: a}); err != nil {
					break
				}
			}
			x.api.Post(func() {
				if err != nil {
					x.api.Notifications().Error("Git could not set your name and e-mail: " + git.MaskURLs(describe(err)))
					return
				}
				var actions []extension.NotificationAction
				if c.again != nil {
					actions = append(actions, extension.NotificationAction{Label: "Commit Again", Run: c.again})
				}
				x.api.Notifications().Info("Your name and e-mail are set in the global configuration of Git.", actions...)
			})
		}()
	})
}

// shellQuote quotes a word for a shell, to show a command the way it can be typed.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@%+=:,./-_") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
