package httpclient

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// The goldens are the dark and the light theme of each view; they are drawn by Glutter's software renderer, so they need no display.

const sideW, sideH = 300.0, 420.0

func TestGoldenTheCollectionsView(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			r.ext.collections.tree.Expand(r.col(pUsers))
			r.ext.collections.tree.SetSelected(r.col(pList))
			r.ext.collections.rebuild()
			ts := r.show(th, CollectionsViewID, sideW, sideH)
			uitest.MatchesGolden(ts, "collections_"+th.Name)
		})
	}
}

func TestGoldenTheCollectionsViewWithProblems(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, func(folder string) {
				writeFile(t, folder+"/.emetos/http/collections/payments/broken.request.jsonc", `{"emetosHttp": 1, "name": `)
				writeFile(t, folder+"/.emetos/http/collections/payments/newer.request.jsonc", `{"emetosHttp": 9, "name": "From the future", "method": "GET", "url": "x"}`)
				writeFile(t, folder+"/.emetos/http/collections/payments/trace.request.jsonc", `{"emetosHttp": 1, "name": "A method of its own", "method": "PROPFIND", "url": "x"}`)
			})
			ts := r.show(th, CollectionsViewID, sideW, sideH)
			uitest.MatchesGolden(ts, "collections_problems_"+th.Name)
		})
	}
}

func TestGoldenTheCollectionsViewWhenEmpty(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			isolate(t)
			r := activate(t, t.TempDir())
			ts := r.show(th, CollectionsViewID, sideW, 200)
			uitest.MatchesGolden(ts, "collections_empty_"+th.Name)
		})
	}
}

func TestGoldenTheEnvironmentsView(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			r.run(CmdSelectEnvironment, "dev")
			ts := r.show(th, EnvironmentsViewID, sideW, 160)
			uitest.MatchesGolden(ts, "environments_"+th.Name)
		})
	}
}

func TestGoldenTheEnvironmentsViewWhenEmpty(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			isolate(t)
			r := activate(t, t.TempDir())
			ts := r.show(th, EnvironmentsViewID, sideW, 200)
			uitest.MatchesGolden(ts, "environments_empty_"+th.Name)
		})
	}
}

// seedHistory adds the requests of the golden of the History view.
func (r *rig) seedHistory() {
	r.t.Helper()
	add := func(ago time.Duration, method, url string, status int, text string) {
		s := sentAt(ago, method, url, status)
		s.StatusText = text
		r.record(s)
	}
	add(80*time.Hour, "GET", "https://api.example.com/v1/orders?page=2", 200, "OK")
	add(26*time.Hour, "GET", "https://api.example.com/v1/users", 200, "OK")
	add(25*time.Hour, "DELETE", "https://api.example.com/v1/users/17", 404, "Not Found")
	add(3*time.Hour, "POST", "https://api.example.com/v1/users", 201, "Created")
	add(2*time.Hour, "PUT", "https://api.example.com/v1/users/17", 200, "OK")
	add(50*time.Minute, "PATCH", "https://api.example.com/v1/users/17?token=abc123", 500, "Internal Server Error")
	failed := sentAt(10*time.Minute, "GET", "{{baseUrl}}/v1/health", 0)
	failed.Err = "connection refused"
	r.record(failed)
}

func TestGoldenTheHistoryView(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			r.seedHistory()
			r.ext.historyView.tree.SetSelected(entryKey(3))
			ts := r.show(th, HistoryViewID, sideW, sideH)
			uitest.MatchesGolden(ts, "history_"+th.Name)
		})
	}
}

func TestGoldenTheHistoryViewWhenEmpty(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			ts := r.show(th, HistoryViewID, sideW, 120)
			uitest.MatchesGolden(ts, "history_empty_"+th.Name)
		})
	}
}

func TestGoldenTheEnvironmentEditor(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			if err := r.ext.values.Set(context.Background(), variables.EnvironmentOwner("dev"), "baseUrl", "http://127.0.0.1:9090", false); err != nil {
				t.Fatal(err)
			}
			r.run(CmdEditEnvironment, r.env("dev"))
			tab := r.ext.envTabs[0]
			ts := r.showEnv(th, tab, 900, 340)
			uitest.MatchesGolden(ts, "environment_"+th.Name)
		})
	}
}

func TestGoldenTheEnvironmentEditorWithEdits(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t)
			r.run(CmdEditEnvironment, r.env("staging"))
			tab := r.ext.envTabs[0]
			ts := r.showEnv(th, tab, 900, 300)
			tab.addRow()
			tab.rows[1].name.SetValue("token")
			tab.rows[1].secret = true
			tab.rows[1].current.SetValue("hunter2-secret")
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "environment_edited_"+th.Name)
		})
	}
}
