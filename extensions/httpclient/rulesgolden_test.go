package httpclient

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// The goldens of the Extract and Checks tabs of the request and of the Checks tab of the response, in the dark and the light theme.

// goldenRules is a request with every kind of rule and check, and the response of the goldens (jsonResponse) it was sent to.
var (
	goldenExtract = []format.ExtractRule{
		{From: "body", Path: "$.id", To: "userId"},
		{From: "header", Header: "X-Request-Id", Regex: `req-(\w+)`, To: "requestId", Scope: "collection"},
		{From: "status", To: "lastStatus", Scope: "global"},
		{From: "cookie", Cookie: "session", To: "session"},
		{From: "body", Path: "$.profile.token", To: "token"},
	}
	goldenChecks = []format.Check{
		{Subject: "status", Op: "equals", Value: 200.0},
		{Subject: "time", Op: "lessThan", Value: 500.0},
		{Subject: "header", Header: "Content-Type", Op: "contains", Value: "json"},
		{Subject: "json", Path: "$.name", Op: "equals", Value: "Grace Example"},
		{Subject: "json", Path: "$.roles", Op: "isType", Value: "array"},
		{Subject: "json", Path: "$.visits", Op: "greaterThan", Value: 5.0},
		{Subject: "json", Path: "$.email", Op: "matches", Value: `@example\.com$`},
		{Subject: "json", Path: "$.address.zip", Op: "exists"},
		{Subject: "body", Op: "contains", Value: "billing"},
	}
)

func TestGoldenTheExtractAndChecksTabsOfTheRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		section string
		h       float64
		how     string
	}{
		{"request_extract_rules", secExtract, 400, "rules"},
		{"request_extract_after_send", secExtract, 460, "outcomes"},
		{"request_checks_rules", secChecks, 520, "rules"},
		{"request_checks_after_send", secChecks, 700, "outcomes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			goldenReqRules(t, tc.name, tc.section, tc.h, tc.how, nil)
		})
	}
}

func TestGoldenTheChecksTabOfTheResponse(t *testing.T) {
	t.Run("passed and failed", func(t *testing.T) {
		goldenReqRules(t, "response_checks", secParams, 700, "response", func(tab *reqTab) { tab.pane.tab = respChecks })
	})
	t.Run("all passed", func(t *testing.T) {
		for _, th := range uitest.Themes() {
			t.Run(th.Name, func(t *testing.T) {
				r, tab := reqRig(t, pCreate)
				r.respondWithRules(tab, jsonResponse(), nil, []format.Check{goldenChecks[0], goldenChecks[1], goldenChecks[2], goldenChecks[4]})
				tab.pane.tab = respChecks
				ts := r.showReq(th, tab, respW, 520)
				ts.Pump(time.Millisecond)
				uitest.MatchesGolden(ts, "response_checks_passed_"+th.Name)
			})
		}
	})
	t.Run("no checks", func(t *testing.T) {
		goldenResp(t, "response_checks_none", fixed(jsonResponse), inTab(respChecks), respW, 480)
	})
}

func goldenReqRules(t *testing.T, name, section string, h float64, how string, show func(tab *reqTab)) {
	t.Helper()
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r, tab := reqRig(t, pCreate)
			tab.d.method = "GET"
			tab.d.url.SetValue("{{baseUrl}}/v1/users/17")
			tab.section = section
			switch how {
			case "response":
				r.respondWithRules(tab, jsonResponse(), goldenExtract, goldenChecks)
			case "outcomes":
				r.recordOutcomes(tab, jsonResponse(), goldenExtract, goldenChecks)
			default:
				tab.setRules(goldenExtract, goldenChecks)
			}
			tab.baseline = tab.d.snapshot()
			tab.tab.SetDirty(false)
			if show != nil {
				show(tab)
			}
			ts := r.showReq(th, tab, respW, h)
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, name+"_"+th.Name)
		})
	}
}
