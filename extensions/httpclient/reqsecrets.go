package httpclient

import (
	"strings"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/history"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
)

// stateSafe returns a copy of the draft for the state of the window with the typed secrets taken out, by the same rules as the
// history masks a request (history.MaskRequest and history.MaskBody): the password, token, key value and client secret of the Auth
// tab, the values of the headers, parameters and form fields whose names look secret, the same keys of a JSON body and the password
// and secret query values of the URL. A value made only of {{variables}} holds no secret and stays. A secret is left empty (not
// "********": a placeholder could be saved or sent by mistake). The second result names what was left out, for the note that the
// restored tab shows.
func stateSafe(f store.RequestFiles) (store.RequestFiles, []string) {
	var gone []string
	empty := func(what, was, masked string) string {
		if masked == was {
			return was
		}
		gone = append(gone, what)
		return strings.TrimSpace(strings.ReplaceAll(masked, history.Masked, ""))
	}
	pairsOf := func(kind string, in []format.Pair) []format.Pair {
		masked := history.MaskPairs(in)
		for i := range masked {
			masked[i].Value = empty(kind+" "+masked[i].Name, in[i].Value, masked[i].Value)
		}
		return masked
	}
	req := f.Request
	if u := history.MaskURL(req.URL); u != req.URL {
		gone = append(gone, "the URL's secret")
		req.URL = strings.ReplaceAll(u, history.Masked, "")
	}
	req.Params, req.Headers = pairsOf("parameter", req.Params), pairsOf("header", req.Headers)
	if a := req.Auth; a != nil {
		c := *a
		c.Password = empty("the password", a.Password, history.MaskValue(a.Password))
		c.Token = empty("the token", a.Token, history.MaskValue(a.Token))
		c.Value = empty("the API key", a.Value, history.MaskValue(a.Value))
		c.ClientSecret = empty("the client secret", a.ClientSecret, history.MaskValue(a.ClientSecret))
		req.Auth = &c
	}
	if b := req.Body; b != nil {
		c := *b
		c.Fields = pairsOf("field", b.Fields)
		req.Body = &c
		if text := history.MaskBody(b.Type, f.Body); text != f.Body {
			gone = append(gone, "a secret in the body")
			f.Body = strings.ReplaceAll(text, history.Masked, "")
		}
	}
	f.Request = req
	return f, gone
}

// retypeNote is the note a restored tab shows for the secrets that were not kept in the state.
func retypeNote(gone []string) string {
	if len(gone) == 0 {
		return ""
	}
	return "Typed secrets are not kept when the window closes; type them again: " + strings.Join(gone, ", ") + "."
}
