package httpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

func TestARequestFileOpenedFromTheExplorerBecomesTheRequestTab(t *testing.T) {
	r := newRig(t)
	p := r.col(pCreate)
	uri := fileURI(p)
	// the Explorer opened the file as a text editor: the tab is closed and the request tab shown
	r.api.SetTabs(extension.DocumentTab{ID: "1:a", Group: 1, Kind: "text", URI: uri})
	r.api.Emit(extension.TopicDocumentOpened, extension.DocumentInfo{URI: uri, LanguageID: "jsonc", Version: 1})
	r.api.Pump()
	r.api.WaitFor("the request tab", func() bool { r.api.Pump(); return len(r.ext.reqTabs) == 1 })
	if got := r.api.ClosedTabs(); len(got) != 1 || got[0] != "1:a" {
		t.Errorf("closed tabs = %v", got)
	}
	if r.ext.reqTabs[0].path != p {
		t.Errorf("tab for %q", r.ext.reqTabs[0].path)
	}
	// a file that is not a request of the collections, and one with edits, are left alone
	other := fileURI(r.folder + "/README.md")
	r.api.SetTabs(extension.DocumentTab{ID: "1:b", Group: 1, Kind: "text", URI: other})
	r.api.Emit(extension.TopicDocumentOpened, extension.DocumentInfo{URI: other})
	edited := fileURI(r.col(pList))
	r.api.SetTabs(extension.DocumentTab{ID: "1:c", Group: 1, Kind: "text", URI: edited, Dirty: true})
	r.api.Emit(extension.TopicDocumentOpened, extension.DocumentInfo{URI: edited})
	r.api.Pump()
	r.settle()
	if len(r.ext.reqTabs) != 1 || len(r.api.ClosedTabs()) != 1 {
		t.Errorf("tabs %d closed %v", len(r.ext.reqTabs), r.api.ClosedTabs())
	}
}

func TestOpenAsTextShowsTheFileAndRemembersIt(t *testing.T) {
	r := newRig(t)
	tab := r.openReq(pCreate)
	p := r.col(pCreate)
	// the command of the title action finds the tab of the group
	r.run(CmdOpenAsText, map[string]any{"groupId": float64(0)})
	shown := r.api.ShownDocuments()
	if len(shown) != 1 || shown[0].URI != fileURI(p) {
		t.Fatalf("shown = %+v", shown)
	}
	if got := r.ext.asText(); len(got) != 1 || got[0] != p {
		t.Fatalf("as text = %v", got)
	}
	// the document that opens is left as text
	uri := fileURI(p)
	r.api.SetTabs(extension.DocumentTab{ID: "1:a", Group: 1, Kind: "text", URI: uri})
	r.api.Emit(extension.TopicDocumentOpened, extension.DocumentInfo{URI: uri})
	r.api.Pump()
	r.settle()
	if len(r.api.ClosedTabs()) != 0 {
		t.Error("the text editor was closed")
	}
	// when the document closes it is a request again
	r.api.Emit(extension.TopicDocumentClosed, extension.DocumentInfo{URI: uri})
	r.api.Pump()
	if got := r.ext.asText(); len(got) != 0 {
		t.Errorf("as text after the close = %v", got)
	}
	_ = tab
	// a request that is not saved has no file
	r.record(sentAt(time.Hour, "GET", "https://h/x", 200))
	r.run(CmdOpenHistory, map[string]any{"seq": 0})
	r.ext.lastReq = r.ext.reqTabs[1]
	r.run(CmdOpenAsText, nil)
	if got := r.notes(); len(got) == 0 || !strings.Contains(got[len(got)-1], "saved in a file") {
		t.Errorf("notes = %v", got)
	}
	// by a path relative to the collections
	r.run(CmdOpenAsText, map[string]any{"path": pList})
	if got := r.api.ShownDocuments(); len(got) != 2 || got[1].URI != fileURI(r.col(pList)) {
		t.Errorf("shown = %+v", got)
	}
}

func TestTheCommandsOfTheRequestTab(t *testing.T) {
	r := newRig(t)
	r.run(CmdSend, nil)
	if got := r.notes(); len(got) == 0 || !strings.Contains(got[len(got)-1], "Open a request first") {
		t.Errorf("notes = %v", got)
	}
	tab := r.openReq(pCreate)
	tab.d.url.SetValue("http://h/saved-by-command")
	r.run(CmdSave, nil)
	if !strings.Contains(r.read(r.col(pCreate)), "saved-by-command") {
		t.Error("the save command did not write the file")
	}
	// the keys and the title action are only for the request tab
	m := r.ext.Manifest()
	var keys []string
	for _, k := range m.Contributes.Keybindings {
		if !strings.Contains(k.When, "activeEditorKind == '"+RequestKindID+"'") {
			t.Errorf("the key %s is not limited to the request tab", k.Key)
		}
		keys = append(keys, k.Command+" "+k.Key)
	}
	if strings.Join(keys, ",") != CmdSend+" ctrl+enter,"+CmdSave+" ctrl+s" {
		t.Errorf("keys = %v", keys)
	}
	if a := m.Contributes.EditorActions; len(a) != 1 || a[0].Command != CmdOpenAsText || !strings.Contains(a[0].When, RequestKindID) {
		t.Errorf("title actions = %+v", a)
	}
	// a tab given by its id
	r.run(CmdCancel, map[string]any{"tab": tab.tab.ID()})
}

func TestTheURLBoxKnowsItsVariables(t *testing.T) {
	r := newRig(t, func(folder string) {
		writeFile(t, folder+"/.emetos/http/environments/dev.jsonc", `{"emetosHttp": 1, "name": "Dev", "variables": [
  {"name": "baseUrl", "value": "http://localhost:8080"}, {"name": "apiKey", "secret": true}]}`)
	})
	r.run(CmdSelectEnvironment, "dev")
	tab := r.openReq(pCreate)
	tab.d.url.SetValue("{{baseUrl}}/a/{{apiKey}}/{{nothing}}/{{$guid}}/{{ baseUrl }}")
	look := tab.look()
	for _, tc := range []struct {
		name  string
		state varState
		value string
	}{
		{"baseUrl", varResolved, "http://localhost:8080"}, {"apiKey", varSecret, ""}, {"nothing", varUnresolved, ""}, {"$guid", varResolved, ""}, {"$bogus", varUnresolved, ""},
	} {
		state, value, _ := look(tc.name)
		if state != tc.state || value != tc.value {
			t.Errorf("%s = %v %q, want %v %q", tc.name, state, value, tc.state, tc.value)
		}
	}
	hover := tab.urlHover()
	for _, want := range []string{"baseUrl = http://localhost:8080  (environment)", "apiKey = ********  (secret, environment Dev)", "nothing  has no value", "$guid  (generated"} {
		if !strings.Contains(hover, want) {
			t.Errorf("the hover lacks %q:\n%s", want, hover)
		}
	}
	if strings.Contains(hover, "hunter2") {
		t.Error("the hover shows a secret")
	}
	// a current value wins over the initial one, and the hover follows
	if err := r.ext.values.Set(context.Background(), "environment:dev", "baseUrl", "http://127.0.0.1:1", false); err != nil {
		t.Fatal(err)
	}
	if h := tab.urlHover(); !strings.Contains(h, "baseUrl = http://127.0.0.1:1  (environment current value)") {
		t.Errorf("hover = %s", h)
	}
	tab.d.url.SetValue("http://plain/url")
	if tab.urlHover() != "" {
		t.Error("a URL without variables has a hover")
	}
	// the preview of the form never asks the keyring
	if n := r.kr.Len(); n != 0 {
		t.Errorf("the keyring has %d entries after only looking", n)
	}
}

func TestFindVars(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"", nil}, {"no vars", nil}, {"{{a}}", []string{"a"}}, {"x{{ a }}y{{b}}", []string{"a", "b"}}, {"{{open", nil}, {"{{$processEnv HOME}}", []string{"$processEnv HOME"}},
	} {
		var got []string
		for _, v := range findVars(tc.text) {
			got = append(got, v.name)
		}
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("findVars(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestTheDevEchoServerAnswersOnLoopbackAndStopsWithTheWindow(t *testing.T) {
	r := newRig(t)
	r.run(CmdSelectEnvironment, "dev")
	r.run(CommandDevEcho, nil)
	cur := r.ext.values.All("environment:dev")
	base := cur["baseUrl"]
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("baseUrl = %q", base)
	}
	resp, err := http.Post(base+"/echo/users?a=1", "application/json", strings.NewReader(`{"name":"Ada"}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%v: %s", err, data)
	}
	if got["method"] != "POST" || got["path"] != "/echo/users" || got["json"].(map[string]any)["name"] != "Ada" || resp.StatusCode != 200 {
		t.Errorf("echo = %v (%d)", got, resp.StatusCode)
	}
	if resp, err = http.Get(base + "/status/418"); err != nil || resp.StatusCode != 418 {
		t.Errorf("status/418: %v %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	if resp, err = http.Get(base + "/status/9"); err != nil || resp.StatusCode != 200 {
		t.Errorf("status/9: %v %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	// without an environment it only says where it is
	r2 := newRig(t)
	r2.run(CommandDevEcho, nil)
	if got := r2.notes(); len(got) != 1 || !strings.Contains(got[0], "Echo server on http://127.0.0.1:") {
		t.Errorf("notes = %v", got)
	}
}

func TestTheDevEchoServerStopsWithTheExtension(t *testing.T) {
	isolate(t)
	folder := t.TempDir()
	copyDir(t, fixtureDir, folder)
	r := activate(t, folder)
	r.run(CmdSelectEnvironment, "dev")
	r.run(CommandDevEcho, nil)
	base := r.ext.values.All("environment:dev")["baseUrl"]
	// the context of the extension ends: the listener closes
	r.cancel()
	r.api.WaitFor("the server to stop", func() bool {
		c := &http.Client{Timeout: 200 * time.Millisecond}
		resp, err := c.Get(base)
		if err == nil {
			_ = resp.Body.Close()
		}
		return err != nil
	})
}

func TestTheDevEchoServerCannotListenAfterTheEnd(t *testing.T) {
	r := newRig(t)
	r.cancel()
	err := r.api.Run(CommandDevEcho, nil)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("HttpDevEchoListenFailed") {
		t.Fatalf("err = %v", err)
	}
	_ = format.Version
}

func TestARequestTabNeedsAFileOrADraft(t *testing.T) {
	r := newRig(t)
	for _, tc := range []struct {
		name  string
		state any
		route string
	}{
		{"no state", nil, "HttpRequestTabStateMissing"},
		{"an empty state", map[string]any{}, "HttpRequestTabStateMissing"},
		{"a state that is not an object", "text", "HttpRequestTabStateInvalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.api.Editors().Open(RequestKindID, tc.state, extension.EditorOpenOptions{})
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute("Extension.createRequestTab."+tc.route) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
