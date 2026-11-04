package httpclient

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

var rich = gluttertest.FindOpts{FindRichText: true}

// bodyServer answers each path with what the test gave it, on 127.0.0.1.
func bodyServer(t *testing.T, routes map[string]func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := routes[r.URL.Path]; ok {
			h(w)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// sendTo sends a GET of the path from the tab and waits for the response pane.
func (r *rig) sendTo(tab *reqTab, srv *httptest.Server, path string) {
	r.t.Helper()
	tab.d.method = "GET"
	tab.d.url.SetValue(srv.URL + path)
	tab.d.bodyType = format.BodyNone
	tab.sendRequest()
	r.settle()
	if tab.send.resp == nil || tab.pane == nil {
		r.t.Fatalf("no response: status %q", tab.send.status)
	}
}

func TestTheResponsePaneShowsASentResponse(t *testing.T) {
	srv := bodyServer(t, map[string]func(http.ResponseWriter){
		"/user": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/", HttpOnly: true})
			_, _ = w.Write([]byte(`{"name":"Ada","roles":["admin"]}`))
		},
	})
	r, tab := reqRig(t, pCreate)
	if tab.pane != nil {
		t.Fatal("a pane before any send")
	}
	r.sendTo(tab, srv, "/user")
	d := tab.pane.disp
	if d.kind != bodyKindJSON || tab.pane.mode != modePretty || strings.Join(d.pretty, "\n") != "{\n  \"name\": \"Ada\",\n  \"roles\": [\n    \"admin\"\n  ]\n}" {
		t.Fatalf("kind %q mode %q pretty %q", d.kind, tab.pane.mode, d.pretty)
	}
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(time.Millisecond)
	for _, want := range []string{"200 OK", "Pretty", "Raw", "Headers (", "Cookies (1)", "Timeline"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, rich)) {
			t.Errorf("the pane does not show %q", want)
		}
	}
	if ts.Exists(gluttertest.ByTextContaining("Press Send", rich)) {
		t.Error("the hint is still shown with a response")
	}
	// the tabs
	for _, tc := range []struct{ tap, want string }{
		{"Headers (", "Content-Type"},
		{"Cookies (1)", "HttpOnly"},
		{"Timeline", "Timings"},
		{"Body", `"name"`},
	} {
		ts.Tap(gluttertest.Last(gluttertest.ByTextContaining(tc.tap, rich)))
		ts.Pump(time.Millisecond)
		if !ts.Exists(gluttertest.ByTextContaining(tc.want, rich)) {
			t.Errorf("after %q the pane does not show %q", tc.tap, tc.want)
		}
	}
	// Raw shows the body as received
	ts.Tap(gluttertest.First(gluttertest.ByText("Raw", rich)))
	ts.Pump(time.Millisecond)
	if tab.pane.mode != modeRaw || !ts.Exists(gluttertest.ByTextContaining(`{"name":"Ada","roles":["admin"]}`, rich)) {
		t.Errorf("mode %q", tab.pane.mode)
	}
	// a new send takes the old response away while it runs
	tab.sendRequest()
	if tab.pane != nil || tab.send.resp != nil {
		t.Error("the old response is still there while the next request runs")
	}
	r.settle()
	if tab.pane == nil {
		t.Error("no pane after the second send")
	}
}

func TestTheResponsePaneForEachKindOfBody(t *testing.T) {
	srv := bodyServer(t, map[string]func(http.ResponseWriter){
		"/empty": func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) },
		"/bin": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0, 1, 2, 0xff, 'A', 'B'})
		},
		"/xml": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/xml")
			_, _ = w.Write([]byte(`<a><b>1</b></a>`))
		},
		"/err": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("try later"))
		},
	})
	r, tab := reqRig(t, pCreate)
	for _, tc := range []struct {
		path, kind, mode string
		code             int
		textual          bool
	}{
		{"/empty", bodyKindEmpty, modeRaw, 204, false},
		{"/bin", bodyKindBinary, modeRaw, 200, false},
		{"/xml", bodyKindXML, modePretty, 200, true},
		{"/err", bodyKindText, modePretty, 503, true},
	} {
		r.sendTo(tab, srv, tc.path)
		if p := tab.pane; p.disp.kind != tc.kind || p.mode != tc.mode || tab.send.resp.StatusCode != tc.code || p.disp.textual() != tc.textual {
			t.Errorf("%s: kind %q mode %q code %d textual %v", tc.path, p.disp.kind, p.mode, tab.send.resp.StatusCode, p.disp.textual())
		}
		ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
		ts.Pump(time.Millisecond)
	}
	// a body that is not text is shown as a hexadecimal dump
	r.sendTo(tab, srv, "/bin")
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByTextContaining("00 01 02 ff 41 42", rich)) {
		t.Error("no hexadecimal dump of a binary body")
	}
}

// bigServer answers /big with lines of text, size bytes of them, and /json with a JSON array of numbers.
func bigServer(t *testing.T, size int) *httptest.Server {
	return bodyServer(t, map[string]func(http.ResponseWriter){
		"/big": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/plain")
			var b strings.Builder
			for i := 0; b.Len() < size; i++ {
				b.WriteString("line " + strconv.Itoa(i) + " of the big body\n")
			}
			_, _ = w.Write([]byte(b.String()))
		},
		"/json": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			var b strings.Builder
			b.WriteString("[")
			for i := range 100_000 {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(strconv.Itoa(i))
			}
			b.WriteString("]")
			_, _ = w.Write([]byte(b.String()))
		},
	})
}

func TestABodyBeyondTheMemoryLimitIsReadInPages(t *testing.T) {
	srv := bigServer(t, 9<<20)
	r, tab := reqRig(t, pCreate)
	r.api.SetSetting(SettingMaxResponseMiB, 1)
	r.sendTo(tab, srv, "/big")
	body := tab.send.resp.Body
	if body.InMemory() || body.Size() < 9<<20 {
		t.Fatalf("the body is in memory %v (%d bytes): the cache file was not used", body.InMemory(), body.Size())
	}
	p := tab.pane
	if p.mode != modeRaw || p.disp.hasPretty() || p.disp.pages < 144 || !strings.Contains(p.disp.why, "too large to format") {
		t.Fatalf("mode %q pretty %v pages %d why %q", p.mode, p.disp.hasPretty(), p.disp.pages, p.disp.why)
	}
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByTextContaining("Page 1 of 14", rich)) {
		t.Error("the pager is not shown")
	}
	if !ts.Exists(gluttertest.ByText("line 0 of the big body", rich)) {
		t.Error("the first page does not start the body")
	}
	// the next page is another part of the body, read when it is asked for
	ts.Tap(iconButton("Next page"))
	ts.Pump(time.Millisecond)
	if p.rawPage != 1 || ts.Exists(gluttertest.ByText("line 0 of the big body", rich)) {
		t.Errorf("page %d after Next", p.rawPage)
	}
	ts.Tap(iconButton("Last page"))
	ts.Pump(time.Millisecond)
	if p.rawPage != p.disp.pages-1 {
		t.Errorf("page %d after Last, of %d", p.rawPage, p.disp.pages)
	}
	// Copy Body refuses what is too large, and says so
	tab.copyBody()
	if len(r.api.ClipboardTexts()) != 0 {
		t.Error("a body over the copy limit was copied")
	}
	// the cache file goes with the response
	cache := body.Path()
	tab.dropResponse()
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Errorf("the cache file of the response is still there: %v", err)
	}
}

func TestPagesOfALargeBodyStayInTheFrameBudget(t *testing.T) {
	// the budgets are the UI thread's processor time (the test goroutine is locked to its thread), not wall time, so that a loaded
	// machine is not a slow frame; the race detector makes the code about ten times slower, as in the search tests
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	factor := time.Duration(1)
	if raceEnabled {
		factor = 10
	}
	srv := bigServer(t, 3<<20)
	r, tab := reqRig(t, pCreate)
	r.api.SetSetting(SettingMaxResponseMiB, 1)
	r.sendTo(tab, srv, "/big")
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(time.Millisecond)
	start := threadCPU(t)
	const frames = 20
	for range frames {
		ts.Pump(16 * time.Millisecond)
	}
	if per := (threadCPU(t) - start) / frames; per > 16*time.Millisecond*factor {
		t.Errorf("a frame of a paged body takes %v of the UI thread", per)
	}
	// moving through the pages costs a read of one page, not of the body
	start = threadCPU(t)
	for range 10 {
		ts.Tap(iconButton("Next page"))
		ts.Pump(16 * time.Millisecond)
	}
	if per := (threadCPU(t) - start) / 10; per > 50*time.Millisecond*factor {
		t.Errorf("a page turn takes %v of the UI thread", per)
	}
	tab.dropResponse()
}

func TestAHundredThousandLinesOfPrettyTextStayInTheFrameBudget(t *testing.T) {
	srv := bigServer(t, 1<<10)
	r, tab := reqRig(t, pCreate)
	r.sendTo(tab, srv, "/json")
	if n := len(tab.pane.disp.pretty); n < 100_000 {
		t.Fatalf("%d lines of pretty text", n)
	}
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(time.Millisecond)
	start := time.Now()
	const frames = 20
	for range frames {
		ts.Pump(16 * time.Millisecond)
	}
	if per := time.Since(start) / frames; per > 16*time.Millisecond {
		t.Errorf("a frame with 100000 lines takes %v", per)
	}
	if !ts.Exists(gluttertest.ByText("[", rich)) && !ts.Exists(gluttertest.ByText("0,", rich)) {
		t.Error("the view does not show the start of the body")
	}
}

func TestSaveResponseWritesTheBodyAsReceived(t *testing.T) {
	srv := bodyServer(t, map[string]func(http.ResponseWriter){
		"/user": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"a":1}`))
		},
	})
	r, tab := reqRig(t, pCreate)
	r.sendTo(tab, srv, "/user")
	dir := t.TempDir()
	to := filepath.Join(dir, "saved.json")
	r.api.AnswerFileChooser(to, nil)
	tab.saveResponse()
	r.settle()
	if data, err := os.ReadFile(to); err != nil || string(data) != `{"a":1}` {
		t.Errorf("file = %q, %v (the body as received, not the pretty text)", data, err)
	}
	calls := r.api.FileChooserCalls()
	if len(calls) != 1 || !calls[0].Options.Save || calls[0].Options.Name != "user.json" {
		t.Errorf("the dialog was asked %+v", calls)
	}
	if notes := r.notes(); len(notes) == 0 || !strings.Contains(notes[len(notes)-1], "Saved the response") {
		t.Errorf("notes = %q", notes)
	}
	// a dialog closed without a choice writes nothing
	r.api.AnswerFileChooser("", nil)
	n := len(r.notes())
	tab.saveResponse()
	r.settle()
	if len(r.notes()) != n {
		t.Errorf("notes after a canceled dialog = %q", r.notes())
	}
	// a file that cannot be written says why
	r.api.AnswerFileChooser(filepath.Join(dir, "missing", "x.json"), nil)
	tab.saveResponse()
	r.settle()
	if notes := r.notes(); !strings.Contains(notes[len(notes)-1], "Cannot save the response") {
		t.Errorf("notes = %q", notes)
	}
}

func TestCopyBodyAndOpenInANewTabUseWhatIsShown(t *testing.T) {
	srv := bodyServer(t, map[string]func(http.ResponseWriter){
		"/user": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"a":1}`))
		},
		"/bin": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0, 1, 2})
		},
	})
	r, tab := reqRig(t, pCreate)
	r.sendTo(tab, srv, "/user")
	tab.copyBody()
	if got := r.api.ClipboardTexts(); len(got) != 1 || got[0] != "{\n  \"a\": 1\n}\n" {
		t.Errorf("clipboard = %q (Pretty is on screen)", got)
	}
	tab.openBodyInTab()
	docs := r.api.UntitledDocuments()
	if len(docs) != 1 || docs[0].Text != "{\n  \"a\": 1\n}\n" || !strings.Contains(strings.ToLower(docs[0].LanguageID), "json") {
		t.Errorf("untitled documents = %+v", docs)
	}
	tab.setMode(modeRaw)
	tab.copyBody()
	if got := r.api.ClipboardTexts(); len(got) != 2 || got[1] != `{"a":1}` {
		t.Errorf("clipboard = %q (Raw is on screen)", got)
	}
	// a body that is not text is neither copied nor opened
	r.sendTo(tab, srv, "/bin")
	tab.copyBody()
	tab.openBodyInTab()
	if len(r.api.ClipboardTexts()) != 2 || len(r.api.UntitledDocuments()) != 1 {
		t.Error("a binary body was copied or opened as text")
	}
	// the buttons are there and say what they do
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(time.Millisecond)
	for _, tip := range []string{"Save Response to File...", "Copy Body", "Open in New Editor Tab"} {
		if !ts.Exists(iconButton(tip)) {
			t.Errorf("no button %q", tip)
		}
	}
}

func TestTheLayoutSettingPutsTheResponseBelowOrBesideTheRequest(t *testing.T) {
	r, tab := reqRig(t, pCreate)
	if got := r.ext.responseLayout(); got != "vertical" {
		t.Errorf("the default layout is %q", got)
	}
	r.api.SetSetting(SettingResponseLayout, "horizontal")
	if got := r.ext.responseLayout(); got != "horizontal" {
		t.Errorf("layout = %q", got)
	}
	r.api.SetSetting(SettingResponseLayout, "sideways")
	if got := r.ext.responseLayout(); got != "vertical" {
		t.Errorf("a value that is not a layout gave %q", got)
	}
	// the response of each is shown in its place: the status line is below the Send button, or to its right
	pos := func(layout string) (status, send gluttertestOffset) {
		r.api.SetSetting(SettingResponseLayout, layout)
		r.respond(tab, jsonResponse())
		ts := r.showReq(uitest.Themes()[0], tab, 1240, 600)
		ts.Pump(time.Millisecond)
		s, b := ts.TopLeft(gluttertest.First(gluttertest.ByText("200 OK", rich))), ts.TopLeft(gluttertest.First(gluttertest.ByText("Send")))
		return gluttertestOffset{s.Dx, s.Dy}, gluttertestOffset{b.Dx, b.Dy}
	}
	st, sd := pos("vertical")
	if st.y <= sd.y || st.x > sd.x {
		t.Errorf("vertical: status at %v, Send at %v", st, sd)
	}
	st, sd = pos("horizontal")
	if st.x <= sd.x || st.y > sd.y+400 {
		t.Errorf("horizontal: status at %v, Send at %v", st, sd)
	}
}

// gluttertestOffset is a position in a test.
type gluttertestOffset struct{ x, y float64 }

func TestAnImageResponseGetsAPreviewAndAnImageThatIsTooBigDoesNot(t *testing.T) {
	r, tab := reqRig(t, pCreate)
	r.respond(tab, imageResponse(t))
	if d := tab.pane.disp; d.pic == nil || d.picW != 120 || d.picH != 72 || tab.pane.mode != modePreview {
		t.Fatalf("picture %v %dx%d mode %q (%s)", d.pic != nil, d.picW, d.picH, tab.pane.mode, d.picNote)
	}
	// a body that says it is an image and is not shows why there is no preview
	bad := apiResponse(200, "200 OK", "image/png", []byte("not a png"))
	r.respond(tab, bad)
	if d := tab.pane.disp; d.pic != nil || !strings.Contains(d.picNote, "cannot be shown") {
		t.Errorf("picture %v note %q", d.pic != nil, d.picNote)
	}
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByTextContaining("cannot be shown", rich)) {
		t.Error("the note is not shown")
	}
}

func TestTheTimelineMasksWhatWasSentAndTheHoverHasTheBreakdown(t *testing.T) {
	r, tab := reqRig(t, pCreate)
	r.respond(tab, redirectResponse())
	ts := r.showReq(uitest.Themes()[0], tab, respW, 1000)
	ts.Pump(time.Millisecond)
	ts.Tap(gluttertest.Last(gluttertest.ByTextContaining("Timeline", rich)))
	ts.Pump(time.Millisecond)
	if ts.Exists(gluttertest.ByTextContaining("secret-token", rich)) || !ts.Exists(gluttertest.ByTextContaining("Bearer ********", rich)) {
		t.Error("the Authorization header is not masked in the Timeline")
	}
	for _, want := range []string{"302 Found", "301 Moved Permanently", "TLS 1.3", "Waiting (first byte)"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, rich)) {
			t.Errorf("the Timeline lacks %q", want)
		}
	}
	if !ts.Exists(gluttertest.ByType[*ui.Hover]()) {
		t.Error("the time of the status line has no hover")
	}
}

func TestANoteListsWhatWentWrongBesideTheStatus(t *testing.T) {
	r, tab := reqRig(t, pCreate)
	resp := jsonResponse()
	resp.Unresolved = []string{"userId"}
	resp.InsecureTLS = true
	resp.Warnings = []string{"the cookies could not be saved"}
	r.respond(tab, resp)
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(time.Millisecond)
	for _, want := range []string{"no value for userId", "certificate of the server was not checked", "cookies could not be saved"} {
		if !ts.Exists(gluttertest.ByTextContaining(want, rich)) {
			t.Errorf("no note %q", want)
		}
	}
}

// iconButton finds the icon button with the tooltip.
func iconButton(tip string) gluttertest.Finder {
	return gluttertest.ByPredicate("icon button "+tip, func(w widgets.Widget) bool {
		b, ok := w.(*ui.IconButton)
		return ok && b.Tooltip() == tip
	})
}

// TestALargeBodyInMemoryIsHeldAboutOnce sends a body of 8 MiB that is within the limit (so it is held in memory), shows the response
// pane and checks that the heap holds about one copy of it: the engine's buffer, the pane's pages and the tab's state keep no other.
func TestALargeBodyInMemoryIsHeldAboutOnce(t *testing.T) {
	const size = 8 << 20
	srv := bodyServer(t, map[string]func(http.ResponseWriter){
		"/mem": func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/plain")
			block := []byte(strings.Repeat("a line of the body that is held in memory\n", 1600))
			for written := 0; written < size; written += len(block) {
				_, _ = w.Write(block[:min(len(block), size-written)])
			}
		},
	})
	r, tab := reqRig(t, pCreate)
	r.api.SetSetting(SettingMaxResponseMiB, 16)
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(16 * time.Millisecond)
	heap := func() float64 {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return float64(m.HeapAlloc)
	}
	before := heap()
	r.sendTo(tab, srv, "/mem")
	ts.Pump(16 * time.Millisecond)
	ts.Pump(16 * time.Millisecond)
	body := tab.send.resp.Body
	if !body.InMemory() || body.Size() != size {
		t.Fatalf("in memory %v, size %d", body.InMemory(), body.Size())
	}
	held := (heap() - before) / size
	t.Logf("the heap holds %.2f bodies after the response is shown", held)
	if held > 1.5 {
		t.Errorf("the heap holds %.2f bodies after the response is shown", held)
	}
	runtime.KeepAlive(body)
	tab.dropResponse()
}
