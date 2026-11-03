package httpclient

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
)

// The goldens of the response pane: every tab for a JSON, an XML and an image response, an error status and a redirect chain, in the
// dark and the light theme. The responses are made here, with fixed times and URLs, so the pictures do not change from run to run.

const respW, respH = 860.0, 700.0

var cookieExpires = time.Date(2026, 12, 1, 8, 30, 0, 0, time.UTC)

// fixedTimings are the timings of every response of the goldens.
var fixedTimings = engine.Timings{DNS: 3 * time.Millisecond, Connect: 11 * time.Millisecond, TLS: 24 * time.Millisecond, Sent: time.Millisecond, FirstByte: 62 * time.Millisecond, Download: 17 * time.Millisecond, Total: 118 * time.Millisecond}

// apiResponse is a response of https://api.example.test with a body and its type.
func apiResponse(code int, status, contentType string, body []byte) *engine.Response {
	h := http.Header{}
	h.Set("Content-Type", contentType)
	h.Set("Cache-Control", "no-store")
	h.Set("Server", "example/1.4")
	h.Set("X-Request-Id", "req-7f3a9c")
	req := http.Header{}
	req.Set("Accept", "*/*")
	req.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.secret-token")
	req.Set("User-Agent", "Emetos/dev")
	return &engine.Response{
		Status: status, StatusCode: code, Proto: "HTTP/1.1", Header: h, Body: engine.NewMemoryBody(body), Transferred: int64(len(body)),
		URL: "https://api.example.test/v1/users/17", Request: engine.Sent{Method: "GET", URL: "https://api.example.test/v1/users/17", Header: req, BodySize: -1},
		Timings: fixedTimings,
		TLS:     &engine.TLSInfo{Version: "TLS 1.3", CipherSuite: "TLS_AES_128_GCM_SHA256", ServerName: "api.example.test", Subject: "CN=api.example.test", Issuer: "CN=Example Test CA", NotAfter: time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)},
	}
}

func jsonResponse() *engine.Response {
	r := apiResponse(200, "200 OK", "application/json; charset=utf-8", []byte(`{"id":17,"name":"Ada Example","email":"ada@example.com","roles":["admin","billing"],"active":true,"address":{"city":"London","zip":null},"visits":3}`))
	r.Header.Add("Set-Cookie", "session=abc123; Path=/; HttpOnly")
	r.Header.Add("Set-Cookie", "theme=dark; Path=/app; Secure; SameSite=Lax")
	r.Cookies = []*http.Cookie{
		{Name: "session", Value: "abc123", Path: "/", Domain: "api.example.test", HttpOnly: true},
		{Name: "theme", Value: "dark", Path: "/app", Domain: "api.example.test", Secure: true, SameSite: http.SameSiteLaxMode, Expires: cookieExpires},
	}
	return r
}

func xmlResponse() *engine.Response {
	return apiResponse(200, "200 OK", "application/xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><user id="17"><name>Ada Example</name><email>ada@example.com</email><roles><role>admin</role><role>billing</role></roles><active>true</active><note><![CDATA[a <b> note]]></note></user>`))
}

func errorResponse() *engine.Response {
	r := apiResponse(500, "500 Internal Server Error", "text/plain; charset=utf-8", []byte("panic: runtime error: index out of range [3] with length 3\n\ngoroutine 41 [running]:\nmain.(*Handler).User(0xc000122000, {0x7a5f20, 0xc0001a2000})\n\t/srv/handler.go:88 +0x1c5\n"))
	r.Timings = engine.Timings{Connect: 2 * time.Millisecond, Sent: time.Millisecond, FirstByte: 8 * time.Millisecond, Total: 12 * time.Millisecond, Reused: true}
	r.TLS = nil
	return r
}

// imageResponse is a PNG of 120 x 72 pixels: a blue field with a yellow disc and a white bar.
func imageResponse(t *testing.T) *engine.Response {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 120, 72))
	for y := range 72 {
		for x := range 120 {
			c := color.RGBA{R: 30, G: uint8(60 + x), B: 170, A: 255}
			if dx, dy := x-40, y-36; dx*dx+dy*dy < 22*22 {
				c = color.RGBA{R: 240, G: 200, B: 40, A: 255}
			}
			if x > 70 && x < 110 && y > 30 && y < 42 {
				c = color.RGBA{R: 250, G: 250, B: 250, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	r := apiResponse(200, "200 OK", "image/png", buf.Bytes())
	r.URL = "https://api.example.test/static/logo"
	return r
}

// redirectResponse is a response that came after a redirect and a login redirect.
func redirectResponse() *engine.Response {
	r := jsonResponse()
	r.URL = "https://api.example.test/v2/users/17"
	r.Request.URL = r.URL
	h1, h2 := http.Header{}, http.Header{}
	h1.Set("Location", "/login?next=%2Fv1%2Fusers%2F17")
	h1.Set("Content-Length", "0")
	h2.Set("Location", "https://api.example.test/v2/users/17")
	h2.Set("Set-Cookie", "session=abc123; Path=/; HttpOnly")
	r.Chain = []engine.Hop{
		{Method: "GET", URL: "https://api.example.test/v1/users/17", StatusCode: 302, Status: "302 Found", Header: h1, Reason: engine.HopRedirect, Timings: engine.Timings{Total: 31 * time.Millisecond}},
		{Method: "GET", URL: "https://api.example.test/login?next=%2Fv1%2Fusers%2F17", StatusCode: 301, Status: "301 Moved Permanently", Header: h2, Reason: engine.HopRedirect, Timings: engine.Timings{Total: 19 * time.Millisecond}},
	}
	return r
}

// respond hands a response to the tab the way the end of a send does.
func (r *rig) respond(tab *reqTab, resp *engine.Response) {
	r.t.Helper()
	tab.finishSend(sendJob{files: tab.d.files()}, sendOutcome{resp: resp, duration: 118 * time.Millisecond})
}

func goldenResp(t *testing.T, name string, resp func(t *testing.T) *engine.Response, show func(tab *reqTab), w, h float64) {
	t.Helper()
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r, tab := reqRig(t, pCreate)
			tab.d.method = "GET"
			tab.d.url.SetValue("{{baseUrl}}/v1/users/17")
			tab.baseline = tab.d.snapshot()
			r.respond(tab, resp(t))
			if show != nil {
				show(tab)
			}
			ts := r.showReq(th, tab, w, h)
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, name+"_"+th.Name)
		})
	}
}

func fixed(f func() *engine.Response) func(t *testing.T) *engine.Response {
	return func(*testing.T) *engine.Response { return f() }
}

func inTab(id string) func(tab *reqTab) { return func(tab *reqTab) { tab.pane.tab = id } }

func inMode(mode string) func(tab *reqTab) {
	return func(tab *reqTab) {
		tab.pane.tab = respBody
		tab.setMode(mode)
	}
}

func TestGoldenTheResponsePaneOfAJSONResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		show func(tab *reqTab)
	}{
		{"pretty", nil},
		{"raw", inMode(modeRaw)},
		{"headers", inTab(respHeaders)},
		{"cookies", inTab(respCookies)},
		{"timeline", inTab(respTimeline)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			goldenResp(t, "response_json_"+tc.name, fixed(jsonResponse), tc.show, respW, respH)
		})
	}
}

func TestGoldenTheResponsePaneOfAnXMLResponse(t *testing.T) {
	t.Run("pretty", func(t *testing.T) { goldenResp(t, "response_xml_pretty", fixed(xmlResponse), nil, respW, respH) })
	t.Run("raw", func(t *testing.T) {
		goldenResp(t, "response_xml_raw", fixed(xmlResponse), inMode(modeRaw), respW, respH)
	})
}

func TestGoldenTheResponsePaneOfAnImage(t *testing.T) {
	t.Run("preview", func(t *testing.T) { goldenResp(t, "response_image_preview", imageResponse, nil, respW, respH) })
}

func TestGoldenTheResponsePaneOfAnErrorStatus(t *testing.T) {
	t.Run("body", func(t *testing.T) { goldenResp(t, "response_error_body", fixed(errorResponse), nil, respW, respH) })
	t.Run("timeline", func(t *testing.T) {
		goldenResp(t, "response_error_timeline", fixed(errorResponse), inTab(respTimeline), respW, respH)
	})
}

func TestGoldenTheResponsePaneOfARedirectChain(t *testing.T) {
	t.Run("timeline", func(t *testing.T) {
		goldenResp(t, "response_redirect_timeline", fixed(redirectResponse), inTab(respTimeline), respW, 1000)
	})
	t.Run("body", func(t *testing.T) {
		goldenResp(t, "response_redirect_body", fixed(redirectResponse), nil, respW, respH)
	})
}

func TestGoldenTheResponseBesideTheRequest(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r, tab := reqRig(t, pCreate)
			r.api.SetSetting(SettingResponseLayout, "horizontal")
			tab.d.method = "GET"
			tab.d.url.SetValue("{{baseUrl}}/v1/users/17")
			tab.baseline = tab.d.snapshot()
			r.respond(tab, jsonResponse())
			ts := r.showReq(th, tab, 1240, 520)
			ts.Pump(time.Millisecond)
			uitest.MatchesGolden(ts, "response_beside_"+th.Name)
		})
	}
}

func TestGoldenTheLargeBodyInPages(t *testing.T) {
	var big strings.Builder
	for i := range 6000 {
		big.WriteString("2026-11-03T14:00:00Z INFO request ")
		big.WriteString(strings.Repeat("x", i%7))
		big.WriteString(" served in 3ms\n")
	}
	r := apiResponse(200, "200 OK", "text/plain", []byte(big.String()))
	r.Body = engine.NewMemoryBody([]byte(strings.Repeat(big.String(), 3)))
	goldenResp(t, "response_paged_raw", func(*testing.T) *engine.Response { return r }, func(tab *reqTab) {
		tab.setMode(modeRaw)
		tab.goPage(2)
	}, respW, 560)
}
