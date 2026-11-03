package httpclient

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// The goldens of the request tab: the dark and the light theme of each body kind and each type of authorization, and of the other tabs.

const reqW, reqH = 860.0, 470.0

var (
	reStringTok = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	reNumberTok = regexp.MustCompile(`\b\d+(?:\.\d+)?\b`)
	reWordTok   = regexp.MustCompile(`\b(?:true|false|null)\b`)
	reTagTok    = regexp.MustCompile(`</?[A-Za-z][\w:-]*|/?>`)
)

// tokenizeForTests colors strings, numbers, words and tags, in the scopes the token themes know.
func tokenizeForTests(_, text string) ([][]extension.Token, error) {
	var out [][]extension.Token
	for _, line := range strings.Split(text, "\n") {
		var toks []extension.Token
		for _, c := range []struct {
			re    *regexp.Regexp
			scope string
		}{{reTagTok, "entity.name.tag"}, {reNumberTok, "constant.numeric"}, {reWordTok, "constant.language"}, {reStringTok, "string.quoted.double"}} {
			for _, m := range c.re.FindAllStringIndex(line, -1) {
				toks = append(toks, extension.Token{Start: m[0], End: m[1], Scopes: []string{c.scope}})
			}
		}
		out = append(out, toks)
	}
	return out, nil
}

// reqRig is a rig with the Dev environment on, the tokenizer of the tests and the request tab of a request opened.
func reqRig(t *testing.T, rel string) (*rig, *reqTab) {
	t.Helper()
	r := newRig(t)
	r.api.SetTokenizer(tokenizeForTests)
	r.run(CmdSelectEnvironment, "dev")
	return r, r.openReq(rel)
}

func goldenReq(t *testing.T, name string, prepare func(r *rig, tab *reqTab), w, h float64) {
	t.Helper()
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r, tab := reqRig(t, pCreate)
			if prepare != nil {
				prepare(r, tab)
			}
			ts := r.showReq(th, tab, w, h)
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, name+"_"+th.Name)
		})
	}
}

const jsonBody = "{\n  \"name\": \"Ada Example\",\n  \"email\": \"ada@example.com\",\n  \"roles\": [\"admin\", \"billing\"],\n  \"active\": true,\n  \"visits\": 3\n}\n"

func TestGoldenTheRequestTabWithEachBody(t *testing.T) {
	for _, tc := range []struct {
		kind    string
		prepare func(tab *reqTab)
	}{
		{format.BodyNone, func(tab *reqTab) { tab.d.bodyType = format.BodyNone }},
		{format.BodyJSON, func(tab *reqTab) { tab.d.texts[format.BodyJSON].SetValue(jsonBody) }},
		{format.BodyXML, func(tab *reqTab) {
			tab.d.bodyType = format.BodyXML
			tab.d.texts[format.BodyXML].SetValue("<user id=\"17\">\n  <name>Ada Example</name>\n  <active>true</active>\n</user>\n")
		}},
		{format.BodyText, func(tab *reqTab) {
			tab.d.bodyType = format.BodyText
			tab.d.texts[format.BodyText].SetValue("Plain text, sent as it is.\nHello {{baseUrl}}")
		}},
		{format.BodyHTML, func(tab *reqTab) {
			tab.d.bodyType = format.BodyHTML
			tab.d.texts[format.BodyHTML].SetValue("<p class=\"note\">Hello</p>\n")
		}},
		{format.BodyJavaScript, func(tab *reqTab) {
			tab.d.bodyType = format.BodyJavaScript
			tab.d.texts[format.BodyJavaScript].SetValue("const answer = 42;\nconsole.log(\"answer\", answer);\n")
		}},
		{format.BodyGraphQL, func(tab *reqTab) {
			tab.d.bodyType = format.BodyGraphQL
			tab.d.texts[format.BodyGraphQL].SetValue("query User($id: ID!) {\n  user(id: $id) {\n    name\n    email\n  }\n}\n")
			tab.d.gqlVars.SetValue("{\n  \"id\": 17\n}\n")
		}},
		{format.BodyForm, func(tab *reqTab) {
			tab.d.bodyType = format.BodyForm
			tab.d.form = []*pairBox{newPairBox(format.Pair{Name: "username", Value: "ada", Enabled: true}), newPairBox(format.Pair{Name: "remember", Value: "1", Enabled: false})}
		}},
		{format.BodyMultipart, func(tab *reqTab) {
			tab.d.bodyType = format.BodyMultipart
			tab.d.multi = []*pairBox{newPairBox(format.Pair{Name: "title", Value: "Report", Enabled: true}), newPairBox(format.Pair{Name: "attachment", File: "docs/report.pdf", Enabled: true})}
		}},
		{format.BodyBinary, func(tab *reqTab) {
			tab.d.bodyType = format.BodyBinary
			tab.d.binary.SetValue("assets/logo.png")
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			goldenReq(t, "request_body_"+tc.kind, func(_ *rig, tab *reqTab) {
				tab.section = secBody
				tc.prepare(tab)
				tab.baseline = tab.d.snapshot()
			}, reqW, map[bool]float64{true: 580, false: reqH}[tc.kind == format.BodyGraphQL])
		})
	}
}

func TestGoldenTheRequestTabWithEachAuth(t *testing.T) {
	for _, tc := range []struct {
		typ     string
		prepare func(tab *reqTab)
	}{
		{format.AuthInherit, func(tab *reqTab) {}},
		{format.AuthNone, func(tab *reqTab) {}},
		{format.AuthBasic, func(tab *reqTab) {
			tab.d.auth.box[fUsername].SetValue("ada")
			tab.d.auth.box[fPassword].SetValue("{{password}}")
		}},
		{format.AuthBearer, func(tab *reqTab) { tab.d.auth.box[fToken].SetValue("{{apiKey}}") }},
		{format.AuthAPIKey, func(tab *reqTab) {
			tab.d.auth.box[fKey].SetValue("X-API-Key")
			tab.d.auth.box[fValue].SetValue("abc123")
			tab.d.auth.in = "query"
		}},
		{format.AuthDigest, func(tab *reqTab) {
			tab.d.auth.box[fUsername].SetValue("ada")
			tab.d.auth.box[fPassword].SetValue("pw")
		}},
		{format.AuthOAuth2, func(tab *reqTab) {
			tab.d.auth.box[fTokenURL].SetValue("https://auth.example.com/oauth/token")
			tab.d.auth.box[fClientID].SetValue("emetos")
			tab.d.auth.box[fClientSecret].SetValue("shh")
			tab.d.auth.box[fScope].SetValue("read write")
		}},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			goldenReq(t, "request_auth_"+tc.typ, func(_ *rig, tab *reqTab) {
				tab.section = secAuth
				tab.d.auth.typ = tc.typ
				tc.prepare(tab)
				tab.baseline = tab.d.snapshot()
			}, reqW, map[bool]float64{true: 520, false: 380}[tc.typ == format.AuthOAuth2])
		})
	}
}

func TestGoldenTheOtherTabsOfTheRequest(t *testing.T) {
	t.Run("params", func(t *testing.T) {
		goldenReq(t, "request_params", func(_ *rig, tab *reqTab) {
			tab.d.method = "GET"
			tab.d.url.SetValue("{{baseUrl}}/v1/users?page=2&size=50&unknown={{nothing}}")
			tab.d.params[1].desc.SetValue("rows per page")
			tab.toggleRow(tab.d.params[1], false, secParams)
			tab.baseline = tab.d.snapshot()
		}, reqW, 330)
	})
	t.Run("params in bulk", func(t *testing.T) {
		goldenReq(t, "request_params_bulk", func(_ *rig, tab *reqTab) {
			tab.d.url.SetValue("{{baseUrl}}/v1/users?page=2&size=50")
			tab.toggleBulk(secParams)
			tab.baseline = tab.d.snapshot()
		}, reqW, 330)
	})
	t.Run("headers", func(t *testing.T) {
		goldenReq(t, "request_headers", func(_ *rig, tab *reqTab) {
			tab.section = secHeaders
			tab.d.headers = []*pairBox{newPairBox(format.Pair{Name: "Accept", Value: "application/json", Enabled: true}), newPairBox(format.Pair{Name: "X-Trace", Value: "{{traceId}}", Description: "for the logs", Enabled: true})}
			tab.baseline = tab.d.snapshot()
		}, reqW, 470)
	})
	t.Run("settings", func(t *testing.T) {
		goldenReq(t, "request_settings", func(_ *rig, tab *reqTab) {
			tab.section = secSettings
			tab.d.timeout.SetValue("5000")
			tab.d.verify = triNo
			tab.baseline = tab.d.snapshot()
		}, reqW, 330)
	})
	t.Run("extract", func(t *testing.T) {
		goldenReq(t, "request_extract", func(_ *rig, tab *reqTab) {
			tab.section = secExtract
			tab.d.req.Extract = []format.ExtractRule{{From: "body", Path: "$.id", To: "userId"}}
			tab.baseline = tab.d.snapshot()
		}, reqW, 220)
	})
	t.Run("edited with a sent status", func(t *testing.T) {
		goldenReq(t, "request_edited", func(_ *rig, tab *reqTab) {
			tab.section = secBody
			tab.d.url.SetValue("{{baseUrl}}/v1/users/{{userId}}")
			tab.send.status, tab.send.code = "201 Created  ·  128 ms  ·  1.2 KB  ·  no value for userId", 201
			tab.tab.SetDirty(true)
		}, reqW, reqH)
	})
	t.Run("sending", func(t *testing.T) {
		goldenReq(t, "request_sending", func(_ *rig, tab *reqTab) {
			tab.send.sending, tab.send.status = true, "Sending..."
		}, reqW, 260)
	})
	t.Run("error", func(t *testing.T) {
		goldenReq(t, "request_error", func(_ *rig, tab *reqTab) {
			tab.send.status, tab.send.bad = "the server did not answer: connection refused", true
		}, reqW, 260)
	})
	t.Run("a method of its own", func(t *testing.T) {
		goldenReq(t, "request_custom_method", func(_ *rig, tab *reqTab) { tab.setMethod("propfind") }, reqW, 260)
	})
	t.Run("read only", func(t *testing.T) {
		goldenReq(t, "request_readonly", func(_ *rig, tab *reqTab) { tab.readOnly = true }, reqW, 260)
	})
}
