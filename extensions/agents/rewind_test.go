package agents

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// "Rewind code to here" (step 13.3), with the fake program restoring or refusing.

// controlRequests are the control requests with the subtype that the fake program got, in order.
func (r *rig) controlRequests(subtype string) []map[string]any {
	r.t.Helper()
	f, err := os.Open(r.log)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		var rec struct {
			Kind string `json:"kind"`
			Msg  struct {
				Type    string         `json:"type"`
				Request map[string]any `json:"request"`
			} `json:"msg"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Kind == "in" && rec.Msg.Type == "control_request" && rec.Msg.Request["subtype"] == subtype {
			out = append(out, rec.Msg.Request)
		}
	}
	return out
}

// answered runs a turn and returns the tab and its user message, which has the UUID the program knows it by.
func (r *rig) answered(message string) (*sessionTab, *item) {
	r.t.Helper()
	tab := r.open()
	r.ready(tab)
	r.send(tab, message)
	r.idle(tab)
	r.api.WaitFor("the UUID of the message", func() bool { return tab.tr.items[0].ID != "" })
	return tab, tab.tr.items[0]
}

func TestRewindAsksFirstAndThenRestoresTheFilesToTheMessage(t *testing.T) {
	r := newRig(t, "rewind.jsonl")
	tab, msg := r.answered("first")
	s := show(t, tab, uitest.Themes()[0], gui.Size{Width: 760, Height: 500})
	if s.ts.Exists(gluttertest.ByText("Restore the files the agent changed since this message? This overwrites them.")) {
		t.Fatal("the question is there before the button was pressed")
	}
	s.ts.Tap(gluttertest.ByText("Rewind code to here"))
	s.sync()
	if tab.rewinding != msg || !s.ts.Exists(gluttertest.ByText("Restore the files the agent changed since this message? This overwrites them.")) {
		t.Fatal("the button did not turn into the question")
	}
	// Cancel takes the question back and asks the program nothing
	s.ts.Tap(gluttertest.ByText("Cancel"))
	s.sync()
	if tab.rewinding != nil || !s.ts.Exists(gluttertest.ByText("Rewind code to here")) || len(r.controlRequests("rewind_files")) != 0 {
		t.Fatal("Cancel did not take the question back")
	}
	s.ts.Tap(gluttertest.ByText("Rewind code to here"))
	s.sync()
	s.ts.Tap(gluttertest.ByText("Rewind"))
	var reqs []map[string]any
	r.api.WaitFor("the rewind request", func() bool { reqs = r.controlRequests("rewind_files"); return len(reqs) == 1 })
	if reqs[0]["user_message_id"] != msg.ID {
		t.Errorf("the request names the message %v, want %q", reqs[0]["user_message_id"], msg.ID)
	}
	r.api.WaitFor("the result", func() bool {
		return slices.Contains(texts(tab), "notice:Restored 2 files to this message: main.go, util/util.go (+3 -1).")
	})
	if tab.restoring {
		t.Error("the tab still restores")
	}
}

func TestRewindShowsTheReasonOfAProgramThatCannotRestore(t *testing.T) {
	r := newRig(t, "rewind_refused.jsonl")
	tab, msg := r.answered("first")
	tab.rewind(msg)
	r.api.WaitFor("the reason", func() bool {
		return slices.Contains(texts(tab), "error:The files could not be restored: no checkpoint for that message")
	})
	if got := r.controlRequests("rewind_files"); len(got) != 1 {
		t.Errorf("the program got %d rewind requests", len(got))
	}
	lines := r.api.Channel(OutputChannel).Lines()
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "cannot restore the files: no checkpoint for that message")
	}) {
		t.Errorf("the channel has %q", lines)
	}
}

func TestRewindIsOnlyOfferedWhereItCanWork(t *testing.T) {
	r := newRig(t, "rewind.jsonl")
	tab, msg := r.answered("first")
	if !tab.canRewind(msg) {
		t.Fatal("an idle session with a running program cannot rewind")
	}
	tests := []struct {
		name   string
		break_ func()
		fix    func()
	}{
		{"a turn is running", func() { tab.busy = true }, func() { tab.busy = false }},
		{"a request waits", func() { tab.perms = []*permission{{}} }, func() { tab.perms = nil }},
		{"a rewind is under way", func() { tab.restoring = true }, func() { tab.restoring = false }},
		{"the program is not running", func() { tab.phase = phaseIdle }, func() { tab.phase = phaseReady }},
		{"the tab shows a recording", func() { tab.replay = true }, func() { tab.replay = false }},
		{"the message has no UUID", func() { msg.ID = "" }, func() { msg.ID = "u" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			saved := msg.ID
			tc.break_()
			if tab.canRewind(msg) {
				t.Error("rewind is offered")
			}
			tc.fix()
			msg.ID = saved
			if !tab.canRewind(msg) {
				t.Error("rewind is not offered again")
			}
		})
	}
	if tab.canRewind(&item{Kind: kindText, ID: "x"}) {
		t.Error("a message of the agent can be rewound to")
	}
}

func TestRewindWithoutAProgramSaysSo(t *testing.T) {
	r := newRig(t, "")
	tab := r.openIdle("")
	tab.rewind(&item{Kind: kindUser, ID: "u"})
	if got := texts(tab); !slices.Equal(got, []string{"error:The files cannot be restored: the agent is not running."}) {
		t.Errorf("the conversation is %q", got)
	}
}

func TestWhatARewindRestoredIsSaid(t *testing.T) {
	tests := []struct {
		res  claude.RewindResult
		want string
	}{
		{claude.RewindResult{CanRewind: true}, "Restored the files to this message; none was different."},
		{claude.RewindResult{CanRewind: true, FilesChanged: []string{"a.go"}}, "Restored 1 file to this message: a.go."},
		{claude.RewindResult{CanRewind: true, FilesChanged: []string{"a.go", "b.go"}, Insertions: 2, Deletions: 5}, "Restored 2 files to this message: a.go, b.go (+2 -5)."},
	}
	for _, tc := range tests {
		if got := restoredText(tc.res); got != tc.want {
			t.Errorf("restoredText(%+v) = %q, want %q", tc.res, got, tc.want)
		}
	}
}
