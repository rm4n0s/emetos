package exthost

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

// The tests of the sharing between window tabs (step 11.3): the links, the check at delivery, the question, the requests
// and the reads across tabs.

// watchExt is the extension of the sharing tests: it may hear and ask the topics under "watch." and the document events of
// other tabs. plainExt declares no cross-tab topic.
const (
	watchExt = "watch"
	plainExt = "plain"
)

// shareRig is a host with two or more tabs, in each of which the extension "watch" and the extension "plain" are active.
type shareRig struct {
	t    *testing.T
	r    *rig
	tabs map[string]*tab
	apis map[string]extension.API // "<extension>@<tab>"
}

// newShareRig makes the tabs named names (the folders are /work/<name>), over the store; nil is a store in memory. The tabs
// listed in noID have no ID and the ones in noAsk cannot ask.
func newShareRig(t *testing.T, store *storage.SharingStore, noID, noAsk []string, names ...string) *shareRig {
	t.Helper()
	sr := &shareRig{t: t, tabs: map[string]*tab{}, apis: map[string]extension.API{}}
	mk := func(id string, decl extension.EventDecl) extension.Factory {
		m := manifest(id)
		m.Events = decl
		f, _ := factory(m, func(f *fake) {
			f.activate = func(_ context.Context, api extension.API) error {
				sr.apis[id+"@"+api.Scope().Name] = api
				return nil
			}
		})
		return f
	}
	sr.r = newRigStore(t, store,
		mk(watchExt, extension.EventDecl{CrossTab: []string{"watch.**", "emetos.document.*", extension.TopicSharingRevoked}}),
		mk(plainExt, extension.EventDecl{}),
	)
	for _, n := range noID {
		sr.r.noID[n] = true
	}
	for _, n := range noAsk {
		sr.r.noAsk[n] = true
	}
	for _, n := range names {
		tb := sr.r.newTab(n)
		sr.tabs[n] = tb
		tb.scope.Start()
	}
	for n, tb := range sr.tabs {
		for _, id := range []string{watchExt, plainExt} {
			if sr.apis[id+"@"+n] == nil {
				t.Fatalf("%s was not activated in %s: %v", id, n, tb.scope.Failure(id))
			}
		}
	}
	sr.pump()
	for _, tb := range sr.tabs {
		tb.deferred = true
	}
	return sr
}

// pump runs what every tab has queued, as the UI thread does, until nothing is left.
func (sr *shareRig) pump() {
	for range 4 {
		for _, tb := range sr.tabs {
			tb.pump()
		}
	}
}

func (sr *shareRig) events(ext, tab string) extension.Events { return sr.apis[ext+"@"+tab].Events() }

// listen subscribes the extension "watch" of the tab to the pattern for the events of the other tabs too.
func (sr *shareRig) listen(tab, pattern string) *heard {
	sr.t.Helper()
	g := &heard{}
	if _, err := sr.events(watchExt, tab).Subscribe(pattern, g.fn, extension.FromOtherScopes()); err != nil {
		sr.t.Fatal(err)
	}
	return g
}

// publish publishes the topic from the extension "watch" of the tab and delivers it.
func (sr *shareRig) publish(tab, topic string) {
	sr.t.Helper()
	if err := sr.events(watchExt, tab).Publish(topic, map[string]string{"from": tab}); err != nil {
		sr.t.Fatal(err)
	}
	sr.pump()
}

func id(tab string) string { return "/work/" + tab }

// link saves a link between the tabs in the store.
func (sr *shareRig) link(from, to string, events ...string) {
	sr.t.Helper()
	if err := sr.r.sharing.AddLink(context.Background(), storage.SharingLink{From: id(from), To: id(to), Events: events}); err != nil {
		sr.t.Fatal(err)
	}
}

func TestLinkDeliversInItsDirectionOnly(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.link("notes", "office")
	atOffice := sr.listen("office", "watch.ping")
	atNotes := sr.listen("notes", "watch.ping")

	sr.publish("notes", "watch.ping")
	if got := atOffice.all(); len(got) != 1 || got[0].Scope.Name != "notes" || got[0].Scope.ID != id("notes") || got[0].Publisher != watchExt {
		t.Fatalf("office heard %+v, want the event of notes", got)
	}
	if got := atNotes.all(); len(got) != 1 {
		t.Fatalf("notes heard %d events, want its own one", len(got))
	}
	atOffice.mu.Lock()
	atOffice.events = nil
	atOffice.mu.Unlock()

	// nothing goes back: office -> notes has no link; notes listens, so it is asked about the link, but the event is not delivered
	sr.publish("office", "watch.ping")
	if got := atNotes.all(); len(got) != 1 {
		t.Errorf("notes heard %d events after office published, want only its own earlier one", len(got))
	}
	if len(atOffice.all()) != 1 {
		t.Errorf("office should hear its own event, got %d", len(atOffice.all()))
	}
	if q := sr.tabs["notes"].asked; len(q) != 1 || q[0].FromID != id("office") || q[0].ToID != id("notes") {
		t.Errorf("notes was asked %+v, want one question about events from office", q)
	}
	if len(sr.tabs["office"].asked) != 0 {
		t.Errorf("office was asked %+v although the link exists", sr.tabs["office"].asked)
	}
}

func TestOnlySubscriptionsThatListenToOtherTabsHearThem(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.link("notes", "office")
	local := sr.subscribe("office", "watch.ping")
	sr.publish("notes", "watch.ping")
	if got := local.all(); len(got) != 0 {
		t.Errorf("a subscription of the own scope heard %+v from another tab", got)
	}
	if len(sr.tabs["office"].asked) != 0 {
		t.Errorf("a local subscription must never cause a question: %+v", sr.tabs["office"].asked)
	}
}

func (sr *shareRig) subscribe(tab, pattern string) *heard {
	sr.t.Helper()
	g := &heard{}
	if _, err := sr.events(watchExt, tab).Subscribe(pattern, g.fn); err != nil {
		sr.t.Fatal(err)
	}
	return g
}

func TestTopicLimitedLink(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.link("notes", "office", "watch.a")
	g := sr.listen("office", "watch.*")
	sr.publish("notes", "watch.a")
	if got := g.topics(); !slices.Equal(got, []string{"watch.a"}) {
		t.Fatalf("heard %v, want [watch.a]", got)
	}
	sr.publish("notes", "watch.b")
	if got := g.topics(); !slices.Equal(got, []string{"watch.a"}) {
		t.Errorf("a topic outside the link crossed: %v", got)
	}
	// the topic outside the link causes a question, for the pattern the extension listens to
	q := sr.tabs["office"].asked
	if len(q) != 1 || !slices.Equal(q[0].Topics, []string{"watch.*"}) {
		t.Fatalf("questions = %+v, want one for watch.*", q)
	}
	sr.tabs["office"].answers[0](SharingDeny)
	sr.publish("notes", "watch.b")
	if got := g.topics(); !slices.Equal(got, []string{"watch.a"}) {
		t.Errorf("a denied topic crossed: %v", got)
	}
}

func TestRemovingALinkStopsDeliveryAtOnceAndTellsTheReceiver(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.link("notes", "office", "watch.*")
	g := sr.listen("office", "watch.*")
	revoked := sr.subscribe("office", extension.TopicSharingRevoked)
	sr.publish("notes", "watch.one")
	if len(g.all()) != 1 {
		t.Fatalf("heard %d events, want 1", len(g.all()))
	}
	// two events wait in the queue of notes when the link is removed: neither arrives
	for _, topic := range []string{"watch.two", "watch.three"} {
		if err := sr.events(watchExt, "notes").Publish(topic, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := sr.r.host.RemoveLink(context.Background(), id("notes"), id("office"), false); err != nil {
		t.Fatal(err)
	}
	sr.pump()
	if got := g.topics(); !slices.Equal(got, []string{"watch.one"}) {
		t.Errorf("heard %v after the link was removed, want only watch.one", got)
	}
	got := revoked.all()
	if len(got) != 1 {
		t.Fatalf("office heard %d revocations, want 1", len(got))
	}
	rev, err := extension.EventValue[extension.SharingRevoked](got[0])
	if err != nil || rev.From != id("notes") || rev.FromName != "notes" || rev.Reason != extension.RevokedLinkRemoved || !slices.Equal(rev.Topics, []string{"watch.*"}) {
		t.Errorf("revocation = %+v (%v)", rev, err)
	}
	if got[0].Publisher != extension.CorePublisher {
		t.Errorf("publisher = %q, want emetos", got[0].Publisher)
	}
	if len(sr.tabs["office"].asked) != 0 {
		t.Errorf("removing a link counts as a Deny; office was asked %+v", sr.tabs["office"].asked)
	}
	if len(sr.r.host.SharingSnapshot().Links) != 0 {
		t.Error("the link is still listed")
	}
	// removing what is not there is not an error and tells no one
	if err := sr.r.host.RemoveLink(context.Background(), id("notes"), id("office"), false); err != nil {
		t.Fatal(err)
	}
	sr.pump()
	if len(revoked.all()) != 1 {
		t.Error("a second removal published a revocation")
	}
}

func TestPrivateWorkspaceSendsNothingAndRefusesReads(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.link("notes", "office")
	sr.link("office", "notes")
	g := sr.listen("office", "watch.ping")
	revoked := sr.subscribe("office", extension.TopicSharingRevoked)
	sr.tabs["notes"].docs.texts["file:///work/notes/a.txt"] = "secret"
	read := func() (string, error) {
		return sr.apis["watch@office"].Documents().Read(context.Background(), "file:///work/notes/a.txt", extension.FromScope(id("notes")))
	}
	// the extension declares emetos.document.*, which covers the emetos.document.changed that a read needs
	if text, err := read(); err != nil || text != "secret" {
		t.Fatalf("Read through the link = %q, %v", text, err)
	}
	sr.publish("notes", "watch.ping")
	if len(g.all()) != 1 {
		t.Fatalf("heard %d events before the workspace was private, want 1", len(g.all()))
	}
	if err := sr.r.host.SetPrivate(context.Background(), id("notes"), true); err != nil {
		t.Fatal(err)
	}
	sr.publish("notes", "watch.ping")
	if len(g.all()) != 1 {
		t.Errorf("a private workspace sent an event: %d heard", len(g.all()))
	}
	if len(sr.tabs["office"].asked) != 0 {
		t.Errorf("a private workspace must not even be asked about: %+v", sr.tabs["office"].asked)
	}
	_, err := read()
	wantRoute(t, err, "readFrom.ExtensionReadPrivate")
	// it cannot be asked either
	o := newOutcome()
	if err := sr.events(watchExt, "notes").Handle("watch.status", func(context.Context, extension.Event) (any, error) { return "ok", nil }); err != nil {
		t.Fatal(err)
	}
	if err := sr.events(watchExt, "office").Request(context.Background(), "watch.status", nil, o.fn, extension.ToOtherScopes()); err != nil {
		t.Fatal(err)
	}
	sr.pump()
	_, _, rerr := o.get()
	wantRoute(t, rerr, "startCross.EventSharingSourcePrivate")
	// and it sends no request itself
	o2 := newOutcome()
	if err := sr.events(watchExt, "notes").Request(context.Background(), "watch.status", nil, o2.fn, extension.ToOtherScopes()); err != nil {
		t.Fatal(err)
	}
	sr.pump()
	_, _, rerr = o2.get()
	wantRoute(t, rerr, "startCross.EventSharingPrivate")
	if got := revoked.all(); len(got) != 1 {
		t.Fatalf("office heard %d revocations, want 1 (notes became private)", len(got))
	} else if rev, _ := extension.EventValue[extension.SharingRevoked](got[0]); rev.Reason != extension.RevokedPrivate || rev.From != id("notes") {
		t.Errorf("revocation = %+v", rev)
	}
	if snap := sr.r.host.SharingSnapshot(); !slices.ContainsFunc(snap.Scopes, func(s SharingScope) bool { return s.ID == id("notes") && s.Private }) {
		t.Errorf("the page does not list notes as private: %+v", snap.Scopes)
	}
	// private again off: events flow again
	if err := sr.r.host.SetPrivate(context.Background(), id("notes"), false); err != nil {
		t.Fatal(err)
	}
	sr.publish("notes", "watch.ping")
	if len(g.all()) != 2 {
		t.Errorf("heard %d events after the workspace was not private any more, want 2", len(g.all()))
	}
}

func TestTheQuestionAndItsThreeAnswers(t *testing.T) {
	tests := []struct {
		name       string
		answer     SharingAnswer
		wantFile   bool
		wantLinked bool // the next event crosses
		asksAgain  bool // an extension activated anew asks again
	}{
		{"Allow saves the link", SharingAllow, true, true, false},
		{"This Session Only keeps it in memory", SharingSession, false, true, false},
		{"Deny stores nothing", SharingDeny, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := storagetest.Isolate(t)
			store := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
			sr := newShareRig(t, store, nil, nil, "notes", "office")
			g := sr.listen("office", "watch.ping")
			office := sr.tabs["office"]

			// three events before the answer: one question, and none of them is delivered
			for range 3 {
				sr.publish("notes", "watch.ping")
			}
			if len(office.asked) != 1 {
				t.Fatalf("asked %d times, want once", len(office.asked))
			}
			q := office.asked[0]
			if q.From != "notes" || q.FromID != id("notes") || q.To != "office" || q.ToID != id("office") ||
				q.Extension != "Ext "+watchExt || !slices.Equal(q.Topics, []string{"watch.ping"}) {
				t.Errorf("question = %+v", q)
			}
			if len(g.all()) != 0 {
				t.Fatalf("an event crossed before the answer: %v", g.topics())
			}
			office.answers[0](tt.answer)
			office.answers[0](SharingAllow) // a second answer does nothing

			_, statErr := os.Stat(paths.EventSharingPath())
			if wrote := statErr == nil; wrote != tt.wantFile {
				t.Errorf("file written = %v, want %v", wrote, tt.wantFile)
			}
			if tt.wantFile {
				data, err := os.ReadFile(paths.EventSharingPath())
				if err != nil {
					t.Fatal(err)
				}
				var f struct {
					Version int
					Links   []storage.SharingLink
				}
				if err := json.Unmarshal(data, &f); err != nil || f.Version != 1 || len(f.Links) != 1 ||
					f.Links[0].From != id("notes") || f.Links[0].To != id("office") || !slices.Equal(f.Links[0].Events, []string{"watch.ping"}) {
					t.Errorf("file = %s (%v)", data, err)
				}
			}
			sr.publish("notes", "watch.ping")
			if crossed := len(g.all()) == 1; crossed != tt.wantLinked {
				t.Errorf("the event after the answer crossed = %v, want %v", crossed, tt.wantLinked)
			}
			if len(office.asked) != 1 {
				t.Errorf("asked %d times after the answer, want still once", len(office.asked))
			}
			if got := sr.r.host.SharingSnapshot().Links; (len(got) == 1) != tt.wantLinked || (len(got) == 1 && got[0].Session != (tt.answer == SharingSession)) {
				t.Errorf("the page lists %+v", got)
			}

			// an extension that is activated anew asks again after Deny, and not after the others
			office.disabled = []string{watchExt}
			office.scope.Refresh()
			office.disabled = nil
			office.scope.Refresh()
			office.scope.Fire(extension.OnStartup)
			sr.pump()
			g2 := sr.listen("office", "watch.ping")
			sr.publish("notes", "watch.ping")
			if asked := len(office.asked) == 2; asked != tt.asksAgain {
				t.Errorf("asked again = %v (%d questions), want %v", asked, len(office.asked), tt.asksAgain)
			}
			if !tt.asksAgain && len(g2.all()) != 1 {
				t.Errorf("the link no longer works after the extension was activated again")
			}
		})
	}
}

func TestADeniedQuestionIsNotAskedAgainWhileTheExtensionRuns(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.listen("office", "watch.ping")
	sr.publish("notes", "watch.ping")
	sr.tabs["office"].answers[0](SharingDeny)
	for range 5 {
		sr.publish("notes", "watch.ping")
	}
	if got := len(sr.tabs["office"].asked); got != 1 {
		t.Errorf("asked %d times, want once", got)
	}
	if snap := sr.r.host.SharingSnapshot(); len(snap.Links) != 0 {
		t.Errorf("a denial stored a link: %+v", snap.Links)
	}
}

func TestQuestionsAboutOnePairWaitForTheOneOnScreen(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	office := sr.tabs["office"]
	sr.listen("office", "watch.a")
	sr.listen("office", "watch.b")
	sr.publish("notes", "watch.a")
	sr.publish("notes", "watch.b")
	if len(office.asked) != 1 || !slices.Equal(office.asked[0].Topics, []string{"watch.a"}) {
		t.Fatalf("questions = %+v, want one about watch.a", office.asked)
	}
	office.answers[0](SharingAllow)
	// what was asked meanwhile is asked again, because the link does not carry it
	if len(office.asked) != 2 || !slices.Equal(office.asked[1].Topics, []string{"watch.b"}) {
		t.Fatalf("questions = %+v, want a second one about watch.b", office.asked)
	}
	office.answers[1](SharingSession)
	links := sr.r.host.SharingSnapshot().Links
	if len(links) != 2 {
		t.Fatalf("links = %+v, want the saved one and the session one", links)
	}
}

func TestATopicOutsideTheManifestIsNeverAskedAboutAndNeverCrosses(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.link("notes", "office")
	office := sr.tabs["office"]

	// a topic that the manifest does not list
	_, err := sr.events(watchExt, "office").Subscribe("other.topic", func(extension.Event) {}, extension.FromOtherScopes())
	wantRoute(t, err, "eventsAPI.Subscribe.EventCrossTabNotDeclared")
	_, err = sr.events(watchExt, "office").Subscribe("watch", func(extension.Event) {}, extension.FromScopes(id("notes")))
	wantRoute(t, err, "eventsAPI.Subscribe.EventCrossTabNotDeclared")
	_, err = sr.events(watchExt, "office").Subscribe("emetos.**", func(extension.Event) {}, extension.FromOtherScopes())
	wantRoute(t, err, "eventsAPI.Subscribe.EventCrossTabNotDeclared") // wider than the manifest lists
	// an extension with no cross-tab topic at all
	_, err = sr.events(plainExt, "office").Subscribe("watch.ping", func(extension.Event) {}, extension.FromOtherScopes())
	wantRoute(t, err, "eventsAPI.Subscribe.EventCrossTabNotDeclared")
	err = sr.events(plainExt, "office").Request(context.Background(), "watch.x", nil, nil, extension.ToOtherScopes())
	wantRoute(t, err, "eventsAPI.Request.EventRequestCrossTabNotDeclared")
	_, err = sr.apis[plainExt+"@office"].Documents().Read(context.Background(), "file:///x", extension.FromScope(id("notes")))
	wantRoute(t, err, "readFrom.ExtensionReadNotDeclared")

	// the same topic subscribed inside the scope only, and an event that the link carries: nothing crosses, nothing is asked
	local := sr.subscribe("office", "other.topic")
	if err := sr.events(watchExt, "notes").Publish("watch.ping", 1); err != nil {
		t.Fatal(err)
	}
	sr.pump()
	if len(local.all()) != 0 || len(office.asked) != 0 {
		t.Errorf("heard %v, asked %+v", local.topics(), office.asked)
	}
}

func TestTheLinksSurviveARestart(t *testing.T) {
	paths := storagetest.Isolate(t)
	first := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
	sr := newShareRig(t, first, nil, nil, "notes", "office")
	sr.listen("office", "watch.ping")
	sr.publish("notes", "watch.ping")
	sr.tabs["office"].answers[0](SharingAllow)
	if err := sr.r.host.SetPrivate(context.Background(), id("secret"), true); err != nil {
		t.Fatal(err)
	}
	for _, tb := range sr.tabs {
		tb.scope.Close()
	}

	// a new process reads the file and delivers without a question
	second := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
	if err := second.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	again := newShareRig(t, second, nil, nil, "notes", "office")
	g := again.listen("office", "watch.ping")
	again.publish("notes", "watch.ping")
	if len(g.all()) != 1 || len(again.tabs["office"].asked) != 0 {
		t.Errorf("heard %d events, asked %+v; want one event and no question", len(g.all()), again.tabs["office"].asked)
	}
	if !second.IsPrivate(id("secret")) {
		t.Error("the private workspace was forgotten")
	}
	snap := again.r.host.SharingSnapshot()
	if len(snap.Links) != 1 || snap.Links[0].FromName != "notes" || snap.Links[0].ToName != "office" {
		t.Errorf("snapshot links = %+v", snap.Links)
	}
	// a private scope that is not open is listed by its id
	if !slices.ContainsFunc(snap.Scopes, func(s SharingScope) bool { return s.ID == id("secret") && s.Private && !s.Open }) {
		t.Errorf("snapshot scopes = %+v", snap.Scopes)
	}
}

func TestTabsThatCannotBeNamedTakePartInNothing(t *testing.T) {
	sr := newShareRig(t, nil, []string{"empty"}, nil, "notes", "empty")
	sr.link("notes", "empty")
	atEmpty := sr.listen("empty", "watch.ping")
	atNotes := sr.listen("notes", "watch.ping")
	sr.publish("notes", "watch.ping")
	sr.publish("empty", "watch.ping")
	if len(atEmpty.all()) != 1 || len(atNotes.all()) != 1 {
		t.Errorf("empty heard %d, notes heard %d; each should hear only its own event", len(atEmpty.all()), len(atNotes.all()))
	}
	if len(sr.tabs["empty"].asked)+len(sr.tabs["notes"].asked) != 0 {
		t.Error("nobody can be asked about a tab with no ID")
	}
	o := newOutcome()
	if err := sr.events(watchExt, "empty").Request(context.Background(), "watch.x", nil, o.fn, extension.ToOtherScopes()); err != nil {
		t.Fatal(err)
	}
	sr.pump()
	_, _, err := o.get()
	wantRoute(t, err, "startCross.EventSharingUnnamed")
	_, err = sr.apis[watchExt+"@empty"].Documents().Read(context.Background(), "file:///x", extension.FromScope(id("notes")))
	wantRoute(t, err, "readFrom.ExtensionReadUnnamed")
}

func TestATabThatCannotAskNeverGetsALink(t *testing.T) {
	sr := newShareRig(t, nil, nil, []string{"office"}, "notes", "office")
	g := sr.listen("office", "watch.ping")
	sr.publish("notes", "watch.ping")
	if len(g.all()) != 0 {
		t.Error("an event crossed without a link")
	}
	if len(sr.r.host.SharingSnapshot().Links) != 0 {
		t.Error("a link appeared")
	}
	sr.handleStatus("notes")
	o := newOutcome()
	if err := sr.events(watchExt, "office").Request(context.Background(), "watch.status", nil, o.fn, extension.ToOtherScopes()); err != nil {
		t.Fatal(err)
	}
	sr.pump()
	_, _, err := o.get()
	wantRoute(t, err, "startCross.EventSharingRefused")
}

// handleStatus makes the extension "watch" of the tab answer watch.status with the name of the tab.
func (sr *shareRig) handleStatus(tab string) {
	sr.t.Helper()
	if err := sr.events(watchExt, tab).Handle("watch.status", func(_ context.Context, ev extension.Event) (any, error) {
		return map[string]string{"answeredBy": tab, "askedBy": ev.Scope.Name, "scopeID": ev.Scope.ID}, nil
	}); err != nil {
		sr.t.Fatal(err)
	}
}

func (sr *shareRig) request(tab, topic string, opts ...extension.RequestOption) *outcome {
	sr.t.Helper()
	o := newOutcome()
	if err := sr.events(watchExt, tab).Request(context.Background(), topic, nil, o.fn, opts...); err != nil {
		sr.t.Fatal(err)
	}
	return o
}

func TestRequestsAndRepliesTravelAlongTheLink(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.handleStatus("notes")
	sr.handleStatus("office") // the own handler is not used when the request goes to other tabs
	sr.link("notes", "office")
	o := sr.request("office", "watch.status", extension.ToOtherScopes())
	sr.pump()
	calls, ev, err := o.get()
	if calls != 1 || err != nil {
		t.Fatalf("calls = %d, err = %v", calls, err)
	}
	var got map[string]string
	if err := ev.Decode(&got); err != nil || got["answeredBy"] != "notes" || got["askedBy"] != "office" || got["scopeID"] != id("office") {
		t.Errorf("reply = %v (%v)", got, err)
	}
	if ev.Scope.Name != "notes" || ev.Publisher != watchExt {
		t.Errorf("the reply says %s from %s", ev.Publisher, ev.Scope.Name)
	}
	// ToScopes names the tab
	o = sr.request("office", "watch.status", extension.ToScopes(id("notes")))
	sr.pump()
	if _, _, err := o.get(); err != nil {
		t.Errorf("ToScopes: %v", err)
	}
	o = sr.request("office", "watch.status", extension.ToScopes(id("elsewhere")))
	sr.pump()
	_, _, err = o.get()
	wantRoute(t, err, "startCross.EventNoHandler")
	// the other direction has no link: office is asked to let notes' events... notes asks office, so the question is in notes
	o = sr.request("notes", "watch.status", extension.ToOtherScopes())
	sr.pump()
	if calls, _, _ := o.get(); calls != 0 || len(sr.tabs["notes"].asked) != 1 {
		t.Fatalf("calls = %d, questions in notes = %+v; want a request that waits for the answer", calls, sr.tabs["notes"].asked)
	}
	sr.tabs["notes"].answers[0](SharingDeny)
	sr.pump()
	_, _, err = o.get()
	wantRoute(t, err, "bus.startCross.func2.EventSharingDenied")
}

func TestARequestAsksOnceAndGoesOnWhenAllowed(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.handleStatus("notes")
	o1 := sr.request("office", "watch.status", extension.ToOtherScopes())
	o2 := sr.request("office", "watch.status", extension.ToOtherScopes())
	sr.pump()
	office := sr.tabs["office"]
	if len(office.asked) != 1 || !slices.Equal(office.asked[0].Topics, []string{"watch.status"}) || office.asked[0].FromID != id("notes") {
		t.Fatalf("questions = %+v, want one", office.asked)
	}
	office.answers[0](SharingSession)
	sr.pump()
	for i, o := range []*outcome{o1, o2} {
		if calls, _, err := o.get(); calls != 1 || err != nil {
			t.Errorf("request %d: calls = %d, err = %v", i+1, calls, err)
		}
	}
}

func TestAReplyIsStoppedWhenTheLinkIsRemovedBeforeItComes(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.link("notes", "office")
	var reply extension.EventReply
	if err := sr.events(watchExt, "notes").HandleAsync("watch.slow", func(_ context.Context, _ extension.Event, r extension.EventReply) { reply = r }); err != nil {
		t.Fatal(err)
	}
	o := sr.request("office", "watch.slow", extension.ToOtherScopes())
	sr.pump()
	if reply == nil {
		t.Fatal("the handler was not called")
	}
	if err := sr.r.host.RemoveLink(context.Background(), id("notes"), id("office"), false); err != nil {
		t.Fatal(err)
	}
	reply("late", nil)
	sr.pump()
	_, _, err := o.get()
	wantRoute(t, err, "EventSharingRevoked")
}

func TestRequestsTimeOutWhileTheQuestionIsOnScreen(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.handleStatus("notes")
	o := sr.request("office", "watch.status", extension.ToOtherScopes())
	sr.pump()
	sr.tabs["office"].clock.advance(DefaultRequestTimeout + time.Second)
	sr.pump()
	_, _, err := o.get()
	wantRoute(t, err, "EventTimeout")
	// the answer that comes after the timeout does nothing to the request
	sr.tabs["office"].answers[0](SharingAllow)
	sr.pump()
	if calls, _, _ := o.get(); calls != 1 {
		t.Errorf("the reply function was called %d times", calls)
	}
}

func TestReadingADocumentOfAnotherTab(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.tabs["notes"].docs.texts["file:///work/notes/a.txt"] = "from notes"
	read := func(scope string) (string, error) {
		return sr.apis[watchExt+"@office"].Documents().Read(context.Background(), "file:///work/notes/a.txt", extension.FromScope(scope))
	}
	_, err := read(id("notes"))
	wantRoute(t, err, "readFrom.ExtensionReadNotShared")
	sr.link("office", "notes") // the wrong direction
	_, err = read(id("notes"))
	wantRoute(t, err, "readFrom.ExtensionReadNotShared")
	_, err = read("/work/unknown")
	wantRoute(t, err, "readFrom.ExtensionScopeUnknown")
	sr.link("notes", "office", "watch.*") // a link that does not carry the document events
	_, err = read(id("notes"))
	wantRoute(t, err, "readFrom.ExtensionReadNotShared")
	sr.link("notes", "office", "emetos.document.changed")
	if text, err := read(id("notes")); err != nil || text != "from notes" {
		t.Errorf("Read = %q, %v", text, err)
	}
	// the own scope needs no link, and naming it is the same as not naming it
	sr.tabs["office"].docs.texts["file:///own"] = "own"
	for _, opts := range [][]extension.ReadOption{nil, {extension.FromScope(id("office"))}} {
		if text, err := sr.apis[watchExt+"@office"].Documents().Read(context.Background(), "file:///own", opts...); err != nil || text != "own" {
			t.Errorf("own Read = %q, %v", text, err)
		}
	}
	// the window of the source has no documents
	sr.tabs["notes"].scope.opts.Documents = nil
	_, err = read(id("notes"))
	wantRoute(t, err, "readFrom.ExtensionSourceDocumentsUnavailable")
}

func TestEventsOfAFastTopicCrossAsABatch(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.link("notes", "office")
	g := sr.listen("office", extension.TopicDocumentChanged)
	for i := range 3 {
		if err := sr.tabs["notes"].scope.PublishCore(extension.TopicDocumentChanged, extension.DocumentChange{URI: "file:///a", Version: i}); err != nil {
			t.Fatal(err)
		}
	}
	sr.tabs["notes"].frame()
	sr.pump()
	got := g.all()
	if len(got) != 1 || len(got[0].Batch) != 3 || got[0].Publisher != extension.CorePublisher || got[0].Scope.ID != id("notes") {
		t.Errorf("heard %+v, want one batch of three from emetos in notes", got)
	}
}

func TestSnapshotListsLinksSessionLinksAndScopes(t *testing.T) {
	sr := newShareRig(t, nil, nil, nil, "notes", "office")
	sr.link("notes", "office", "emetos.document.*")
	sr.r.sharing.AddSessionLink(storage.SharingLink{From: id("office"), To: id("notes")})
	snap := sr.r.host.SharingSnapshot()
	want := []SharingEntry{
		{From: id("notes"), To: id("office"), FromName: "notes", ToName: "office", Events: []string{"emetos.document.*"}},
		{From: id("office"), To: id("notes"), FromName: "office", ToName: "notes", Session: true},
	}
	if len(snap.Links) != 2 || !entriesEqual(snap.Links[0], want[0]) || !entriesEqual(snap.Links[1], want[1]) {
		t.Errorf("links = %+v, want %+v", snap.Links, want)
	}
	if len(snap.Scopes) != 2 || snap.Scopes[0].Name != "notes" || !snap.Scopes[0].Open || snap.Scopes[1].Name != "office" {
		t.Errorf("scopes = %+v", snap.Scopes)
	}
	if err := sr.r.host.RemoveLink(context.Background(), id("office"), id("notes"), true); err != nil {
		t.Fatal(err)
	}
	if got := sr.r.host.SharingSnapshot().Links; len(got) != 1 {
		t.Errorf("links after removing the session link = %+v", got)
	}
}

func entriesEqual(a, b SharingEntry) bool {
	return a.From == b.From && a.To == b.To && a.FromName == b.FromName && a.ToName == b.ToName && a.Session == b.Session && slices.Equal(a.Events, b.Events)
}

func TestAWriteThatFailsKeepsTheLinkForTheSession(t *testing.T) {
	paths := storagetest.Isolate(t)
	failing := storage.NewSharingStore(paths, storage.SharingStoreOptions{WriteFile: func(string, []byte) error { return os.ErrPermission }})
	sr := newShareRig(t, failing, nil, nil, "notes", "office")
	g := sr.listen("office", "watch.ping")
	sr.publish("notes", "watch.ping")
	sr.tabs["office"].answers[0](SharingAllow)
	sr.publish("notes", "watch.ping")
	if len(g.all()) != 1 {
		t.Errorf("heard %d events; the link must work for the session although it could not be saved", len(g.all()))
	}
	if msgs := sr.tabs["office"].notifications(); len(msgs) != 1 || !strings.Contains(msgs[0], "Cannot save") {
		t.Errorf("notifications = %v", msgs)
	}
	// removing it when the file cannot be written still stops it
	if err := sr.r.host.SetPrivate(context.Background(), id("x"), true); err == nil {
		t.Error("expected the write error")
	} else if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "SharingWriteFailed" {
		t.Errorf("error = %v", err)
	}
}
