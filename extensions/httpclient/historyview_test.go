package httpclient

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/history"
)

func pair(name, value string) format.Pair {
	return format.Pair{Name: name, Value: value, Enabled: true}
}

// sentAt is a request that was sent some time before testNow.
func sentAt(ago time.Duration, method, url string, status int) history.Sent {
	return history.Sent{
		Time: testNow.Add(-ago), Request: format.Request{Name: method + " " + url, Method: method, URL: url},
		Status: status, StatusText: "OK", Duration: 40 * time.Millisecond, Size: 1500,
	}
}

func (r *rig) record(s history.Sent) {
	r.t.Helper()
	if err := r.ext.RecordSent(s); err != nil {
		r.t.Fatal(err)
	}
}

func TestTheHistoryIsGroupedByDayNewestFirst(t *testing.T) {
	r := newRig(t)
	r.record(sentAt(4*24*time.Hour, "GET", "https://h/old", 200))
	r.record(sentAt(26*time.Hour, "GET", "https://h/yesterday", 200))
	r.record(sentAt(2*time.Hour, "POST", "https://h/a", 201))
	r.record(sentAt(time.Hour, "DELETE", "https://h/b", 404))
	var labels []string
	for _, row := range r.ext.historyView.tree.Rows() {
		labels = append(labels, row.Label)
	}
	want := []string{"Today", "DELETE https://h/b", "POST https://h/a", "Yesterday", "GET https://h/yesterday", "Fri, 30 Oct 2026", "GET https://h/old"}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Errorf("rows = %q\nwant   %q", labels, want)
	}
}

func TestTheHistoryLimitIsASetting(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 8; i++ {
		r.record(sentAt(time.Duration(i)*time.Minute, "GET", "https://h/"+string(rune('a'+i)), 200))
	}
	r.api.SetSetting(SettingHistoryLimit, 3)
	r.api.Pump()
	if got := len(r.ext.historyView.entries); got != 3 {
		t.Errorf("%d entries with a limit of 3", got)
	}
	r.api.SetSetting(SettingHistoryLimit, 0)
	r.api.Pump()
	r.record(sentAt(0, "GET", "https://h/z", 200))
	if got := len(r.ext.historyView.entries); got != 0 {
		t.Errorf("%d entries with a limit of 0", got)
	}
}

// The state holds the masked request and never the secret (SPEC §10.33: secrets and Authorization values are masked).
func TestWhatTheHistoryKeepsIsMasked(t *testing.T) {
	for _, tc := range []struct {
		name    string
		req     format.Request
		body    string
		secrets []string
		keeps   []string
	}{
		{"an Authorization header", format.Request{Method: "GET", URL: "https://h/x", Headers: []format.Pair{pair("Authorization", "Bearer eyJhbGciOi.payload.sig")}},
			"", []string{"eyJhbGciOi"}, []string{"Bearer ********"}},
		{"a bearer token of the auth tab", format.Request{Method: "GET", URL: "https://h/x", Auth: &format.Auth{Type: format.AuthBearer, Token: "tok-123456"}},
			"", []string{"tok-123456"}, []string{`"type":"bearer"`}},
		{"a variable stays so the request can be sent again", format.Request{Method: "GET", URL: "https://h/x", Headers: []format.Pair{pair("Authorization", "Bearer {{token}}")}},
			"", nil, []string{"Bearer {{token}}"}},
		{"a password in the URL", format.Request{Method: "GET", URL: "https://ada:hunter2@h/x?api_key=KEY123&q=1"},
			"", []string{"hunter2", "KEY123"}, []string{"q=1"}},
		{"a basic auth password", format.Request{Method: "GET", URL: "https://h/x", Auth: &format.Auth{Type: format.AuthBasic, Username: "ada", Password: "pw-12345"}},
			"", []string{"pw-12345"}, []string{`"username":"ada"`}},
		{"a password in a JSON body", format.Request{Method: "POST", URL: "https://h/x", Body: &format.Body{Type: format.BodyJSON, File: "b.body.json"}},
			`{"user":"ada","password":"pw-67890"}`, []string{"pw-67890"}, []string{"ada"}},
		{"a cookie", format.Request{Method: "GET", URL: "https://h/x", Headers: []format.Pair{pair("Cookie", "sid=abcdef")}},
			"", []string{"abcdef"}, nil},
		{"a secret of an API key", format.Request{Method: "GET", URL: "https://h/x", Auth: &format.Auth{Type: format.AuthAPIKey, Key: "X-Key", Value: "key-98765", In: "header"}},
			"", []string{"key-98765"}, []string{"X-Key"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			s := history.Sent{Time: testNow, Request: tc.req, Body: tc.body, Status: 200}
			r.record(s)
			var all strings.Builder
			for _, key := range []string{history.StateKey, history.StateKey + ".0"} {
				if raw, ok := r.api.State().Get(key); ok {
					all.Write(raw)
				}
			}
			text := all.String()
			for _, secret := range tc.secrets {
				if strings.Contains(text, secret) {
					t.Errorf("the state holds %q: %s", secret, text)
				}
			}
			for _, keep := range tc.keeps {
				if !strings.Contains(text, keep) {
					t.Errorf("the state lacks %q: %s", keep, text)
				}
			}
			// the one that is shown has no secret either
			for _, en := range r.ext.historyView.entries {
				for _, secret := range tc.secrets {
					if strings.Contains(entryTip(en)+shortURL(en.URL), secret) {
						t.Errorf("the view shows %q", secret)
					}
				}
			}
		})
	}
}

func TestOpeningAnEntryMakesAnUnsavedRequest(t *testing.T) {
	r := newRig(t)
	s := sentAt(time.Hour, "POST", "https://h/users?token=abc", 201)
	s.Request.Headers = []format.Pair{pair("Authorization", "Bearer abc")}
	s.Request.Body = &format.Body{Type: format.BodyJSON, File: "x.body.json"}
	s.Body = `{"name":"Ada"}`
	r.record(s)
	r.run(CmdOpenHistory, nil)
	if len(r.api.LastPick().Labels()) != 1 {
		t.Fatalf("labels = %v", r.api.LastPick().Labels())
	}
	r.choose("POST")
	docs := r.api.UntitledDocuments()
	if len(docs) != 1 || docs[0].LanguageID != "jsonc" {
		t.Fatalf("untitled = %+v", docs)
	}
	for _, want := range []string{`"method": "POST"`, "token=********", "Bearer ********", `{"name":"Ada"}`} {
		if !strings.Contains(docs[0].Text, want) {
			t.Errorf("the request lacks %q:\n%s", want, docs[0].Text)
		}
	}
	// by the number of the entry
	r.run(CmdOpenHistory, map[string]any{"seq": 0})
	if len(r.api.UntitledDocuments()) != 2 {
		t.Error("an entry named by its number was not opened")
	}
	r.run(CmdOpenHistory, map[string]any{"seq": 99})
	if got := r.notes(); len(got) == 0 || !strings.Contains(got[len(got)-1], "not in the history any more") {
		t.Errorf("notes = %v", got)
	}
}

func TestAnEmptyHistoryHasNothingToOpen(t *testing.T) {
	r := newRig(t)
	r.run(CmdOpenHistory, nil)
	r.run(CmdClearHistory, nil)
	got := r.notes()
	if len(got) != 2 || !strings.Contains(got[0], "history is empty") || !strings.Contains(got[1], "already empty") {
		t.Errorf("notes = %v", got)
	}
	if len(r.api.OpenDialogs()) != 0 {
		t.Error("an empty history asked before it was cleared")
	}
}

func TestClearingTheHistoryAsksFirst(t *testing.T) {
	r := newRig(t)
	r.record(sentAt(time.Hour, "GET", "https://h/a", 200))
	r.record(sentAt(time.Minute, "GET", "https://h/b", 200))
	r.run(CmdClearHistory, nil)
	r.pressDialog("Cancel")
	if len(r.ext.hist.List()) != 2 {
		t.Fatal("a cancelled dialog cleared the history")
	}
	r.run(CmdClearHistory, nil)
	r.pressDialog("Clear History")
	if len(r.ext.hist.List()) != 0 || len(r.ext.historyView.entries) != 0 {
		t.Error("the history was not cleared")
	}
	// and the collections are untouched
	if len(r.ext.tree.Collections) != 2 {
		t.Error("clearing the history touched the collections")
	}
}

func TestRecordingARequestWithoutAURLFails(t *testing.T) {
	r := newRig(t)
	if err := r.ext.RecordSent(history.Sent{Request: format.Request{Method: "GET"}}); err == nil {
		t.Error("an empty request was recorded")
	}
}

func TestTheDevCommandSeedsTheHistory(t *testing.T) {
	if !foundation.DebugMode {
		t.Skip("the dev command exists in debug builds")
	}
	r := newRig(t)
	r.run(CommandDevSeedHistory, nil)
	if got := len(r.ext.historyView.entries); got != 6 {
		t.Errorf("%d entries", got)
	}
}

func TestSavingAnEntryToACollection(t *testing.T) {
	r := newRig(t)
	s := sentAt(time.Hour, "POST", "https://h/users", 201)
	s.Request.Name = ""
	s.Request.Headers = []format.Pair{pair("Authorization", "Bearer abc"), pair("Accept", "application/json")}
	s.Request.Body = &format.Body{Type: format.BodyJSON, File: "x.body.json"}
	s.Body = `{"name":"Ada"}`
	r.record(s)
	r.run(CmdSaveHistory, nil)
	r.choose("POST")
	groups := r.api.LastPick().Labels()
	if len(groups) != 3 || !strings.Contains(groups[0], "Payments") || !strings.Contains(groups[2], "Users") {
		t.Fatalf("collections and folders = %v", groups)
	}
	r.choose("Users")
	file := r.col("shop-api/users/post-users.request.jsonc")
	if !r.has(file) {
		t.Fatalf("the request is not saved; notes %v", r.notes())
	}
	text := r.read(file)
	for _, want := range []string{`"name": "POST /users"`, `"method": "POST"`, "Bearer ********", "Accept"} {
		if !strings.Contains(text, want) {
			t.Errorf("the saved request lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Bearer abc") {
		t.Error("the secret was saved")
	}
	if body := r.read(r.col("shop-api/users/post-users.body.json")); body != `{"name":"Ada"}` {
		t.Errorf("body file = %q", body)
	}
	notes := r.api.Notes()
	if last := notes[len(notes)-1]; !strings.Contains(last.Message, "masked") || last.Actions[0] != "Open" {
		t.Errorf("notes = %v", r.notes())
	}
	// by the number of the entry; the folder is picked again
	r.run(CmdSaveHistory, map[string]any{"seq": 0})
	r.choose("Payments")
	if !r.has(r.col("payments/post-users.request.jsonc")) {
		t.Error("the entry named by its number was not saved")
	}
}

func TestSavingAnEntryWithoutACollectionSaysSo(t *testing.T) {
	r := newRig(t, func(folder string) {
		if err := removeAll(folder + "/.emetos/http/collections"); err != nil {
			t.Fatal(err)
		}
	})
	r.record(sentAt(time.Hour, "GET", "https://h/a", 200))
	r.run(CmdSaveHistory, nil)
	r.choose("GET")
	if got := r.notes(); len(got) != 1 || !strings.Contains(got[0], "no collection") {
		t.Errorf("notes = %v", got)
	}
}
