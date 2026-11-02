package testing

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Test Results (SPEC §10.32, step 26.3): the panel view with the runs, newest first, their tests that took part and the messages of those,
// and beside the tree the text of what is selected in the ANSI text widget of the Debug Console: the output of a run, the output and the
// messages of a test, the details of a message (its text, the expected and the actual text, and the stack).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testResultsView/testResultsViewContent.ts, testResultsTree.ts and
// testResultsSubject.ts

// Keys of the widgets of Test Results, which tests find them by.
var (
	// ResultsTreeKey is the key of the tree, ResultsTextKey that of the text beside it and ResultsEmptyKey that of the message of a view
	// with no result.
	ResultsTreeKey  = foundation.NewValueKey("testresults:tree")
	ResultsTextKey  = foundation.NewValueKey("testresults:text")
	ResultsEmptyKey = foundation.NewValueKey("testresults:empty")
)

// ResultKind says what a node of the tree of Test Results is.
type ResultKind int

// The kinds of node.
const (
	// NodeResult is a run, NodeTest a test that took part in it and NodeMessage a message of the test.
	NodeResult ResultKind = iota
	NodeTest
	NodeMessage
)

// ResultNode is a node of the tree of Test Results.
type ResultNode struct {
	Kind   ResultKind
	Key    string
	Label  string
	State  coretesting.State
	Result *coretesting.Result
	Test   coretesting.TestResult
	// Message is the message of a NodeMessage and Index its place among the messages of the test (over all its tasks).
	Message coretesting.Message
	Index   int
	// Duration is the duration of a test, coretesting.NoDuration for none.
	Duration time.Duration
	Retired  bool
}

// ResultsOpts are what Test Results needs of its window.
type ResultsOpts struct {
	// Ctx ends what the view asked for when it is done.
	Ctx context.Context
	// Service is the test service of the workspace.
	Service *coretesting.Service
	// Post runs a function on the UI thread, later.
	Post func(func())
	// Links make the paths in the text of a node links; nil for none.
	Links *kit.AnsiLinks
	// Display writes the path of a file of a stack frame; nil writes it as it is.
	Display func(path string) string
	// OnSelect is called when a node is selected.
	OnSelect func(*ResultNode)
}

// Results is the model of Test Results: the nodes as the service has them, and the selected one.
type Results struct {
	opts  ResultsOpts
	model *kit.TreeModel
	ctl   *kit.TreeController

	nodes     map[string]*ResultNode
	known     map[string]bool
	listeners []*func()
	// selected is the key of the selected node, the newest run when none was chosen.
	selected string
	stop     func()
	dirty    atomic.Bool
}

// NewResults makes the model and follows the service.
func NewResults(opts ResultsOpts) *Results {
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	r := &Results{opts: opts, nodes: map[string]*ResultNode{}, known: map[string]bool{}, ctl: kit.NewTreeController()}
	r.model = kit.NewTreeModel(kit.TreeModelOpts{})
	r.stop = opts.Service.Subscribe(r.onEvent)
	r.Refresh()
	return r
}

// Dispose stops following the service.
func (r *Results) Dispose() { r.stop() }

// onEvent hands the events of the service to the UI thread, one burst at a time.
func (r *Results) onEvent(ev coretesting.Event) {
	if ev.Kind == coretesting.EventCoverage || ev.Kind == coretesting.EventControllers || ev.Kind == coretesting.EventProfiles {
		return
	}
	if r.dirty.Swap(true) {
		return
	}
	r.opts.Post(func() {
		r.dirty.Store(false)
		r.Refresh()
	})
}

// Model is the tree and Controller gives access to the tree widget.
func (r *Results) Model() *kit.TreeModel { return r.model }

// Controller returns the tree controller of the view.
func (r *Results) Controller() *kit.TreeController { return r.ctl }

// Node is the node with the key.
func (r *Results) Node(key string) (*ResultNode, bool) { n, ok := r.nodes[key]; return n, ok }

// Selected is the node whose text shows: the one chosen, else the newest run.
func (r *Results) Selected() (*ResultNode, bool) {
	if n, ok := r.nodes[r.selected]; ok {
		return n, true
	}
	if rs := r.opts.Service.Results(); len(rs) > 0 {
		n, ok := r.nodes[ResultKey(rs[0])]
		return n, ok
	}
	return nil, false
}

// OnChange calls fn after the nodes or the selection changed, and returns the function that stops it.
func (r *Results) OnChange(fn func()) func() {
	p := &fn
	r.listeners = append(r.listeners, p)
	return func() {
		if i := slices.Index(r.listeners, p); i >= 0 {
			r.listeners = slices.Delete(r.listeners, i, i+1)
		}
	}
}

func (r *Results) notify() {
	for _, f := range slices.Clone(r.listeners) {
		(*f)()
	}
}

// ResultKey is the key of the node of a run; TestKey and MessageNodeKey are those of a test of it and of a message of the test.
func ResultKey(res *coretesting.Result) string { return "r:" + res.ID() }

// TestKey is the key of the node of a test in a run.
func TestKey(res *coretesting.Result, testID string) string { return "t:" + res.ID() + ":" + testID }

// MessageNodeKey is the key of the node of the message with the index of a test in a run.
func MessageNodeKey(res *coretesting.Result, testID string, index int) string {
	return "m:" + res.ID() + ":" + testID + ":" + strconv.Itoa(index)
}

// Select selects the node with the key and shows its text; false when there is none.
func (r *Results) Select(key string) bool {
	if _, ok := r.nodes[key]; !ok {
		return false
	}
	r.selected = key
	r.model.SetSelected(key)
	r.model.SetFocus(key)
	r.ctl.Reveal(key)
	r.notify()
	if r.opts.OnSelect != nil {
		r.opts.OnSelect(r.nodes[key])
	}
	return true
}

// ShowLatest selects the newest run (VS Code's showLatestRun).
func (r *Results) ShowLatest() {
	if rs := r.opts.Service.Results(); len(rs) > 0 {
		r.Select(ResultKey(rs[0]))
	}
}

// messagesOf are the messages of a test over all its tasks, in the order of the tasks.
func messagesOf(t coretesting.TestResult) []coretesting.Message {
	var out []coretesting.Message
	for _, task := range t.Tasks {
		out = append(out, task.Messages...)
	}
	return out
}

// Refresh makes the nodes again from the service: the runs newest first, and in each the tests that have a state, in a tree of their
// parents, and the messages of the tests.
func (r *Results) Refresh() {
	r.nodes = map[string]*ResultNode{}
	var items []kit.TreeItem
	var fresh []string
	for _, res := range r.opts.Service.Results() {
		rn := &ResultNode{Kind: NodeResult, Key: ResultKey(res), Result: res, Label: res.Name(), Duration: coretesting.NoDuration}
		if rn.Label == "" {
			rn.Label = "Test run at " + res.StartedAt().Format("15:04:05")
		}
		c := res.Counts()
		for _, s := range statesInOrder {
			if c[s] > 0 && s != coretesting.StateUnset {
				rn.State = s
				break
			}
		}
		if !res.IsComplete() {
			rn.State = coretesting.StateRunning
		}
		r.nodes[rn.Key] = rn
		item := kit.TreeItem{Key: rn.Key, Data: rn, Label: rn.Label}
		byID := map[string]coretesting.TestResult{}
		for _, t := range res.Tests() {
			byID[t.Item.ID] = t
		}
		var build func(ids []string) []kit.TreeItem
		build = func(ids []string) []kit.TreeItem {
			var out []kit.TreeItem
			for _, id := range ids {
				t, ok := byID[id]
				if !ok || t.State == coretesting.StateUnset {
					continue
				}
				tn := &ResultNode{Kind: NodeTest, Key: TestKey(res, id), Result: res, Test: t, Label: t.Item.Label, State: t.State, Duration: t.Duration, Retired: t.Retired}
				r.nodes[tn.Key] = tn
				ti := kit.TreeItem{Key: tn.Key, Data: tn, Label: tn.Label, Children: build(t.Children)}
				for i, m := range messagesOf(t) {
					if m.Type != coretesting.MessageError {
						continue
					}
					mn := &ResultNode{Kind: NodeMessage, Key: MessageNodeKey(res, id, i), Result: res, Test: t, Message: m, Index: i, State: t.OwnState, Label: firstLine(m.Text), Duration: coretesting.NoDuration}
					r.nodes[mn.Key] = mn
					ti.Children = append(ti.Children, kit.TreeItem{Key: mn.Key, Data: mn, Label: mn.Label})
				}
				out = append(out, ti)
			}
			return out
		}
		var roots []string
		for _, t := range res.Tests() {
			parent := coretesting.ParentID(t.Item.ID)
			if _, ok := byID[parent]; !ok {
				roots = append(roots, t.Item.ID)
			}
		}
		item.Children = build(roots)
		items = append(items, item)
		fresh = r.opened(item, fresh)
	}
	_ = r.model.SetItems(items)
	for _, k := range fresh {
		r.model.Expand(k)
	}
	r.notify()
}

// opened adds the nodes of an item that have children and were not seen before to the keys to open, so that a run, its groups and the
// tests that failed (with their messages) show open; a node the user closed stays closed.
func (r *Results) opened(item kit.TreeItem, keys []string) []string {
	if len(item.Children) > 0 && !r.known[item.Key] {
		r.known[item.Key] = true
		keys = append(keys, item.Key)
	}
	for _, c := range item.Children {
		keys = r.opened(c, keys)
	}
	return keys
}

// firstLine is the first line of a text, for a label.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "(no message)"
	}
	return s
}

// TestOutput is the output that belongs to a test in a run: the pieces of the output of each task that its messages name, in order.
func TestOutput(res *coretesting.Result, t coretesting.TestResult) string {
	var out strings.Builder
	tasks := res.Tasks()
	for i, task := range t.Tasks {
		if i >= len(tasks) {
			break
		}
		data, start := res.Output(tasks[i].ID)
		for _, m := range task.Messages {
			if m.Type != coretesting.MessageOutput {
				continue
			}
			from := m.Offset - start
			to := from + m.Length
			if from < 0 || to > int64(len(data)) || from > to {
				continue
			}
			out.Write(data[from:to])
		}
	}
	return out.String()
}

// RunOutput is the output of all the tasks of a run.
func RunOutput(res *coretesting.Result) string {
	var out strings.Builder
	for _, task := range res.Tasks() {
		data, _ := res.Output(task.ID)
		out.Write(data)
	}
	return out.String()
}

// NodeText is the text beside the tree for a node, with the ANSI sequences it holds. display writes the path of a file of a stack frame (nil
// writes it as it is).
func NodeText(n *ResultNode, display func(path string) string) string {
	switch n.Kind {
	case NodeResult:
		return RunOutput(n.Result)
	case NodeTest:
		var b strings.Builder
		for _, m := range messagesOf(n.Test) {
			if m.Type == coretesting.MessageError {
				b.WriteString(MessageText(m, display))
				b.WriteString("\n")
			}
		}
		if out := TestOutput(n.Result, n.Test); out != "" {
			b.WriteString(out)
		}
		return b.String()
	}
	return MessageText(n.Message, display)
}

// MessageText is the details of a message: its text, the expected and the actual text of a diff, and the stack. display writes the path of
// a file of a frame (nil writes it as it is).
func MessageText(m coretesting.Message, display func(path string) string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(m.Text, "\n"))
	b.WriteString("\n")
	if m.HasDiff {
		fmt.Fprintf(&b, "\n\x1b[32mExpected\x1b[0m\n%s\n\x1b[31mActual\x1b[0m\n%s\n", strings.TrimRight(m.Expected, "\n"), strings.TrimRight(m.Actual, "\n"))
	}
	if len(m.StackTrace) > 0 {
		b.WriteString("\n\x1b[2mStack\x1b[0m\n")
		for _, f := range m.StackTrace {
			b.WriteString(f.Label)
			if f.URI != "" {
				place := strings.TrimPrefix(f.URI, "file://")
				if display != nil {
					place = display(place)
				}
				if f.Pos != nil {
					place += ":" + strconv.Itoa(f.Pos.Line+1) + ":" + strconv.Itoa(f.Pos.Col+1)
				}
				b.WriteString("  " + place)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ResultsBody is the body of Test Results: the tree of the runs and beside it the text of the selected node.
type ResultsBody struct {
	widgets.StatefulWidget
	r *Results
}

// NewResultsBody makes the body of Test Results.
func NewResultsBody(r *Results) *ResultsBody {
	return &ResultsBody{StatefulWidget: widgets.NewStatefulWidgetBase(), r: r}
}

// CreateState implements widgets.StatefulWidget.
func (b *ResultsBody) CreateState() widgets.State { return &resultsBodyState{} }

type resultsBodyState struct {
	widgets.StateOf[*ResultsBody]
	stop func()
}

func (s *resultsBodyState) InitState() { s.follow() }

func (s *resultsBodyState) follow() {
	s.stop = s.Widget().r.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

func (s *resultsBodyState) DidUpdateWidget(old *ResultsBody) {
	if old.r != s.Widget().r {
		s.stop()
		s.follow()
	}
}

func (s *resultsBodyState) Dispose() { s.stop() }

// resultsTreeWidth is the width of the tree beside the text.
const resultsTreeWidth = 300.0

// Build implements widgets.State.
func (s *resultsBodyState) Build(bc widgets.BuildContext) widgets.Widget {
	r := s.Widget().r
	colors := kit.ThemeOf(bc)
	if r.model.Len() == 0 {
		return widgets.NewKeyedSubtree(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 8, 20, 8),
			widgets.NewText("No test results yet.", widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.DescriptionForeground)})), widgets.WidgetOpts{Key: ResultsEmptyKey})
	}
	tree := kit.NewTree(r.model, kit.TreeOpts{
		Key: ResultsTreeKey, Controller: r.ctl, TypeToFind: foundation.Some(false),
		Render: func(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
			return s.render(bc, n, st)
		},
		OnSelect: func(n kit.TreeNode) {
			r.selected = n.Key
			r.notify()
			if r.opts.OnSelect != nil {
				if node, ok := r.nodes[n.Key]; ok {
					r.opts.OnSelect(node)
				}
			}
		},
	})
	var text widgets.Widget = widgets.NewSizedBox(nil)
	if n, ok := r.Selected(); ok {
		out := NodeText(n, r.opts.Display)
		if strings.TrimSpace(out) == "" {
			out = "\x1b[2mThe test printed no output.\x1b[0m"
		}
		text = widgets.NewKeyedSubtree(widgets.NewClipRect(widgets.NewSingleChildScrollView(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(6, 12),
			kit.NewAnsiText(out, kit.AnsiTextOpts{TrimEndNewline: true, Links: r.opts.Links})))), widgets.WidgetOpts{Key: ResultsTextKey})
	}
	divider := widgets.NewSizedBox(widgets.NewColoredBox(colors.PanelBorder, widgets.NewSizedBox(nil)), widgets.SizedBoxOpts{Width: foundation.Some(1.0)})
	return widgets.NewRow([]widgets.Widget{
		widgets.NewSizedBox(tree, widgets.SizedBoxOpts{Width: foundation.Some(resultsTreeWidth)}), divider, widgets.NewExpanded(text),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// render is a row of the tree: the icon of the state, the label and the duration.
func (s *resultsBodyState) render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
	node, ok := n.Data.(*ResultNode)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	colors := kit.ThemeOf(bc)
	base := kit.TextStyleOf(bc, 13, st.Foreground)
	dim := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(st.Foreground.WithOpacity(0.65))})
	icon, color := StateIcon(node.State), StateColor(colors, node.State, node.Retired)
	if node.Kind == NodeMessage {
		icon = fonts.CodiconError
	}
	spans := []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(node.Label), Style: base})}
	if node.Kind == NodeTest && node.Duration >= 0 {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + FormatDuration(node.Duration)), Style: dim}))
	}
	return widgets.NewKeyedSubtree(widgets.NewRow([]widgets.Widget{
		widgets.NewIcon(fonts.CodiconIcon(icon), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(color)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
		widgets.NewFlexible(widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}),
			widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), widgets.WidgetOpts{Key: foundation.NewValueKey("testresults:row:" + n.Key)})
}
