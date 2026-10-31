package gitscm

import (
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/trust"
)

// The text of the trust question (SPEC §10.30 "Trusting a repository", D37).
const (
	trustDetail = "Git can run programs that a repository names in its configuration and hooks, for example when Emetos checks its status. " +
		"If you did not create this repository or do not know its source, do not trust it."
	trustButton     = "Trust Repository"
	dontTrustButton = "Don't Trust"
	maxProgramLines = 8
	maxProgramText  = 80
)

// trustDialog builds the dialog that asks whether to trust a repository: the path, the programs its configuration names (read by
// Emetos's own reader, which runs nothing) and the three answers. Escape is Don't Trust.
// programText is a value of the configuration as the trust dialog lists it: on one line, and cut after maxProgramText
// characters (not bytes, so that a letter is never cut in two) with "..." after it.
func programText(value string) string {
	v := []rune(strings.Join(strings.Fields(value), " "))
	if len(v) > maxProgramText {
		return string(v[:maxProgramText]) + "..."
	}
	return string(v)
}

func trustDialog(path string, programs []git.Program) extension.DialogSpec {
	detail := trustDetail
	if len(programs) > 0 {
		var b strings.Builder
		b.WriteString(detail + "\n\nThe configuration of this repository names these programs:")
		for i, p := range programs {
			if i == maxProgramLines {
				b.WriteString("\n    and " + strconv.Itoa(len(programs)-maxProgramLines) + " more")
				break
			}
			b.WriteString("\n    " + p.Key + " = " + programText(p.Value))
		}
		detail = b.String()
	}
	parent := trust.ParentOf(path)
	return extension.DialogSpec{
		Type:    extension.DialogWarning,
		Message: "Do you trust the authors of the Git repository in " + path + "?",
		Detail:  detail,
		Buttons: []extension.DialogButton{
			{Label: trustButton},
			{Label: "Trust All Repositories in " + baseName(parent)},
			{Label: dontTrustButton, Cancel: true},
		},
	}
}

// parentDialog asks about the parent folder of several repositories that wait together ("opening many repositories at once asks
// about the parent folder first").
func parentDialog(parent string, roots []string) extension.DialogSpec {
	var b strings.Builder
	b.WriteString(trustDetail + "\n\nThe repositories:")
	for i, r := range roots {
		if i == maxProgramLines {
			b.WriteString("\n    and " + strconv.Itoa(len(roots)-maxProgramLines) + " more")
			break
		}
		b.WriteString("\n    " + r)
	}
	return extension.DialogSpec{
		Type:    extension.DialogWarning,
		Message: "Do you trust the authors of the " + strconv.Itoa(len(roots)) + " Git repositories in " + parent + "?",
		Detail:  b.String(),
		Buttons: []extension.DialogButton{
			{Label: "Trust All Repositories in " + baseName(parent)},
			{Label: "Ask for Each Repository"},
			{Label: dontTrustButton, Cancel: true},
		},
	}
}

// ask queues the repository for the trust question and asks the next question when none is open. The question is asked once for each
// repository in a session (Store.Declined) and never while another dialog of the extension is open.
func (m *model) ask(r *repository) {
	if r.disposed || r.trusted || m.x.trust.Declined(r.root) {
		return
	}
	for _, q := range m.queue {
		if q == r {
			return
		}
	}
	m.queue = append(m.queue, r)
}

// askNext shows the next question, when there is one and no question is open.
func (m *model) askNext() {
	if m.asking {
		return
	}
	// drop what is answered already (a parent folder that was trusted, a repository that was closed)
	var live []*repository
	for _, r := range m.queue {
		if !r.disposed && !r.trusted && !m.x.trust.Declined(r.root) {
			live = append(live, r)
		}
	}
	m.queue = live
	if len(m.queue) == 0 {
		return
	}
	sort.SliceStable(m.queue, func(i, j int) bool { return m.queue[i].root < m.queue[j].root })
	first := m.queue[0]
	parent := trust.ParentOf(first.root)
	var siblings []string
	for _, r := range m.queue {
		if trust.ParentOf(r.root) == parent {
			siblings = append(siblings, r.root)
		}
	}
	if len(siblings) > 1 && !m.askedParent[parent] {
		m.askedParent[parent] = true
		m.asking = true
		m.dialog(parentDialog(parent, siblings), func(res extension.DialogResult) {
			m.asking = false
			switch res.Button {
			case 0:
				m.trustParent(parent, nil)
			case 2:
				for _, r := range m.queue {
					if trust.ParentOf(r.root) == parent {
						m.decline(r)
					}
				}
			}
			m.askNext()
		})
		return
	}
	m.asking = true
	programs := m.programsOf(first)
	m.dialog(trustDialog(first.root, programs), func(res extension.DialogResult) {
		m.asking = false
		switch {
		case res.Cancelled || res.Button == 2:
			m.decline(first)
		case res.Button == 0:
			m.trustRepository(first)
		case res.Button == 1:
			m.trustParent(trust.ParentOf(first.root), first)
		}
		m.askNext()
	})
}

// dialog asks a question and calls done with the answer. A scope that cannot show a dialog answers Don't Trust, so that nothing runs.
func (m *model) dialog(spec extension.DialogSpec, done func(extension.DialogResult)) {
	if err := m.x.api.Dialogs().Ask(spec, done); err != nil {
		m.x.log.Warn("cannot ask the trust question", "err", err)
		m.x.api.Post(func() { done(extension.DialogResult{Button: -1, Cancelled: true}) })
	}
}

// programsOf lists the programs the local configuration of the repository names; nothing is run to find them.
func (m *model) programsOf(r *repository) []git.Program {
	entries, err := git.ReadLocalConfig(r.loc)
	if err != nil {
		m.x.log.Warn("cannot read the configuration of a repository", "root", r.root, "err", err)
		return nil
	}
	return git.Programs(entries)
}

// decline keeps Don't Trust for the session and leaves the repository untrusted.
func (m *model) decline(r *repository) {
	m.x.trust.Decline(r.root)
	m.updateContext()
}

// trustRepository trusts one repository, keeps the answer, and starts the repository.
func (m *model) trustRepository(r *repository) {
	if err := m.x.trust.Trust(r.root); err != nil {
		m.saveFailed(err)
	}
	m.startTrusted(r)
}

// trustParent trusts every repository below a folder. asked is the repository the question was about, when it was about one: its
// dialog listed the programs of its configuration already.
func (m *model) trustParent(parent string, asked *repository) {
	if err := m.x.trust.TrustParent(parent); err != nil {
		m.saveFailed(err)
	}
	m.startTrusted(asked)
}

// saveFailed tells the user that the answer could not be saved: it holds until Emetos quits.
func (m *model) saveFailed(err error) {
	m.x.log.Warn("cannot save the answer to the trust question", "err", err)
	m.x.logLine("cannot save the trust file: " + err.Error())
	m.x.api.Notifications().Warn("The answer could not be saved, so it holds until Emetos quits: " + err.Error())
}

// startTrusted starts the repositories that the answers trust now. The user did not see the programs of the configuration of those
// that the answer covers without being asked about them (all but asked): the first start of each says so once.
func (m *model) startTrusted(asked *repository) {
	for _, r := range append([]*repository(nil), m.order...) {
		if !r.trusted && !r.disposed && m.x.trustedBy(r.loc) {
			if r != asked {
				m.notifyPrograms(r)
			}
			r.start()
		}
	}
	m.updateContext()
}

// untrusted lists the repositories that wait for an answer or were declined, in the order they were found.
func (m *model) untrusted() []*repository {
	var out []*repository
	for _, r := range m.order {
		if !r.trusted && !r.disposed {
			out = append(out, r)
		}
	}
	return out
}

// trustedBy says whether the user trusts a repository: its path, a parent folder, or the repository it belongs to as a submodule or a
// linked worktree.
func (x *gitscm) trustedBy(loc git.Location) bool { return git.TrustedBy(x.trust)(loc) }
