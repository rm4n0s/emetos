package httpclient

import (
	"context"
	"strconv"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/oauth"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// oauthState is the OAuth 2.0 part of the Auth tab: the token kept for the request's authorization and what the buttons are doing. The access
// token is held here only to show it (masked); it is read from the keyring, and the state of the window never has it.
type oauthState struct {
	// loaded says the keyring was asked; loading that it is being asked.
	loaded, loading bool
	tok             oauth.Token
	found           bool
	// box shows the access token, masked until "Show passwords and tokens".
	box *ui.InputBoxController
	// busy says "Get New Access Token" waits (for the browser or the server); cancel ends the wait.
	busy   bool
	cancel context.CancelFunc
	// msg is the line under the buttons; bad says it is a failure.
	msg string
	bad bool
}

// oauthAuth is the OAuth 2.0 authorization that applies to the tab's request, or nil.
func (t *reqTab) oauthAuth() *format.Auth {
	if t.d == nil {
		return nil
	}
	return effectiveOAuth(t.d.authValue(), t.e.placeOf(t.path))
}

// wantToken has the stored token read once, after the panel is first built.
func (t *reqTab) wantToken() {
	o := &t.oauth
	if o.loaded || o.loading {
		return
	}
	o.loading = true
	t.e.api.Post(t.loadToken)
}

// loadToken reads the stored token for the request's authorization off the UI thread.
func (t *reqTab) loadToken() {
	auth := t.oauthAuth()
	o := &t.oauth
	if auth == nil {
		o.loading = false
		return
	}
	e, place := t.e, t.e.placeOf(t.path)
	var tok oauth.Token
	var found bool
	var lerr error
	e.async(func(ctx context.Context) error {
		cfg := oauthConfigOf(auth, e.resolverFor(ctx, place, nil))
		tok, found, lerr = e.oauthManager().Stored(ctx, oauthOwner(place), cfg)
		return nil
	}, func(error) {
		o.loading, o.loaded = false, true
		o.tok, o.found = tok, found
		o.setBox(tok.AccessToken)
		if lerr != nil {
			o.msg, o.bad = "The token cannot be read from the keyring: "+lerr.Error(), true
		}
		t.tab.Refresh()
	})
}

func (o *oauthState) setBox(text string) {
	if o.box == nil {
		o.box = ui.NewInputBoxController(text)
		return
	}
	o.box.SetValue(text)
}

// getNewToken is "Get New Access Token": the client credentials grant asks the server; the authorization code grant opens the browser
// and waits for the redirect. The token replaces the one that was kept.
func (t *reqTab) getNewToken() {
	auth := t.oauthAuth()
	o := &t.oauth
	if auth == nil {
		t.e.api.Notifications().Info("This request does not use OAuth 2.0.")
		return
	}
	if o.busy || t.d == nil {
		return
	}
	e, place := t.e, t.e.placeOf(t.path)
	ctx, cancel := context.WithCancel(e.ctx)
	o.busy, o.cancel, o.msg, o.bad = true, cancel, "", false
	if auth.Grant == oauth.GrantAuthorizationCode {
		o.msg = "Waiting for you in the browser..."
	} else {
		o.msg = "Asking the server..."
	}
	t.tab.Refresh()
	var tok oauth.Token
	var gerr error
	e.async(func(context.Context) error {
		cfg := oauthConfigOf(auth, e.resolverFor(ctx, place, nil))
		tok, gerr = e.oauthManager().GetNew(ctx, oauthOwner(place), cfg)
		return nil
	}, func(error) {
		cancel()
		o.busy, o.cancel = false, nil
		if gerr == nil {
			o.tok, o.found, o.loaded = tok, true, true
			o.setBox(tok.AccessToken)
			o.msg, o.bad = "Got a new access token.", false
			e.api.Notifications().Info("Got a new access token.")
		} else {
			o.msg, o.bad = gerr.Error(), true
			if appErr, ok := errors.FromError(gerr); ok && appErr.Tag == "OAuthCanceled" {
				o.msg, o.bad = "Canceled.", false
			}
		}
		t.tab.Refresh()
	})
}

// cancelToken ends the wait of "Get New Access Token".
func (t *reqTab) cancelToken() {
	if t.oauth.busy && t.oauth.cancel != nil {
		t.oauth.cancel()
	}
}

// clearToken is "Clear Token": the kept token is removed from the keyring.
func (t *reqTab) clearToken() {
	auth := t.oauthAuth()
	o := &t.oauth
	if auth == nil || o.busy {
		return
	}
	e, place := t.e, t.e.placeOf(t.path)
	var cerr error
	e.async(func(ctx context.Context) error {
		cfg := oauthConfigOf(auth, e.resolverFor(ctx, place, nil))
		cerr = e.oauthManager().Clear(ctx, oauthOwner(place), cfg)
		return nil
	}, func(error) {
		if cerr != nil {
			o.msg, o.bad = "The token cannot be removed from the keyring: "+cerr.Error(), true
		} else {
			o.tok, o.found = oauth.Token{}, false
			o.setBox("")
			o.msg, o.bad = "The token was removed.", false
		}
		t.tab.Refresh()
	})
}

// tokenInfo says in a line what is known of a token: its scheme, when it ends and whether it can be refreshed.
func tokenInfo(tok oauth.Token, now time.Time) string {
	s := tok.Scheme()
	switch {
	case tok.ExpiresAt.IsZero():
		s += " · no expiry given"
	case !now.Before(tok.ExpiresAt):
		s += " · expired " + agoText(now.Sub(tok.ExpiresAt)) + " ago"
	default:
		s += " · expires in " + agoText(tok.ExpiresAt.Sub(now))
	}
	if tok.RefreshToken != "" {
		s += " · can be refreshed"
	}
	return s
}

// agoText is a length of time in the largest unit that fits ("45 s", "12 min", "3 h", "2 days").
func agoText(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d/time.Second)) + " s"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + " days"
}

// tokenSection is the part of the Auth tab that shows the token and its buttons. field lays out a labeled row.
func (t *reqTab) tokenSection(bc widgets.BuildContext, colors ui.Colors, dim *painting.TextStyle, locked bool, field func(string, widgets.Widget)) []widgets.Widget {
	o := &t.oauth
	t.wantToken()
	if o.box == nil {
		o.box = ui.NewInputBoxController("")
	}
	var rows []widgets.Widget
	hint := "No token yet: use Get New Access Token."
	if o.found {
		hint = ""
	}
	field("Access token", ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Key: foundation.NewObjectKey(o.box), Controller: o.box, Placeholder: hint, Disabled: true, Obscure: !t.showSecrets,
	}))
	if o.found {
		rows = append(rows, center(wide(pageui.Nothing(), 150), flex(pageui.Paragraph(tokenInfo(o.tok, t.e.opts.Now()), dim), 1)), pageui.Gap(8))
	}
	get := ui.NewButton("Get New Access Token", ui.ButtonOpts{OnPressed: t.getNewToken, Disabled: locked, Tooltip: "Get a token from the authorization server"})
	if o.busy {
		get = ui.NewButton("Cancel", ui.ButtonOpts{OnPressed: t.cancelToken, Secondary: true, Tooltip: "Stop waiting for the authorization"})
	}
	clr := ui.NewButton("Clear Token", ui.ButtonOpts{OnPressed: t.clearToken, Secondary: true, Disabled: o.busy || !o.found, Tooltip: "Remove the token from the keyring"})
	rows = append(rows, center(wide(pageui.Nothing(), 150), get, pageui.HGap(8), clr), pageui.Gap(8))
	if o.msg != "" {
		rows = append(rows, center(wide(pageui.Nothing(), 150), flex(pageui.NoteLine(bc, colors, pageui.Note{Text: o.msg, Bad: o.bad}), 1)), pageui.Gap(8))
	}
	return rows
}
