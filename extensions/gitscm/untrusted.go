package gitscm

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The Not Trusted item of the status bar (SPEC §10.30 "The status bar says it", step 35.1): while the window tab has a repository that
// is not trusted (the question not answered yet, or Don't Trust in this session), an item on the left, where the branch item of a
// trusted repository is, says so; a click asks the trust question again (git.trustRepository).

// The item.
const (
	// UntrustedItemID is the id of the item among the extension's.
	UntrustedItemID = "git.untrusted"
	// untrustedPriority is the priority of the branch item of the Source Control (VS Code's 10000, at the left end).
	untrustedPriority = 10000
)

// untrustedItem is the item for the repositories that wait for trust; ok is false when there are none.
func untrustedItem(roots []string) (item extension.StatusItem, ok bool) {
	if len(roots) == 0 {
		return extension.StatusItem{}, false
	}
	text := "$(workspace-untrusted) Not Trusted"
	tooltip := "Git runs nothing in " + roots[0] + " until you trust it. Click to decide."
	if len(roots) > 1 {
		text = "$(workspace-untrusted) " + strconv.Itoa(len(roots)) + " Not Trusted"
		tooltip = "Git runs nothing in these repositories until you trust them. Click to decide.\n" + strings.Join(roots, "\n")
	}
	return extension.StatusItem{
		ID: UntrustedItemID, Name: "Git Repository Trust", Text: text, Tooltip: tooltip, Alignment: extension.StatusLeft,
		Kind: extension.StatusWarning, Priority: untrustedPriority, Command: CommandTrustRepository,
	}, true
}

// updateUntrustedItem shows, changes or hides the item for the repositories that wait for trust. It shows nothing while git.enabled is
// false or git was not found.
func (x *gitscm) updateUntrustedItem(pending []*repository) {
	var roots []string
	if x.cfg().enabled && !x.missing {
		for _, r := range pending {
			roots = append(roots, r.root)
		}
	}
	item, ok := untrustedItem(roots)
	if !ok {
		if x.untrustedStatus != nil {
			x.untrustedStatus.SetVisible(false)
		}
		return
	}
	if x.untrustedStatus == nil {
		h, err := x.api.StatusBar().Add(item)
		if err != nil {
			if appErr, isOurs := errors.FromError(err); !isOurs || appErr.Tag != "ExtensionStatusBarUnavailable" {
				x.log.Warn("the Not Trusted item cannot be added to the status bar", "err", err)
			}
			return
		}
		x.untrustedStatus = h
		return
	}
	x.untrustedStatus.Update(item)
	x.untrustedStatus.SetVisible(true)
}
