package debug_test

import (
	"slices"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

func out(body map[string]any) daptest.Step { return daptest.Event("output", body) }

// replOf starts a session whose adapter sends the events when kicked and returns the session.
func replSession(t *testing.T, e *env, name string, caps map[string]any, tail ...daptest.Step) *debug.Session {
	t.Helper()
	e.script(name, join(handshake(caps, noBreakpoints(caps)), steps(tail...), ending()))
	sess, ok, err := e.start(fakeConfig(name))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	idle(t, sess)
	return sess
}

func waitRepl(t *testing.T, sess *debug.Session, what string, cond func(*debug.ReplModel) bool) {
	t.Helper()
	waitUntil(t, what, func() bool { return cond(sess.Repl()) })
}

func TestOutputEventsGoToTheConsole(t *testing.T) {
	e := newEnv(t, nil)
	sess := replSession(t, e, "console", fullCaps, kickWith(
		out(map[string]any{"category": "stdout", "output": "hello\n"}),
		out(map[string]any{"category": "stderr", "output": "oops\n", "source": map[string]any{"path": "/p/a.go", "name": "a.go"}, "line": 7}),
		out(map[string]any{"category": "console", "output": "adapter says\n"}),
		out(map[string]any{"category": "telemetry", "output": "not shown"}),
		out(map[string]any{"category": "stdout", "output": ""}),
		out(map[string]any{"category": "important", "output": "look at this\n"}), // a notification of the window, not a line
	)...)
	kick(t, sess)
	waitRepl(t, sess, "the lines", func(m *debug.ReplModel) bool { return m.Len() == 3 })
	els := outputs(sess.Repl())
	want := []struct {
		text string
		sev  debug.ReplSeverity
	}{{"hello\n", debug.ReplInfo}, {"oops\n", debug.ReplError}, {"adapter says\n", debug.ReplWarning}}
	for i, w := range want {
		if els[i].Value != w.text || els[i].Severity != w.sev {
			t.Errorf("line %d = %q %v, want %q %v", i, els[i].Value, els[i].Severity, w.text, w.sev)
		}
	}
	src := els[1].Src
	if src == nil || src.Line != 7 || src.Column != 1 || src.Source.Path != "/p/a.go" || els[0].Src != nil {
		t.Errorf("sources = %+v and %+v", src, els[0].Src)
	}
	if els[1].Session != sess {
		t.Error("the line does not know its session")
	}
}

func TestGroupsInTheConsole(t *testing.T) {
	e := newEnv(t, nil)
	sess := replSession(t, e, "groups", fullCaps, kickWith(
		out(map[string]any{"category": "stdout", "output": "before\n"}),
		out(map[string]any{"category": "stdout", "output": "Group A", "group": "start"}),
		out(map[string]any{"category": "stdout", "output": "inside\n"}),
		out(map[string]any{"category": "stdout", "output": "Inner", "group": "startCollapsed"}),
		out(map[string]any{"category": "stdout", "output": "deep\n"}),
		out(map[string]any{"category": "stdout", "group": "end"}),
		out(map[string]any{"category": "stdout", "output": "closing text\n", "group": "end"}),
		out(map[string]any{"category": "stdout", "output": "last\n"}),
	)...)
	kick(t, sess)
	waitRepl(t, sess, "all the lines", func(m *debug.ReplModel) bool {
		els := m.Elements()
		return len(els) > 0 && els[len(els)-1].Text(false) == "last\n"
	})
	els := sess.Repl().Elements()
	// before, Group A (inside, Inner (deep)), the text of the second end, last
	if len(els) != 4 {
		t.Fatalf("elements = %q", texts(sess.Repl()))
	}
	group, ok := els[1].(*debug.ReplGroup)
	if !ok || group.Name != "Group A" || !group.AutoExpand {
		t.Fatalf("second element = %T %+v", els[1], els[1])
	}
	kids := group.Children()
	if len(kids) != 2 || kids[0].Text(false) != "inside\n" {
		t.Fatalf("children of the group = %v", kids)
	}
	inner, ok := kids[1].(*debug.ReplGroup)
	if !ok || inner.Name != "Inner" || inner.AutoExpand || !inner.Ended() || len(inner.Children()) != 1 {
		t.Fatalf("inner group = %+v", kids[1])
	}
	// the second end carried text: it closes Group A, and the text comes after the group
	if !group.Ended() || els[2].Text(false) != "closing text\n" {
		t.Errorf("Group A ended %v, the text after it is %q", group.Ended(), els[2].Text(false))
	}
}

func TestObjectsInTheConsole(t *testing.T) {
	e := newEnv(t, nil)
	sess := replSession(t, e, "objects", fullCaps,
		append(kickWith(
			// an object with the text the adapter formatted, and one with no text
			out(map[string]any{"category": "stdout", "output": "\x1b[32mobj\x1b[0m\n", "variablesReference": 500}),
			out(map[string]any{"category": "stdout", "variablesReference": 501}),
		),
			daptest.ReplyArgs("variables", map[string]any{"variablesReference": 500}, map[string]any{"variables": []map[string]any{{"name": "k", "value": "v", "variablesReference": 510}}}),
			daptest.ReplyArgs("variables", map[string]any{"variablesReference": 501}, map[string]any{"variables": []map[string]any{{"name": "x", "value": "1"}}}),
		)...)
	kick(t, sess)
	waitRepl(t, sess, "both", func(m *debug.ReplModel) bool { return m.Len() == 2 })
	els := sess.Repl().Elements()
	// with a text the text wins, and the object behind it is the element's expression
	o, ok := els[0].(*debug.ReplOutput)
	if !ok || o.Value != "\x1b[32mobj\x1b[0m\n" || o.Expression == nil || o.Expression.ID() == "" {
		t.Fatalf("object output = %+v", els[0])
	}
	if x, ok := o.Expression.(*debug.Variable); !ok || x.Name != "k" || !o.HasChildren() {
		t.Errorf("the object behind the text = %+v", o.Expression)
	}
	// with no text the one child is the element
	v, ok := els[1].(*debug.ReplVariable)
	if !ok {
		t.Fatalf("variable element = %T", els[1])
	}
	if x, ok := v.Expression.(*debug.Variable); !ok || x.Name != "x" || v.Text(false) != "x:1" || v.HasChildren() {
		t.Errorf("variable = %+v", v.Expression)
	}
}

func TestSeveralChildrenAreShownOneAfterTheOther(t *testing.T) {
	e := newEnv(t, nil)
	sess := replSession(t, e, "several", fullCaps,
		append(kickWith(
			out(map[string]any{"category": "stdout", "output": "ignored text\n", "variablesReference": 502}),
			out(map[string]any{"category": "stdout", "output": "empty object\n", "variablesReference": 503}),
			out(map[string]any{"category": "stdout", "output": "last\n"}),
		),
			daptest.ReplyArgs("variables", map[string]any{"variablesReference": 502}, map[string]any{"variables": []map[string]any{{"name": "a", "value": "1"}, {"name": "b", "value": "2"}}}),
			daptest.ReplyArgs("variables", map[string]any{"variablesReference": 503}, map[string]any{"variables": []map[string]any{}}),
		)...)
	kick(t, sess)
	waitRepl(t, sess, "all the lines", func(m *debug.ReplModel) bool {
		els := m.Elements()
		return len(els) > 0 && els[len(els)-1].Text(false) == "last\n"
	})
	els := sess.Repl().Elements()
	if len(els) != 4 {
		t.Fatalf("elements = %q", texts(sess.Repl()))
	}
	for i, want := range []string{"1", "2"} {
		v, ok := els[i].(*debug.ReplVariable)
		if !ok {
			t.Fatalf("element %d = %T", i, els[i])
		}
		if x, ok := v.Expression.(*debug.Variable); !ok || x.Name != "" || x.Value() != want {
			t.Errorf("variable %d = %+v (its name is cleared)", i, v.Expression)
		}
		if v.Text(false) != want {
			t.Errorf("text of variable %d = %q", i, v.Text(false))
		}
	}
	// an object with no children keeps the text the adapter gave
	if els[2].Text(false) != "empty object\n" {
		t.Errorf("element 2 = %q", els[2].Text(false))
	}
}

func TestOutputKeepsItsOrderWhileAnObjectLoads(t *testing.T) {
	e := newEnv(t, nil)
	sess := replSession(t, e, "order", fullCaps,
		append(kickWith(
			out(map[string]any{"category": "stdout", "output": "one\n"}),
			out(map[string]any{"category": "stdout", "output": "two\n", "variablesReference": 600}),
			out(map[string]any{"category": "stdout", "output": "three\n"}),
		),
			daptest.Delay(50),
			daptest.ReplyArgs("variables", map[string]any{"variablesReference": 600}, map[string]any{"variables": []map[string]any{{"name": "k", "value": "v"}}}),
		)...)
	kick(t, sess)
	waitRepl(t, sess, "three lines", func(m *debug.ReplModel) bool { return m.Len() == 3 })
	if got, want := texts(sess.Repl()), []string{"one\n", "two\n", "three\n"}; !slices.Equal(got, want) {
		t.Errorf("order = %q, want %q", got, want)
	}
}

func TestEvaluatingInTheConsole(t *testing.T) {
	e := newEnv(t, nil)
	sess := replSession(t, e, "eval", fullCaps,
		daptest.ReplyArgs("evaluate", map[string]any{"expression": "1+1", "context": "repl"}, map[string]any{"result": "2", "type": "int", "variablesReference": 0}),
		daptest.ReplyArgs("evaluate", map[string]any{"expression": "boom", "context": "repl"}, nil),
	)
	_ = sess
	var changes atomic.Int32
	sess.Repl().Subscribe(func() { changes.Add(1) })
	sess.AddReplExpression(ctxT(t), nil, "1+1")
	els := sess.Repl().Elements()
	if len(els) != 2 {
		t.Fatalf("elements = %d, want an input and a result", len(els))
	}
	in, ok := els[0].(*debug.ReplInput)
	if !ok || in.Value != "1+1" {
		t.Errorf("input = %+v", els[0])
	}
	res, ok := els[1].(*debug.ReplResult)
	if !ok || res.Text(false) != "2" || res.Original != "1+1" || !res.Available() || res.Expression.Type() != "int" {
		t.Errorf("result = %+v", els[1])
	}
	if changes.Load() < 2 {
		t.Errorf("the console told its listeners %d times", changes.Load())
	}
	if e.count(debug.EventRepl) < 2 {
		t.Errorf("the model got %d console events", e.count(debug.EventRepl))
	}
}

func TestAFailedEvaluationIsTheResult(t *testing.T) {
	e := newEnv(t, nil)
	sess := replSession(t, e, "fail", fullCaps,
		daptest.ReplyError("evaluate", "name 'boom' is not defined", nil),
	)
	sess.AddReplExpression(ctxT(t), nil, "boom")
	els := sess.Repl().Elements()
	if len(els) != 2 {
		t.Fatalf("elements = %d", len(els))
	}
	res, ok := els[1].(*debug.ReplResult)
	if !ok || res.Available() || res.Text(false) != "name 'boom' is not defined" {
		t.Errorf("result = %+v", els[1])
	}
}

func TestChildSessionsMergeOrNot(t *testing.T) {
	e := newEnv(t, nil)
	parent := replSession(t, e, "parent", fullCaps)
	e.script("merged", join(handshake(fullCaps, noBreakpoints(fullCaps)), ending()))
	e.script("separate", join(handshake(fullCaps, noBreakpoints(fullCaps)), ending()))
	e.script("default", join(handshake(fullCaps, noBreakpoints(fullCaps)), ending()))
	start := func(name, repl string) *debug.Session {
		t.Helper()
		ok, err := e.svc.StartDebugging(ctxT(t), nil, debug.Target{Config: fakeConfig(name)}, debug.SessionOptions{Parent: parent, Repl: repl})
		if err != nil || !ok {
			t.Fatalf("start %s: %v %v", name, ok, err)
		}
		ss := e.svc.Model().Sessions(false)
		return ss[len(ss)-1]
	}
	merged, separate, def := start("merged", "mergeWithParent"), start("separate", "separate"), start("default", "")
	if merged.Repl() != parent.Repl() || merged.HasSeparateRepl() || merged.ConsoleRoot() != parent {
		t.Error("a merged child does not share its parent's console")
	}
	if separate.Repl() == parent.Repl() || !separate.HasSeparateRepl() || separate.ConsoleRoot() != separate {
		t.Error("a separate child shares its parent's console")
	}
	if def.Repl() == parent.Repl() || !def.HasSeparateRepl() {
		t.Error("a child with no choice does not have its own console")
	}
	merged.AppendToRepl("from the child\n", debug.ReplInfo)
	if got := texts(parent.Repl()); !slices.Equal(got, []string{"from the child\n"}) {
		t.Errorf("the parent's console = %q", got)
	}
	if separate.Repl().Len() != 0 || def.Repl().Len() != 0 {
		t.Error("a line of the merged child reached a separate console")
	}
	// a mergeWithParent session with no parent has its own console
	e.script("lone", join(handshake(fullCaps, noBreakpoints(fullCaps)), ending()))
	if ok, err := e.svc.StartDebugging(ctxT(t), nil, debug.Target{Config: fakeConfig("lone")}, debug.SessionOptions{Repl: "mergeWithParent"}); err != nil || !ok {
		t.Fatalf("start lone: %v %v", ok, err)
	}
	ss := e.svc.Model().Sessions(false)
	if lone := ss[len(ss)-1]; !lone.HasSeparateRepl() || lone.Repl() == parent.Repl() {
		t.Error("a session with no parent shares a console")
	}
}

func TestAnExtensionWritesToTheFocusedConsole(t *testing.T) {
	e := newEnv(t, nil)
	// no session: the text is dropped, but the event of the model is sent
	e.svc.AppendConsole("nobody listens\n")
	if e.count(debug.EventOutput) != 1 {
		t.Errorf("output events = %d, want 1", e.count(debug.EventOutput))
	}
	sess := replSession(t, e, "ext", fullCaps)
	e.svc.AppendConsole("from an extension\n")
	waitRepl(t, sess, "the line", func(m *debug.ReplModel) bool { return m.Len() == 1 })
	o := outputs(sess.Repl())[0]
	if o.Value != "from an extension\n" || o.Severity != debug.ReplWarning {
		t.Errorf("line = %q %v", o.Value, o.Severity)
	}
}

func TestTheConsoleReadsItsSettings(t *testing.T) {
	collapse := atomic.Bool{}
	e := newEnv(t, func(o *debug.Options) {
		o.Console = func() debug.ReplConfig {
			return debug.ReplConfig{CollapseIdenticalLines: collapse.Load(), MaximumLines: 3}
		}
	})
	sess := replSession(t, e, "settings", fullCaps, kickWith(
		out(map[string]any{"output": "a\n"}), out(map[string]any{"output": "a\n"}),
		out(map[string]any{"output": "b\n"}), out(map[string]any{"output": "c\n"}), out(map[string]any{"output": "d\n"}),
	)...)
	kick(t, sess)
	waitRepl(t, sess, "the lines", func(m *debug.ReplModel) bool { return m.Len() == 3 && m.Elements()[2].Text(false) == "d\n" })
	if got, want := texts(sess.Repl()), []string{"b\n", "c\n", "d\n"}; !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q (three lines at most, no counting)", got, want)
	}
}
