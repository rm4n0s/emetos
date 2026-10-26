package agents

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// "Rewind code to here" (SPEC §10.16 "As built (step 13.3)"): each message of the user that has a UUID has a button that asks the
// program (rewind_files) to restore the files the agent changed since that message. No capability names the file checkpoints, so
// the button is there whenever the program runs, which Emetos started with checkpointing on, and the program's reason is shown
// when it cannot. Because it overwrites files, the button asks to be pressed a second time.

// canRewind tells whether a message of the user can be rewound to: the program runs, no turn is in progress, no request waits,
// the tab shows no recording, and the message has the UUID the program knows it by.
func (t *sessionTab) canRewind(it *item) bool {
	return it.Kind == kindUser && it.ID != "" && !t.replay && t.sess != nil && t.phase == phaseReady && !t.busy && !t.waiting() && !t.restoring
}

// askRewind turns the button of a message into the question "Restore the files...?".
func (t *sessionTab) askRewind(it *item) {
	t.rewinding = it
	t.tab.Refresh()
}

// cancelRewind takes the question back.
func (t *sessionTab) cancelRewind() {
	t.rewinding = nil
	t.tab.Refresh()
}

// rewind asks the program to restore the files to their state at the message. The result is a line of the conversation.
func (t *sessionTab) rewind(it *item) {
	t.rewinding = nil
	sess := t.sess
	if sess == nil || it.ID == "" {
		t.problemNote("The files cannot be restored: the agent is not running.")
		return
	}
	a := t.ext
	a.logf("[%s] rewinding the files to the message %s", shortID(t.id), it.ID)
	t.restoring = true // one rewind at a time
	t.changed()
	go func() {
		res, err := sess.RewindFiles(a.ctx, it.ID)
		a.api.Post(func() {
			if t.closed {
				return
			}
			t.restoring = false
			t.rewound(res, err)
			t.changed()
		})
	}()
}

// rewound shows the result of a rewind.
func (t *sessionTab) rewound(res claude.RewindResult, err error) {
	switch {
	case err != nil:
		t.ext.logf("[%s] the rewind failed: %v", shortID(t.id), err)
		t.tr.add(&item{Kind: kindError, Text: "The files could not be restored: " + errorText(err)})
	case !res.CanRewind:
		reason := strings.TrimSpace(res.Error)
		if reason == "" {
			reason = "the agent gave no reason"
		}
		t.ext.logf("[%s] the program cannot restore the files: %s", shortID(t.id), reason)
		t.tr.add(&item{Kind: kindError, Text: "The files could not be restored: " + reason})
	default:
		t.ext.logf("[%s] the files were restored: %s", shortID(t.id), strings.Join(res.FilesChanged, ", "))
		t.tr.add(&item{Kind: kindNotice, Text: restoredText(res)})
	}
}

// restoredText says what a successful rewind did.
func restoredText(res claude.RewindResult) string {
	n := len(res.FilesChanged)
	var sb strings.Builder
	switch n {
	case 0:
		sb.WriteString("Restored the files to this message; none was different.")
	case 1:
		sb.WriteString("Restored 1 file to this message: " + res.FilesChanged[0])
	default:
		sb.WriteString("Restored " + strconv.Itoa(n) + " files to this message: " + strings.Join(res.FilesChanged, ", "))
	}
	if res.Insertions > 0 || res.Deletions > 0 {
		sb.WriteString(" (+" + strconv.Itoa(res.Insertions) + " -" + strconv.Itoa(res.Deletions) + ")")
	}
	if n > 0 {
		sb.WriteString(".")
	}
	return sb.String()
}
