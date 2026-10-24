package quickinput

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// recorder is a provider that records what the controller does to it.
type recorder struct {
	info     ProviderInfo
	picks    []Item
	filters  []string
	accepted []string
	done     []bool
	provided int
}

func (r *recorder) Info() ProviderInfo { return r.info }
func (r *recorder) Provide(s *Session) Handler {
	r.provided++
	s.Picker().MatchOnLabel, s.Picker().SortByLabel = false, false
	return &recorderHandler{r: r, s: s}
}

type recorderHandler struct {
	r *recorder
	s *Session
}

func (h *recorderHandler) ValueChanged(f string) {
	h.r.filters = append(h.r.filters, f)
	h.s.Picker().SetItems(h.r.picks)
}
func (h *recorderHandler) Accept(row Row, mods KeyMods) {
	h.r.accepted = append(h.r.accepted, row.Item.Label)
	h.s.c.AcceptRow(row, mods)
}
func (h *recorderHandler) Done(accepted bool) { h.r.done = append(h.r.done, accepted) }

func newRecorderController() (*Controller, *recorder, *recorder) {
	reg := NewRegistry()
	a := &recorder{info: ProviderInfo{Prefix: ">", Placeholder: "commands", ContextKey: "inA"}, picks: items("one", "two")}
	b := &recorder{info: ProviderInfo{Prefix: ":", Placeholder: "lines"}, picks: items("line")}
	reg.MustRegister(a, b)
	return NewController(ControllerOptions{Registry: reg, FallbackPlaceholder: "nothing here"}), a, b
}

func TestControllerShowAndSwitchProviders(t *testing.T) {
	c, a, b := newRecorderController()
	c.Show(">li", ShowOptions{})
	if !c.Visible() || c.Picker().Value() != ">li" || c.Picker().Placeholder() != "commands" || a.provided != 1 {
		t.Fatalf("visible %v value %q placeholder %q provided %d", c.Visible(), c.Picker().Value(), c.Picker().Placeholder(), a.provided)
	}
	if !reflect.DeepEqual(a.filters, []string{"li"}) {
		t.Errorf("filters %v", a.filters)
	}
	// typing goes to the same provider
	c.SetValue(">lin")
	if !reflect.DeepEqual(a.filters, []string{"li", "lin"}) || a.provided != 1 {
		t.Errorf("filters %v provided %d", a.filters, a.provided)
	}
	// typing another prefix starts the other provider with the same text
	c.SetValue(":12")
	if c.Provider() != Provider(b) || c.Picker().Placeholder() != "lines" || c.Picker().Value() != ":12" || b.provided != 1 {
		t.Errorf("provider %v placeholder %q value %q", c.Provider(), c.Picker().Placeholder(), c.Picker().Value())
	}
	if !reflect.DeepEqual(a.done, []bool{false}) || !reflect.DeepEqual(b.filters, []string{"12"}) {
		t.Errorf("a.done %v b.filters %v", a.done, b.filters)
	}
	// a value no provider takes shows the placeholder of the controller and no items
	c.SetValue("x")
	if c.Provider() != nil || c.Picker().Placeholder() != "nothing here" || len(c.Picker().Rows()) != 0 {
		t.Errorf("provider %v placeholder %q rows %d", c.Provider(), c.Picker().Placeholder(), len(c.Picker().Rows()))
	}
	// and typing a prefix again brings a provider back
	c.SetValue(">a")
	if c.Provider() != Provider(a) || a.provided != 2 {
		t.Errorf("provider %v provided %d", c.Provider(), a.provided)
	}
	c.Hide()
	if c.Visible() || !reflect.DeepEqual(a.done, []bool{false, false}) {
		t.Errorf("visible %v done %v", c.Visible(), a.done)
	}
}

func TestControllerShowWhileVisible(t *testing.T) {
	tests := []struct {
		name      string
		first     string
		second    string
		opts      ShowOptions
		wantValue string
		// wantStart is where the selection starts; it ends at the end of the value
		wantStart int
	}{
		{"the same provider with a longer value replaces the text", ">", ">li", ShowOptions{}, ">li", 1},
		{"the same provider with just the prefix keeps the text", ">li", ">", ShowOptions{}, ">li", 1},
		{"preserving keeps the text and puts the caret at the end", ">li", ">other", ShowOptions{PreserveValue: true}, ">li", 3},
		{"another provider keeps the text after the old prefix", ">li", ":", ShowOptions{}, ":li", 1},
		{"another provider preserving takes the value as it is", ">li", ":", ShowOptions{PreserveValue: true}, ":", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := newRecorderController()
			c.Show(tc.first, ShowOptions{})
			c.Show(tc.second, tc.opts)
			if got := c.Picker().Value(); got != tc.wantValue {
				t.Errorf("value %q, want %q", got, tc.wantValue)
			}
			start, end, ok := c.Picker().ValueSelection()
			if !ok || start != tc.wantStart || end != len(tc.wantValue) {
				t.Errorf("selection %d-%d (%v), want %d-%d", start, end, ok, tc.wantStart, len(tc.wantValue))
			}
		})
	}
}

func TestControllerPreserveInputRestoresTheLastValue(t *testing.T) {
	reg := NewRegistry()
	p := &ListProvider{ProviderInfo: ProviderInfo{Prefix: ">"}, Picks: func(string) []Item {
		return []Item{{Label: "x", Accept: func(KeyMods) {}}}
	}, DefaultFilter: func() DefaultFilterKind { return DefaultFilterLast }}
	reg.MustRegister(p)
	c := NewController(ControllerOptions{Registry: reg})
	c.Show(">", ShowOptions{})
	c.SetValue(">word")
	c.Accept(KeyMods{})
	if c.Visible() {
		t.Fatal("still visible")
	}
	c.Show(">", ShowOptions{})
	if got := c.Picker().Value(); got != ">word" {
		t.Errorf("value %q, want the last one", got)
	}
	// with a value of its own the controller does not rewrite it
	c.Hide()
	c.Show(">other", ShowOptions{})
	if got := c.Picker().Value(); got != ">other" {
		t.Errorf("value %q", got)
	}
}

func TestControllerAcceptHidesThenRuns(t *testing.T) {
	reg := NewRegistry()
	var order []string
	p := &ListProvider{ProviderInfo: ProviderInfo{Prefix: ">"}, Picks: func(string) []Item {
		return []Item{{Label: "x", Accept: func(m KeyMods) {
			order = append(order, "accepted")
			if m != (KeyMods{Ctrl: true}) {
				t.Errorf("mods %+v", m)
			}
		}}, {Label: "nothing"}}
	}}
	reg.MustRegister(p)
	var afterRan int
	c := NewController(ControllerOptions{Registry: reg, After: func(f func()) {
		afterRan++
		if c := f; c == nil {
			t.Fatal("nil")
		}
		order = append(order, "after")
		f()
	}})
	c.OnChange(func() {
		if !c.Visible() {
			order = append(order, "hidden")
		}
	})
	c.Show(">", ShowOptions{})
	c.Accept(KeyMods{Ctrl: true})
	want := []string{"hidden", "after", "accepted"}
	if !reflect.DeepEqual(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
	// an item without Accept keeps the picker open
	c.Show(">", ShowOptions{})
	c.Picker().SetActive(1)
	c.Accept(KeyMods{})
	if !c.Visible() {
		t.Errorf("hidden by an item that does nothing")
	}
	// nothing active, nothing happens
	c.Hide()
	c.Accept(KeyMods{})
}

func TestControllerPick(t *testing.T) {
	var chosen []string
	var cancelled int
	opts := func() PickOptions {
		return PickOptions{
			Placeholder: "Select a snippet",
			Items: []Item{
				{Separator: true, Label: "User Snippets"},
				{Label: "alpha", Detail: "first", Accept: func(KeyMods) { chosen = append(chosen, "alpha") }},
				{Label: "beta", Detail: "second", Accept: func(KeyMods) { chosen = append(chosen, "beta") }},
			},
			MatchOnDetail: true,
			OnCancel:      func() { cancelled++ },
		}
	}
	t.Run("filters by label and detail", func(t *testing.T) {
		c := NewController(ControllerOptions{})
		c.Pick(opts())
		if !c.Visible() || c.Picker().Placeholder() != "Select a snippet" || len(c.Picker().Rows()) != 2 {
			t.Fatalf("visible %v rows %d", c.Visible(), len(c.Picker().Rows()))
		}
		if got := c.Picker().Rows()[0].Separator; got != "User Snippets" {
			t.Errorf("separator %q", got)
		}
		c.SetValue("sec")
		if got := labelsOf(c.Picker().Rows()); !reflect.DeepEqual(got, []string{"beta"}) {
			t.Errorf("rows %v", got)
		}
		// typing a prefix does not change the provider of a pick
		c.SetValue(">x")
		if !c.Visible() || c.Provider() != nil {
			t.Errorf("the pick changed to a provider")
		}
	})
	t.Run("accepting runs the item", func(t *testing.T) {
		chosen, cancelled = nil, 0
		c := NewController(ControllerOptions{})
		c.Pick(opts())
		c.Picker().Move(1)
		c.Accept(KeyMods{})
		if c.Visible() || !reflect.DeepEqual(chosen, []string{"beta"}) || cancelled != 0 {
			t.Errorf("visible %v chosen %v cancelled %d", c.Visible(), chosen, cancelled)
		}
	})
	t.Run("hiding cancels", func(t *testing.T) {
		chosen, cancelled = nil, 0
		c := NewController(ControllerOptions{})
		c.Pick(opts())
		c.Hide()
		if len(chosen) != 0 || cancelled != 1 {
			t.Errorf("chosen %v cancelled %d", chosen, cancelled)
		}
	})
}

func TestControllerPickActiveItem(t *testing.T) {
	items := []Item{
		{Separator: true, Label: "group"},
		{ID: "a", Label: "a", Accept: func(KeyMods) {}},
		{ID: "b", Label: "b", Accept: func(KeyMods) {}},
		{ID: "c", Label: "c", Accept: func(KeyMods) {}},
	}
	tests := []struct {
		name   string
		active string
		want   string
	}{
		{"no id: the first item", "", "a"},
		{"an id that is there", "c", "c"},
		{"the middle one", "b", "b"},
		{"an id that is not there: the first item", "z", "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewController(ControllerOptions{})
			c.Pick(PickOptions{Items: items, ActiveID: tt.active})
			row, ok := c.Picker().Active()
			if !ok || row.Item.Label != tt.want {
				t.Errorf("active %q, %v; want %q", row.Item.Label, ok, tt.want)
			}
		})
	}
}

func TestControllerButtons(t *testing.T) {
	reg := NewRegistry()
	var triggered []int
	next := TriggerNone
	p := &ListProvider{ProviderInfo: ProviderInfo{Prefix: ">"}, Picks: func(string) []Item {
		return []Item{
			{ID: "a", Label: "a", Buttons: []Button{{Icon: 1}}, Trigger: func(i int, _ KeyMods) TriggerAction { triggered = append(triggered, i); return next }},
			{ID: "b", Label: "b"},
		}
	}}
	reg.MustRegister(p)
	c := NewController(ControllerOptions{Registry: reg})
	c.Show(">", ShowOptions{})
	row := c.Picker().Rows()[0]
	next = TriggerRemove
	c.Trigger(row, 0, KeyMods{})
	if got := labelsOf(c.Picker().Rows()); !reflect.DeepEqual(got, []string{"b"}) || !reflect.DeepEqual(triggered, []int{0}) {
		t.Errorf("rows %v triggered %v", got, triggered)
	}
	c.Trigger(row, 5, KeyMods{}) // no such button
	c.Trigger(Row{}, 0, KeyMods{})
	if len(triggered) != 1 {
		t.Errorf("triggered %v", triggered)
	}
	next = TriggerClose
	c.Hide()
	c.Show(">", ShowOptions{})
	c.Trigger(c.Picker().Rows()[0], 0, KeyMods{})
	if c.Visible() {
		t.Errorf("TriggerClose did not hide")
	}
}

func TestListProviderNoResultsAndAsync(t *testing.T) {
	t.Run("no results pick", func(t *testing.T) {
		reg := NewRegistry()
		reg.MustRegister(&ListProvider{ProviderInfo: ProviderInfo{Prefix: ">"}, NoResults: "No matching commands", Picks: func(f string) []Item {
			if f == "zzz" {
				return nil
			}
			return items("a")
		}})
		c := NewController(ControllerOptions{Registry: reg})
		c.Show(">", ShowOptions{})
		c.SetValue(">zzz")
		if got := labelsOf(c.Picker().Rows()); !reflect.DeepEqual(got, []string{"No matching commands"}) {
			t.Errorf("rows %v", got)
		}
		// it does nothing when chosen
		c.Accept(KeyMods{})
		if !c.Visible() {
			t.Errorf("hidden")
		}
		// an empty filter with no picks shows nothing
		reg2 := NewRegistry()
		reg2.MustRegister(&ListProvider{ProviderInfo: ProviderInfo{Prefix: ">"}, NoResults: "none", Picks: func(string) []Item { return nil }})
		c2 := NewController(ControllerOptions{Registry: reg2})
		c2.Show(">", ShowOptions{})
		if len(c2.Picker().Rows()) != 0 {
			t.Errorf("rows %v", labelsOf(c2.Picker().Rows()))
		}
	})
	t.Run("async picks are cancelled when the filter changes", func(t *testing.T) {
		reg := NewRegistry()
		release := map[string]chan struct{}{"a": make(chan struct{}), "ab": make(chan struct{})}
		started := make(chan string, 4)
		var cancelled []string
		cancelledCh := make(chan string, 4)
		reg.MustRegister(&ListProvider{ProviderInfo: ProviderInfo{Prefix: ">"}, PicksAsync: func(ctx context.Context, f string) []Item {
			started <- f
			ch, ok := release[f]
			if !ok {
				return nil
			}
			select {
			case <-ch:
			case <-ctx.Done():
				cancelledCh <- f
				return nil
			}
			return items("result " + f)
		}})
		var posted []func()
		postCh := make(chan func(), 8)
		c := NewController(ControllerOptions{Registry: reg, Post: func(f func()) { postCh <- f }})
		c.Show(">a", ShowOptions{})
		<-started
		c.SetValue(">ab")
		if got := <-cancelledCh; got != "a" {
			t.Fatalf("cancelled %q", got)
		}
		cancelled = append(cancelled, "a")
		<-started
		close(release["ab"])
		// the first search was cancelled and still hands its (empty) result over, which is dropped; only the
		// second result is applied
		posted = append(posted, <-postCh, <-postCh)
		for _, f := range posted {
			f()
		}
		if got := labelsOf(c.Picker().Rows()); !reflect.DeepEqual(got, []string{"result ab"}) {
			t.Errorf("rows %v", got)
		}
		// the session ends: a late result is dropped
		c.Hide()
		select {
		case f := <-postCh:
			f()
		default:
		}
		_ = cancelled
	})
}

func TestControllerInvokerContext(t *testing.T) {
	keys := contextkey.MapContext{"a": true}
	calls := 0
	c := NewController(ControllerOptions{Registry: NewRegistry(), Invoker: func() contextkey.Context { calls++; return keys }})
	if c.InvokerContext().Value("a") != nil {
		t.Errorf("keys before showing")
	}
	c.Show("x", ShowOptions{})
	c.Show("y", ShowOptions{}) // still visible: asked once
	if calls != 1 || c.InvokerContext().Value("a") != true {
		t.Errorf("calls %d", calls)
	}
}

func TestControllerPrompt(t *testing.T) {
	validate := func(v string) string {
		if v == "" {
			return "A name is required."
		}
		if v == "bad" {
			return "That name is not allowed."
		}
		return ""
	}
	type result struct {
		accepted []string
		cancels  int
	}
	start := func(r *result, c *Controller) {
		c.Prompt(PromptOptions{
			Placeholder: "Path", Value: "/a/b.txt", Validate: validate,
			OnAccept: func(v string) { r.accepted = append(r.accepted, v) },
			OnCancel: func() { r.cancels++ },
		})
	}
	tests := []struct {
		name         string
		do           func(c *Controller)
		wantAccepted []string
		wantCancels  int
		wantVisible  bool
		wantMessage  string
	}{
		{"shows the value with the caret at its end", func(c *Controller) {}, nil, 0, true, ""},
		{"enter accepts a valid text", func(c *Controller) { c.Accept(KeyMods{}) }, []string{"/a/b.txt"}, 0, false, ""},
		{"an edited text is accepted", func(c *Controller) { c.SetValue("/c"); c.Accept(KeyMods{}) }, []string{"/c"}, 0, false, ""},
		{"an invalid text shows its message while typing", func(c *Controller) { c.SetValue("bad") }, nil, 0, true, "That name is not allowed."},
		{"an invalid text is not accepted", func(c *Controller) { c.SetValue("bad"); c.Accept(KeyMods{}) }, nil, 0, true, "That name is not allowed."},
		{"the message goes when the text is valid again", func(c *Controller) { c.SetValue("bad"); c.SetValue("good") }, nil, 0, true, ""},
		{"an empty text is invalid", func(c *Controller) { c.SetValue(""); c.Accept(KeyMods{}) }, nil, 0, true, "A name is required."},
		{"escape cancels", func(c *Controller) { c.Hide() }, nil, 1, false, ""},
		{"a text accepted does not cancel", func(c *Controller) { c.Accept(KeyMods{}); c.Hide() }, []string{"/a/b.txt"}, 0, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var r result
			c := NewController(ControllerOptions{})
			start(&r, c)
			if !c.Prompting() || c.Picker().Placeholder() != "Path" || c.Picker().Value() != "/a/b.txt" {
				t.Fatalf("prompting %v placeholder %q value %q", c.Prompting(), c.Picker().Placeholder(), c.Picker().Value())
			}
			if s, e, ok := c.Picker().ValueSelection(); !ok || s != 8 || e != 8 {
				t.Errorf("selection %d-%d %v, want the caret at the end", s, e, ok)
			}
			tt.do(c)
			if !reflect.DeepEqual(r.accepted, tt.wantAccepted) || r.cancels != tt.wantCancels || c.Visible() != tt.wantVisible ||
				c.Picker().Message() != tt.wantMessage {
				t.Errorf("accepted %v cancels %d visible %v message %q", r.accepted, r.cancels, c.Visible(), c.Picker().Message())
			}
		})
	}
	t.Run("a pick replaces the prompt", func(t *testing.T) {
		var r result
		c := NewController(ControllerOptions{})
		start(&r, c)
		c.Pick(PickOptions{Items: items("one")})
		if c.Prompting() {
			t.Error("still prompting")
		}
		c.SetValue("bad")
		if c.Picker().Message() != "" {
			t.Error("the validation of the prompt still runs")
		}
	})
}

func TestControllerPickHooks(t *testing.T) {
	items := func() []Item {
		return []Item{
			{Separator: true, Label: "group"},
			{ID: "a", Label: "alpha", Accept: func(KeyMods) {}},
			{ID: "b", Label: "beta", Accept: func(KeyMods) {}},
			{ID: "c", Label: "gamma", Accept: func(KeyMods) {}},
		}
	}
	// events records what the hooks saw, in order
	var events []string
	opts := func() PickOptions {
		return PickOptions{
			Items: items(), ActiveID: "b",
			OnActive: func(it Item, ok bool) {
				if !ok {
					events = append(events, "active:none")
					return
				}
				events = append(events, "active:"+it.ID)
			},
			OnHide: func(accepted bool) { events = append(events, fmt.Sprintf("hide:%v", accepted)) },
			OnCancel: func() {
				events = append(events, "cancel")
			},
		}
	}
	tests := []struct {
		name string
		do   func(c *Controller)
		want []string
	}{
		{"the initial active item is not reported", func(c *Controller) {}, nil},
		{"moving reports each active item", func(c *Controller) { c.Picker().Move(1); c.Picker().Move(1) }, []string{"active:c", "active:a"}},
		{"moving to the same item reports nothing", func(c *Controller) { c.Picker().SetActive(1) }, nil},
		{"typing a filter reports the new active item, then none for no match", func(c *Controller) {
			c.SetValue("gam")
			c.SetValue("zzz")
		}, []string{"active:c", "active:none"}},
		{"escape hides without a choice", func(c *Controller) { c.Hide() }, []string{"hide:false", "cancel"}},
		{"accepting hides with a choice and no cancel", func(c *Controller) { c.Accept(KeyMods{}) }, []string{"hide:true"}},
		{"another pick takes the place", func(c *Controller) { c.Pick(PickOptions{Items: items()}) }, []string{"hide:false"}},
		{"a prompt takes the place", func(c *Controller) { c.Prompt(PromptOptions{}) }, []string{"hide:false"}},
		{"a quick access takes the place", func(c *Controller) { c.Show(">", ShowOptions{}) }, []string{"hide:false"}},
		{"moving after the hide reports nothing", func(c *Controller) {
			p := c.Picker()
			c.Hide()
			events = nil
			p.Move(1)
		}, nil},
		{"hiding twice calls the hook once", func(c *Controller) { c.Hide(); c.Hide() }, []string{"hide:false", "cancel"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events = nil
			c := NewController(ControllerOptions{})
			c.Pick(opts())
			tt.do(c)
			if !reflect.DeepEqual(events, tt.want) {
				t.Errorf("events %v, want %v", events, tt.want)
			}
		})
	}
}
