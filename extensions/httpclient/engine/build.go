package engine

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// Input is a request to send: the model, and what it needs from outside.
type Input struct {
	// Request is the request as the file has it, with its {{variables}}.
	Request format.Request
	// Headers are the headers the collection and its folders pass down, outermost first; a header of the request with the same
	// name replaces them.
	Headers []format.Pair
	// Auth is the authorization that applies (the caller resolved "inherit" along the folders); nil: the request's own.
	Auth *format.Auth
	// Resolver replaces the variables; nil: the text is sent as written.
	Resolver *variables.Resolver
	// Folder is the workspace folder, which every file stays inside; Dir is the folder of the request file, where the body
	// files are (empty: Folder).
	Folder string
	Dir    string
	// Texts are the texts of body files by name, used in place of reading the files: the request tab sends what is in its boxes, which
	// may not be saved. A name that is not in Texts is read from Dir as before.
	Texts map[string]string
	// Environment names the cookie jar (empty: the jar of requests without an environment).
	Environment string
	// OAuth is the access token an authorization of type oauth2 sends; the caller got it (and refreshed it) before sending. Without it
	// such a request fails with EngineOAuthTokenMissing.
	OAuth *OAuthToken
}

// Prepared is a request that is ready to send.
type Prepared struct {
	// Method and URL are what will be sent (the URL has the parameters and an API key in the query).
	Method string
	URL    *url.URL
	// Header holds the headers before the engine adds its own (User-Agent, Accept-Encoding, Content-Length).
	Header http.Header
	// Host overrides the Host header when the request set one.
	Host string
	// Unresolved and MissingSecrets are the variables that had no value.
	Unresolved     []string
	MissingSecrets []string

	body        *bodySource
	digest      *digestCreds
	env         string
	timeout     int // milliseconds; 0 uses the engine's
	follow      *bool
	maxRedirect *int
	verify      *bool
	authHeaders []string // the headers the authorization set, dropped when a redirect leaves the host
}

type digestCreds struct{ user, pass string }

// Prepare builds the request: variables, URL with its parameters, headers, authorization and body.
func (e *Engine) Prepare(in Input) (*Prepared, error) {
	p := &Prepared{Header: http.Header{}, env: in.Environment}
	resolve := func(s string) string {
		if in.Resolver == nil {
			return s
		}
		res := in.Resolver.Resolve(s)
		for _, n := range res.Unresolved {
			addOnce(&p.Unresolved, n)
		}
		for _, n := range res.MissingSecrets {
			addOnce(&p.MissingSecrets, n)
		}
		return res.Text
	}
	req := in.Request

	p.Method = strings.ToUpper(strings.TrimSpace(resolve(req.Method)))
	if p.Method == "" {
		p.Method = http.MethodGet
	}
	if !isToken(p.Method) {
		return nil, errors.New("EngineMethodInvalid", "the method is not a valid HTTP method", "method", p.Method)
	}

	raw := strings.TrimSpace(resolve(req.URL))
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.NewErr("EngineURLInvalid", err).SetMetadata("url", raw)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("EngineURLInvalid", "the URL needs the scheme http or https and a host", "url", raw)
	}
	p.URL = u
	var extra []string
	for _, pr := range req.Params {
		if !pr.Enabled || pr.Name == "" {
			continue
		}
		extra = append(extra, url.QueryEscape(resolve(pr.Name))+"="+url.QueryEscape(resolve(pr.Value)))
	}
	addQuery(u, extra)

	// headers: inherited first, replaced by the request's, which may repeat a name
	own := map[string]bool{}
	for _, h := range req.Headers {
		if h.Enabled && h.Name != "" {
			own[http.CanonicalHeaderKey(strings.TrimSpace(resolve(h.Name)))] = true
		}
	}
	setHeader := func(h format.Pair, inherited bool) error {
		if !h.Enabled || h.Name == "" {
			return nil
		}
		name := strings.TrimSpace(resolve(h.Name))
		if !isToken(name) {
			return errors.New("EngineHeaderInvalid", "the header name is not valid", "name", name)
		}
		value := resolve(h.Value)
		if strings.ContainsAny(value, "\r\n") {
			return errors.New("EngineHeaderInvalid", "the header value has a line break", "name", name)
		}
		canon := http.CanonicalHeaderKey(name)
		if inherited && own[canon] {
			return nil
		}
		if canon == "Host" {
			p.Host = value
			return nil
		}
		p.Header.Add(canon, value)
		return nil
	}
	for _, h := range in.Headers {
		if err := setHeader(h, true); err != nil {
			return nil, err
		}
	}
	for _, h := range req.Headers {
		if err := setHeader(h, false); err != nil {
			return nil, err
		}
	}

	auth := in.Auth
	if auth == nil {
		auth = req.Auth
	}
	if err := p.applyAuth(auth, resolve, in.OAuth); err != nil {
		return nil, err
	}

	if s := req.Settings; s != nil {
		if s.TimeoutMs != nil {
			p.timeout = *s.TimeoutMs
			if p.timeout == 0 {
				p.timeout = -1 // 0 in the file is "no timeout"
			}
		}
		p.follow, p.maxRedirect, p.verify = s.FollowRedirects, s.MaxRedirects, s.VerifyTLS
	}

	body, err := buildBody(in, resolve, p.Header)
	if err != nil {
		return nil, err
	}
	p.body = body
	return p, nil
}

// applyAuth puts the authorization in the headers or the query, or arranges the Digest answer.
func (p *Prepared) applyAuth(a *format.Auth, resolve func(string) string, tok *OAuthToken) error {
	if a == nil {
		return nil
	}
	switch a.Type {
	case "", format.AuthNone, format.AuthInherit:
	case format.AuthBasic:
		cred := resolve(a.Username) + ":" + resolve(a.Password)
		p.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cred)))
		p.authHeaders = append(p.authHeaders, "Authorization")
	case format.AuthBearer:
		p.Header.Set("Authorization", "Bearer "+resolve(a.Token))
		p.authHeaders = append(p.authHeaders, "Authorization")
	case format.AuthAPIKey:
		key := resolve(a.Key)
		if key == "" {
			return errors.New("EngineAuthInvalid", "the API key has no name")
		}
		switch a.In {
		case "", "header":
			if !isToken(key) {
				return errors.New("EngineHeaderInvalid", "the header name of the API key is not valid", "name", key)
			}
			p.Header.Set(key, resolve(a.Value))
			p.authHeaders = append(p.authHeaders, http.CanonicalHeaderKey(key))
		case "query":
			addQuery(p.URL, []string{url.QueryEscape(key) + "=" + url.QueryEscape(resolve(a.Value))})
		default:
			return errors.New("EngineAuthInvalid", "the API key goes in \"header\" or \"query\"", "in", a.In)
		}
	case format.AuthDigest:
		p.digest = &digestCreds{user: resolve(a.Username), pass: resolve(a.Password)}
	case format.AuthOAuth2:
		return p.applyOAuth(a.AddTo, tok)
	default:
		return errors.New("EngineAuthUnsupported", "the authorization type is not known", "type", a.Type)
	}
	return nil
}

// addQuery appends encoded name=value pairs to the query of u, in order.
func addQuery(u *url.URL, pairs []string) {
	if len(pairs) == 0 {
		return
	}
	if u.RawQuery != "" {
		u.RawQuery += "&"
	}
	u.RawQuery += strings.Join(pairs, "&")
}

func addOnce(list *[]string, s string) {
	for _, x := range *list {
		if x == s {
			return
		}
	}
	*list = append(*list, s)
}

// isToken reports whether s is an RFC 9110 token (a method or a header name).
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
		if !ok {
			return false
		}
	}
	return true
}
