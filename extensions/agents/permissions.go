package agents

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// The permission requests of the program (SPEC §10.16 "As built (step 13.3)"): a can_use_tool request asks whether the agent may
// use a tool. The adapter calls canUseTool from a goroutine of its own and waits for the answer; the request is queued in the
// tab, and the first of the queue is the one the user is asked: the diff editor for an edit (Edit, Write, NotebookEdit), the
// plan for ExitPlanMode, the questions for AskUserQuestion, and a dialog with the command or the address for any other tool.

// permKind says how a request is shown.
type permKind int

const (
	// permTool is a dialog with the input of the tool: Allow once, Allow for this session, Deny.
	permTool permKind = iota
	// permEdit is an Edit, Write or NotebookEdit shown as a diff in the editor.
	permEdit
	// permPlan is ExitPlanMode: the plan, to approve or to keep planning.
	permPlan
	// permQuestion is AskUserQuestion.
	permQuestion
)

// permission is a request that waits for the user.
type permission struct {
	kind  permKind
	req   claude.ToolRequest
	input map[string]any
	// reply gets the decision, once; the goroutine of canUseTool waits for it. It is buffered, so that settling never waits.
	reply chan claude.Decision
	// started says that the user is being asked (the request is the first of the queue); settled that it was answered or
	// withdrawn.
	started, settled bool
	// cancel ends the diff of an edit that waits for the user.
	cancel context.CancelFunc
	// message is the box for what the user tells the agent with a denial (or a rejected edit).
	message *ui.InputBoxController
	// note says why a request is a dialog although its tool has a better view (the edit that cannot be shown as a diff).
	note string
	// plan is the text of an ExitPlanMode and scroll scrolls the panel.
	plan   string
	scroll ui.ScrollHandle
	// questions are the questions of an AskUserQuestion.
	questions []*question
}

// question is one question of an AskUserQuestion with the answer being made.
type question struct {
	text, header string
	multi        bool
	options      []questionOption
	picked       map[int]bool
	other        *ui.InputBoxController
}

// questionOption is a choice of a question.
type questionOption struct{ label, description string }

// classify says how a tool is shown.
func classify(tool string) permKind {
	switch {
	case isEditTool(tool):
		return permEdit
	case tool == "ExitPlanMode":
		return permPlan
	case tool == "AskUserQuestion":
		return permQuestion
	}
	return permTool
}

// canUseTool answers a permission request of the program. It runs on a goroutine of the adapter: it hands the request to the
// UI thread and waits for the decision. When the program takes the request back (ctx ends) or the session ends, the request
// leaves the tab and nothing is answered.
func (t *sessionTab) canUseTool(ctx context.Context, gen int, req claude.ToolRequest) (claude.Decision, error) {
	p := &permission{kind: classify(req.ToolName), req: req, reply: make(chan claude.Decision, 1), message: ui.NewInputBoxController("")}
	a := t.ext
	a.api.Post(func() { t.addPermission(gen, p) })
	select {
	case d := <-p.reply:
		return d, nil
	case <-ctx.Done():
		a.api.Post(func() { t.withdraw(p) })
		return claude.Decision{}, errors.NewErr("PermissionWithdrawn", ctx.Err()).SetMetadata("tool", req.ToolName)
	}
}

// addPermission queues a request on the UI thread. A request of a program that is gone is denied.
func (t *sessionTab) addPermission(gen int, p *permission) {
	if t.closed || gen != t.gen {
		p.reply <- claude.Decision{Message: "The session is over."}
		return
	}
	if p.settled {
		return // withdrawn before it came
	}
	if err := json.Unmarshal(p.req.Input, &p.input); err != nil || p.input == nil {
		p.input = map[string]any{}
	}
	switch p.kind {
	case permPlan:
		p.plan, _ = p.input["plan"].(string)
	case permQuestion:
		p.questions = readQuestions(p.input)
		if len(p.questions) == 0 {
			p.kind = permTool // nothing to ask: the dialog shows the input
		}
	}
	// the handlers of the program's requests run on goroutines of their own, so a request can be here before an earlier one: it
	// is put in the order they were sent, unless the one before it is a diff that is open already
	at := len(t.perms)
	for at > 0 && t.perms[at-1].req.Seq > p.req.Seq && t.perms[at-1].kind != permEdit {
		at--
		t.perms[at].started = false // a dialog that was first only by the race has not been asked
	}
	t.perms = slices.Insert(t.perms, at, p)
	t.ext.logf("[%s] %s asks for permission", shortID(t.id), p.req.ToolName)
	if p.kind == permQuestion || p.kind == permPlan || len(t.perms) == 1 {
		t.stick = true
	}
	t.notifyWaiting(p)
	t.startNext()
	t.ext.sync()
	t.changed()
}

// startNext starts asking the first request of the queue, if it is not being asked yet.
func (t *sessionTab) startNext() {
	if len(t.perms) == 0 {
		return
	}
	if p := t.perms[0]; !p.started {
		p.started = true
		if p.kind == permEdit {
			t.reviewEdit(p)
		}
	}
}

// current is the request the user is asked, or nil.
func (t *sessionTab) current() *permission {
	if len(t.perms) == 0 {
		return nil
	}
	return t.perms[0]
}

// withdraw takes back a request that the program cancelled or whose session ended; nothing is answered.
func (t *sessionTab) withdraw(p *permission) {
	if p.settled {
		return
	}
	p.settled = true
	if p.cancel != nil {
		p.cancel() // the diff of an edit closes
	}
	if i := slices.Index(t.perms, p); i >= 0 {
		t.perms = slices.Delete(t.perms, i, i+1)
		if !t.closed {
			t.tr.add(&item{Kind: kindNotice, Text: "The request to use " + p.req.ToolName + " was withdrawn."})
		}
	}
	if t.closed {
		return
	}
	t.startNext()
	t.ext.sync()
	t.changed()
}

// settle answers a request with the decision and goes on to the next, with a line in the conversation.
func (t *sessionTab) settle(p *permission, d claude.Decision, line string) {
	if p.settled {
		return
	}
	p.settled = true
	if p.cancel != nil {
		p.cancel()
	}
	p.reply <- d
	if i := slices.Index(t.perms, p); i >= 0 {
		t.perms = slices.Delete(t.perms, i, i+1)
	}
	if t.replay {
		t.ext.api.Log().Info("replay: a permission request was answered", "tool", p.req.ToolName, "allow", d.Allow, "message", d.Message)
	}
	verb := "Denied"
	if d.Allow {
		verb = "Allowed"
	}
	t.ext.logf("[%s] %s %s", shortID(t.id), strings.ToLower(verb), p.req.ToolName)
	if line != "" {
		t.tr.add(&item{Kind: kindNotice, Text: line})
	}
	t.startNext()
	t.ext.sync()
	t.changed()
}

// what is a short phrase for the use of a tool in the conversation: the tool and what the line of its call says.
func what(p *permission, folder string) string {
	s := toolSummary(p.req.ToolName, p.req.Input, folder)
	if s == "" {
		return p.req.ToolName
	}
	return p.req.ToolName + " " + s
}

// reply texts
const (
	deniedByUser   = "The user denied this tool use."
	rejectedByUser = "The user rejected the proposed edit."
)

// withReason adds what the user typed to the text of a denial.
func withReason(text, typed string) string {
	if typed = strings.TrimSpace(typed); typed != "" {
		return text + " " + typed
	}
	return text
}

// allowOnce lets the tool run this once.
func (t *sessionTab) allowOnce(p *permission) {
	t.settle(p, claude.Decision{Allow: true}, "Allowed "+what(p, t.tr.folder)+".")
}

// allowSession lets the tool run, with the rule changes the program suggested saved for this session only.
func (t *sessionTab) allowSession(p *permission) {
	var updates []claude.PermissionUpdate
	for _, s := range p.req.Suggestions {
		updates = append(updates, s.WithDestination("session"))
	}
	t.settle(p, claude.Decision{Allow: true, UpdatedPermissions: updates}, "Allowed "+what(p, t.tr.folder)+" for this session.")
}

// deny refuses the tool; what the user typed in the box goes to the agent.
func (t *sessionTab) deny(p *permission) {
	t.settle(p, claude.Decision{Message: withReason(deniedByUser, p.message.Value())}, "Denied "+what(p, t.tr.folder)+".")
}

// approvePlan lets the agent leave plan mode and start. The program goes back to the mode it was in before the plan, which
// the picker shows.
func (t *sessionTab) approvePlan(p *permission) {
	if t.mode == claude.ModePlan {
		t.mode = defaultMode
		t.ext.touch(t)
	}
	t.settle(p, claude.Decision{Allow: true}, "Approved the plan.")
}

// keepPlanning refuses the plan: the agent stays in plan mode and gets what the user typed.
func (t *sessionTab) keepPlanning(p *permission) {
	t.settle(p, claude.Decision{Message: withReason("The user wants to keep planning; the plan is not approved.", p.message.Value())}, "Kept planning.")
}

// readQuestions reads the questions of the input of AskUserQuestion: {"questions": [{question, header, multiSelect, options:
// [{label, description}]}]}.
func readQuestions(input map[string]any) []*question {
	list, _ := input["questions"].([]any)
	var out []*question
	for _, raw := range list {
		m, _ := raw.(map[string]any)
		text, _ := m["question"].(string)
		if text == "" {
			continue
		}
		q := &question{text: text, picked: map[int]bool{}, other: ui.NewInputBoxController("")}
		q.header, _ = m["header"].(string)
		q.multi, _ = m["multiSelect"].(bool)
		opts, _ := m["options"].([]any)
		for _, o := range opts {
			om, _ := o.(map[string]any)
			label, _ := om["label"].(string)
			if label == "" {
				continue
			}
			desc, _ := om["description"].(string)
			q.options = append(q.options, questionOption{label: label, description: desc})
		}
		out = append(out, q)
	}
	return out
}

// pick chooses or unchooses an option; a question with a single choice keeps one.
func (q *question) pick(i int, on bool) {
	if !q.multi {
		q.picked = map[int]bool{}
	}
	if on {
		q.picked[i] = true
	} else {
		delete(q.picked, i)
	}
}

// answer is the answer to the question: the labels of the chosen options, then what the user typed besides them; empty when
// there is none.
func (q *question) answer() string {
	var parts []string
	for i, o := range q.options {
		if q.picked[i] {
			parts = append(parts, o.label)
		}
	}
	if other := strings.TrimSpace(q.other.Value()); other != "" {
		parts = append(parts, other)
	}
	return strings.Join(parts, ", ")
}

// answered tells whether every question has an answer.
func (p *permission) answered() bool {
	for _, q := range p.questions {
		if q.answer() == "" {
			return false
		}
	}
	return true
}

// submitAnswers answers the questions: the tool gets its input back with the answers by question text.
func (t *sessionTab) submitAnswers(p *permission) {
	if !p.answered() {
		return
	}
	answers := map[string]any{}
	for _, q := range p.questions {
		answers[q.text] = q.answer()
	}
	input := cloneInput(p.input)
	input["answers"] = answers
	data, err := marshalInput(input)
	if err != nil {
		t.settle(p, claude.Decision{Message: "The answers could not be sent: " + err.Error()}, "The answers could not be sent.")
		return
	}
	t.settle(p, claude.Decision{Allow: true, UpdatedInput: data}, "Answered the agent's question.")
}

// skipQuestions refuses to answer.
func (t *sessionTab) skipQuestions(p *permission) {
	t.settle(p, claude.Decision{Message: withReason("The user chose not to answer the questions.", p.message.Value())}, "Skipped the agent's question.")
}

// ---- edits ----

// reviewEdit shows the edit of a request as a diff, on a goroutine of its own: it reads the file, works out the proposed text,
// opens the diff and waits for the user. The answer comes back on the UI thread.
func (t *sessionTab) reviewEdit(p *permission) {
	a := t.ext
	ctx, cancel := context.WithCancel(a.ctx)
	p.cancel = cancel
	go func() {
		plan, err := a.planEdit(ctx, p.req)
		if err != nil {
			a.api.Post(func() { t.editUnavailable(p, err) })
			return
		}
		res, err := a.api.Editors().OpenProposedChange(ctx, extension.ProposedChange{
			URI: plan.uri, Original: plan.original, Proposed: plan.proposed, Title: plan.title, Language: plan.language,
		})
		a.api.Post(func() {
			switch {
			case ctx.Err() != nil:
				// withdrawn: nothing to do
			case err != nil:
				t.editUnavailable(p, err)
			default:
				t.editAnswered(p, plan, res)
			}
		})
	}()
}

// editUnavailable handles an edit that cannot be shown as a diff. An edit the tool could not do is denied with the reason;
// any other (a file outside the folder, one a deny rule excludes, a host that shows no diff) becomes the dialog of the tool
// with the reason above it.
func (t *sessionTab) editUnavailable(p *permission, err error) {
	if p.settled {
		return
	}
	reason := err.Error()
	if appErr, ok := errors.FromError(err); ok {
		switch appErr.Tag {
		case "EditOldStringMissing", "EditOldStringAmbiguous", "EditFileMissing", "NotebookCellMissing":
			t.ext.logf("[%s] %s: %s", shortID(t.id), p.req.ToolName, reason)
			t.settle(p, claude.Decision{Message: "The edit cannot be applied: " + reason}, "Denied "+what(p, t.tr.folder)+": "+reason+".")
			return
		}
		reason = appErr.Message
	}
	t.ext.logf("[%s] %s is shown as a dialog: %s", shortID(t.id), p.req.ToolName, reason)
	p.kind, p.note = permTool, "Emetos cannot show this change as a diff: "+reason
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	t.changed()
}

// editAnswered turns the answer of the diff editor into the decision.
func (t *sessionTab) editAnswered(p *permission, plan *editPlan, res extension.ProposedResult) {
	if p.settled {
		return
	}
	line := what(p, t.tr.folder)
	if !res.Accepted {
		t.settle(p, claude.Decision{Message: withReason(rejectedByUser, p.message.Value())}, "Rejected "+line+".")
		return
	}
	d := claude.Decision{Allow: true}
	if res.Edited {
		input, err := plan.rebuild(res.Text)
		switch {
		case err != nil:
			t.settle(p, claude.Decision{Message: "The changed edit could not be sent: " + err.Error()}, "The changed edit could not be sent: "+err.Error())
			return
		case input == nil:
			t.settle(p, claude.Decision{Message: "The user removed every change from the proposed edit."}, "Rejected "+line+": nothing was left to apply.")
			return
		}
		d.UpdatedInput = input
		t.settle(p, d, "Accepted "+line+" with changes.")
		return
	}
	t.settle(p, d, "Accepted "+line+".")
}
