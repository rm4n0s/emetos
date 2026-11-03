package history

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

func TestMaskValue(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"empty", "", ""},
		{"a variable", "{{token}}", "{{token}}"},
		{"two variables", "{{a}} {{b}}", "{{a}} {{b}}"},
		{"bearer with a variable", "Bearer {{token}}", "Bearer {{token}}"},
		{"bearer with a literal", "Bearer abc.def.ghi", "Bearer " + Masked},
		{"basic literal", "Basic dXNlcjpwYXNz", "Basic " + Masked},
		{"bearer in other case", "bearer abc", "bearer " + Masked},
		{"a literal", "hunter2", Masked},
		{"a literal and a variable", "pre-{{x}}", Masked},
		{"a scheme alone", "Bearer", Masked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskValue(tc.in); got != tc.want {
				t.Errorf("MaskValue(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSensitive(t *testing.T) {
	for name, want := range map[string]bool{
		"Authorization": true, "proxy-authorization": true, "Cookie": true, "X-API-Key": true, "api_key": true, "access_token": true,
		"client_secret": true, "Password": true, "X-Session-Id": true, "Content-Type": false, "Accept": false, "name": false, "": false,
	} {
		if got := Sensitive(name); got != want {
			t.Errorf("Sensitive(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestMaskURL(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"nothing to mask", "https://api.example.com/users?page=2", "https://api.example.com/users?page=2"},
		{"a token", "https://api.example.com/x?token=abc123&page=2", "https://api.example.com/x?token=" + Masked + "&page=2"},
		{"a variable stays", "{{base}}/x?api_key={{key}}", "{{base}}/x?api_key={{key}}"},
		{"a password in the user information", "https://ada:s3cret@example.com/x", "https://ada:" + Masked + "@example.com/x"},
		{"a user without a password", "https://ada@example.com/x", "https://ada@example.com/x"},
		{"an encoded name", "https://h/x?access%5Ftoken=zzz", "https://h/x?access%5Ftoken=" + Masked},
		{"a fragment", "https://h/x?secret=1#top", "https://h/x?secret=" + Masked + "#top"},
		{"a name without a value", "https://h/x?token", "https://h/x?token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskURL(tc.in); got != tc.want {
				t.Errorf("MaskURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMaskBody(t *testing.T) {
	for _, tc := range []struct{ name, kind, in, want string }{
		{"json password", format.BodyJSON, `{"user":"ada","password":"hunter2"}`, `{"user":"ada","password":"` + Masked + `"}`},
		{"json reference", format.BodyJSON, `{"password": "{{pw}}"}`, `{"password": "{{pw}}"}`},
		{"nested", format.BodyJSON, `{"auth":{"client_secret":"abc","id":"x"}}`, `{"auth":{"client_secret":"` + Masked + `","id":"x"}}`},
		{"free text", format.BodyText, `password: hunter2`, `password: hunter2`},
		{"no match", format.BodyJSON, `{"name":"ada"}`, `{"name":"ada"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskBody(tc.kind, tc.in); got != tc.want {
				t.Errorf("MaskBody = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMaskRequest(t *testing.T) {
	enabled := func(n, v string) format.Pair { return format.Pair{Name: n, Value: v, Enabled: true} }
	in := format.Request{
		Name: "login", Method: "POST", URL: "https://ada:pw@h/login?api_key=k1&q=1",
		Params:  []format.Pair{enabled("token", "t1"), enabled("q", "1")},
		Headers: []format.Pair{enabled("Authorization", "Bearer abc"), enabled("Accept", "*/*"), enabled("Cookie", "{{cookie}}")},
		Auth:    &format.Auth{Type: format.AuthBasic, Username: "ada", Password: "pw"},
		Body:    &format.Body{Type: format.BodyForm, Fields: []format.Pair{enabled("password", "hunter2"), enabled("user", "ada")}},
	}
	got := MaskRequest(in)
	if strings.Contains(got.URL, "pw@") || strings.Contains(got.URL, "k1") {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Params[0].Value != Masked || got.Params[1].Value != "1" {
		t.Errorf("params = %+v", got.Params)
	}
	if got.Headers[0].Value != "Bearer "+Masked || got.Headers[1].Value != "*/*" || got.Headers[2].Value != "{{cookie}}" {
		t.Errorf("headers = %+v", got.Headers)
	}
	if got.Auth.Username != "ada" || got.Auth.Password != Masked {
		t.Errorf("auth = %+v", got.Auth)
	}
	if got.Body.Fields[0].Value != Masked || got.Body.Fields[1].Value != "ada" {
		t.Errorf("fields = %+v", got.Body.Fields)
	}
	// the request that came in is not changed
	if in.Headers[0].Value != "Bearer abc" || in.Auth.Password != "pw" || in.Body.Fields[0].Value != "hunter2" {
		t.Error("MaskRequest changed its argument")
	}
}
