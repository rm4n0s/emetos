//go:build emetos_display

package main

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/gui/app"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The whole of the HTTP client with real input (step 27.9, SPEC §10.33, docs/MANUAL-CHECKS.md 27.9): in a project with an environment
// "Dev", the dev echo server (an HTTP server on 127.0.0.1 that answers with a description of the request) is started, a collection is made
// in the side bar, a request "Login" is made in it, set to POST with a JSON body, sent, and an extraction rule takes the token that the
// echo sends back into the variable `token`; a second request "Profile" sends it in an Authorization header (`Bearer {{token}}`) and
// checks the status; the collection is run in the runner, which shows both requests with their checks passed. The screenshot of that
// state is the proof of the step (docs/implemented-proof-images/27.9-desktop-pass.png). No request leaves 127.0.0.1.
//
//	scripts/realinput.sh --shots docs/implemented-proof-images -- -tags emetos_display ./cmd/emetos -run TestRealInputTheWholeOfTheHTTPClient -v -count=2

// httpTabInputs are the input boxes of the editor tab (to the right of the side bar), from the top and then from the left.
func httpTabInputs(a *display.App) []app.DevInput {
	var out []app.DevInput
	for _, in := range a.Inputs() {
		if in.X > 330 && in.Y > 60 {
			out = append(out, in)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		return out[i].X < out[j].X
	})
	return out
}

// httpClickIn clicks the text, which is in the editor tab or the runner (x > 330) when tab is true and in the side bar (x < 330) when not.
func httpClickIn(t *testing.T, a *display.App, text string, tab bool) {
	t.Helper()
	a.EventuallyFor(20*time.Second, "the text "+text, func() (bool, string) {
		_, ok := textWhere(a, text, func(tx app.DevText) bool { return (tx.X > 330) == tab && tx.Y > 40 })
		return ok, a.Dump()
	})
	clickTextWhere(t, a, text, func(tx app.DevText) bool { return (tx.X > 330) == tab && tx.Y > 40 })
	a.Settle()
}

// httpType clicks the input box and types the text.
func httpType(t *testing.T, a *display.App, in app.DevInput, text string) {
	t.Helper()
	a.ClickInput(in)
	a.Type(text)
	a.Settle()
}

// httpMakeRequest makes a request in the collection through its context menu in the side bar and waits for its tab.
func httpMakeRequest(t *testing.T, a *display.App, name string) {
	t.Helper()
	a.RightClickText("Orders")
	httpClickIn(t, a, "New Request...", false)
	a.Eventually("the box for the name", func() (bool, string) { _, ok := a.Focused(); return ok, a.Dump() })
	a.Type(name)
	a.Press("enter")
	a.Eventually("the tab of "+name, func() (bool, string) {
		return activeTab(a) == name || len(httpTabInputs(a)) > 0 && textsContaining(a, "Send"), a.Dump()
	})
	a.Settle()
}

func TestRealInputTheWholeOfTheHTTPClient(t *testing.T) {
	a := display.Start(t, display.Options{
		Args: []string{"."},
		Project: map[string]string{
			".emetos/http/environments/dev.jsonc": "{\n  \"emetosHttp\": 1,\n  \"name\": \"Dev\",\n  \"variables\": [{\"name\": \"baseUrl\", \"value\": \"\"}]\n}\n",
		},
		Config: map[string]string{"settings.json": `{
	"workbench.colorTheme": "Dark+",
	"editor.minimap.enabled": false,
	"git.enabled": false,
	"window.menuBarVisibility": "classic"
}`},
	})
	a.Settle()
	dir := proofDir(t)
	snap := func(name string) { a.Settle(); a.Screenshot(filepath.Join(dir, name+".png")) }
	dismissToasts(a)

	// the side bar and the dev echo server, which points {{baseUrl}} of the active environment at itself
	a.Dev("emetos.httpclient.focus", nil)
	a.WaitText("New Collection...")
	a.Dev("emetos.httpclient.selectEnvironment", "dev")
	a.Dev("dev.httpclient.echo", nil)
	a.Eventually("the echo server's address", func() (bool, string) { return textsContaining(a, "Echo server on http://127.0.0.1:"), a.Dump() })
	dismissToasts(a)

	// a collection
	a.ClickText("New Collection...")
	a.Eventually("the box for the name", func() (bool, string) { return len(a.Inputs()) > 0, a.Dump() })
	a.Type("Orders")
	a.Press("enter")
	a.WaitText("Orders")
	waitFile(a, ".emetos/http/collections/orders/collection.jsonc", true)

	// the first request: POST {{baseUrl}}/login with a JSON body, and a rule that keeps the token the echo sends back
	httpMakeRequest(t, a, "Login")
	httpClickIn(t, a, "GET", true)
	httpClickIn(t, a, "POST", true)
	in := httpTabInputs(a)
	if len(in) == 0 {
		t.Fatalf("no input box in the request tab: %s", a.Dump())
	}
	httpType(t, a, in[0], "{{baseUrl}}/login")
	httpClickIn(t, a, "Body", true)
	httpClickIn(t, a, "None", true)
	httpClickIn(t, a, "JSON", true)
	a.Eventually("the body box", func() (bool, string) { return len(httpTabInputs(a)) >= 2, a.Dump() })
	httpType(t, a, httpTabInputs(a)[1], `{"token": "tok-4821"}`)
	httpClickIn(t, a, "Extract", true)
	httpClickIn(t, a, "Add Rule", true)
	var row []app.DevInput
	a.Eventually("the three boxes of the rule", func() (bool, string) {
		all := httpTabInputs(a)
		row = nil
		if len(all) > 0 {
			last := all[len(all)-1].Y
			for _, x := range all {
				if x.Y == last {
					row = append(row, x)
				}
			}
		}
		return len(row) == 3, a.Dump()
	})
	httpType(t, a, row[0], "$.json.token")
	httpType(t, a, row[2], "token")
	httpClickIn(t, a, "Send", true)
	a.EventuallyFor(30*time.Second, "the response of Login", func() (bool, string) {
		return textsContaining(a, "200") && textsContaining(a, "tok-4821"), a.Dump()
	})
	snap("httpclient-desktop-login")
	a.Press("ctrl+s")
	waitFile(a, ".emetos/http/collections/orders/login.request.jsonc", true)
	if got := readProjectFile(t, a, ".emetos/http/collections/orders/login.request.jsonc"); !strings.Contains(got, "$.json.token") || !strings.Contains(got, "POST") {
		t.Errorf("the request file of Login is wrong:\n%s", got)
	}

	// the second request sends the token in a header and checks the status
	httpMakeRequest(t, a, "Profile")
	in = httpTabInputs(a)
	httpType(t, a, in[0], "{{baseUrl}}/profile")
	httpClickIn(t, a, "Headers", true)
	httpClickIn(t, a, "Add", true)
	a.Eventually("the boxes of the header", func() (bool, string) { return len(httpTabInputs(a)) >= 4, a.Dump() })
	hdr := httpTabInputs(a)
	hdr = hdr[len(hdr)-3:] // name, value and description
	httpType(t, a, hdr[0], "Authorization")
	httpType(t, a, hdr[1], "Bearer {{token}}")
	httpClickIn(t, a, "Checks", true)
	httpClickIn(t, a, "Add Check", true)
	httpClickIn(t, a, "Send", true)
	a.EventuallyFor(30*time.Second, "the response of Profile with the token", func() (bool, string) {
		return textsContaining(a, "Bearer tok-4821"), a.Dump()
	})
	snap("httpclient-desktop-profile")
	a.Press("ctrl+s")
	waitFile(a, ".emetos/http/collections/orders/profile.request.jsonc", true)
	if got := readProjectFile(t, a, ".emetos/http/collections/orders/profile.request.jsonc"); !strings.Contains(got, "{{token}}") || strings.Contains(got, "tok-4821") {
		t.Errorf("the request file of Profile is wrong (it keeps {{token}}, never the value):\n%s", got)
	}

	// the runner: both requests, in order, with the values carried from one to the next
	a.RightClickText("Orders")
	httpClickIn(t, a, "Run Collection...", false)
	httpClickIn(t, a, "Run", true)
	a.EventuallyFor(60*time.Second, "the results of the run", func() (bool, string) {
		return textsContaining(a, "Completed") && textsContaining(a, "2 requests") && textsContaining(a, "0 failed"), a.Dump()
	})
	for _, want := range []string{"Login", "Profile", "200 OK"} {
		if !textsContaining(a, want) {
			t.Errorf("the runner does not show %q: %s", want, a.Dump())
		}
	}
	snap("27.9-desktop-pass")
}
