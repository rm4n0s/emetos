package history

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// Masked is what takes the place of a secret value in a copy of a request.
const Masked = "********"

// sensitiveWords are the parts of a name (of a header, a parameter, a form field or a key in a JSON body) whose value is a secret.
var sensitiveWords = []string{
	"authorization", "cookie", "token", "secret", "password", "passwd", "pwd", "api-key", "api_key", "apikey",
	"credential", "session", "signature", "private-key", "private_key",
}

// Sensitive says whether the value of something with this name (a header, a parameter, a field) is a secret.
func Sensitive(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	for _, w := range sensitiveWords {
		if strings.Contains(n, w) {
			return true
		}
	}
	return false
}

var (
	// reference is a {{variable}} of a request.
	reference = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	// schemes are the words that begin the value of an Authorization header.
	schemes = []string{"bearer", "basic", "digest", "token", "negotiate", "hawk", "aws4-hmac-sha256"}
)

// MaskValue masks a secret value: a value made only of {{variables}} (and spaces) is kept, as it holds no secret, and so is the
// scheme word of an Authorization value ("Bearer {{token}}", "Basic ********"). An empty value stays empty.
func MaskValue(v string) string {
	rest := strings.TrimSpace(reference.ReplaceAllString(v, ""))
	if rest == "" {
		return v
	}
	trim := strings.TrimSpace(v)
	if sp := strings.IndexByte(trim, ' '); sp > 0 {
		word := strings.ToLower(trim[:sp])
		for _, s := range schemes {
			if word == s {
				tail := strings.TrimSpace(trim[sp+1:])
				if strings.TrimSpace(reference.ReplaceAllString(tail, "")) == "" {
					return v
				}
				return trim[:sp] + " " + Masked
			}
		}
	}
	return Masked
}

// MaskPairs returns a copy of the pairs with the values of the sensitive names masked.
func MaskPairs(pairs []format.Pair) []format.Pair {
	if pairs == nil {
		return nil
	}
	out := make([]format.Pair, len(pairs))
	for i, p := range pairs {
		if Sensitive(p.Name) && p.File == "" {
			p.Value = MaskValue(p.Value)
		}
		out[i] = p
	}
	return out
}

// MaskURL masks the password of the user information of a URL and the values of the query parameters with sensitive names. Text that
// is not a URL (it has {{variables}} in the place of the host, say) is looked at as far as it can be: the query is masked by name.
func MaskURL(raw string) string {
	base, query, hasQuery := strings.Cut(raw, "?")
	if hasQuery {
		query, frag, hasFrag := strings.Cut(query, "#")
		parts := strings.Split(query, "&")
		for i, part := range parts {
			name, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			decoded, err := url.QueryUnescape(name)
			if err != nil {
				decoded = name
			}
			if Sensitive(decoded) {
				parts[i] = name + "=" + maskQueryValue(value)
			}
		}
		raw = base + "?" + strings.Join(parts, "&")
		if hasFrag {
			raw += "#" + frag
		}
	}
	return maskUserinfo(raw)
}

// maskQueryValue masks a value of a query without escaping the mask.
func maskQueryValue(v string) string {
	unescaped, err := url.QueryUnescape(v)
	if err != nil {
		unescaped = v
	}
	if m := MaskValue(unescaped); m != unescaped {
		return m
	}
	return v
}

// maskUserinfo masks the password in scheme://user:password@host.
func maskUserinfo(raw string) string {
	i := strings.Index(raw, "://")
	if i < 0 {
		return raw
	}
	rest := raw[i+3:]
	end := strings.IndexAny(rest, "/?#")
	if end < 0 {
		end = len(rest)
	}
	authority := rest[:end]
	at := strings.LastIndexByte(authority, '@')
	if at < 0 {
		return raw
	}
	user, pass, ok := strings.Cut(authority[:at], ":")
	if !ok {
		return raw
	}
	return raw[:i+3] + user + ":" + MaskValue(pass) + authority[at:] + rest[end:]
}

// jsonSecret finds "name": "value" in the text of a JSON body when the name is sensitive; the value is the last group.
var jsonSecret = regexp.MustCompile(`("(?:[^"\\]|\\.)*"\s*:\s*)"((?:[^"\\]|\\.)*)"`)

// MaskBody masks the values of the keys with sensitive names in the text of a JSON body, and the sensitive fields of a form-encoded
// text; any other text is returned as it is (a body of free text cannot be searched for secrets).
func MaskBody(kind, text string) string {
	switch kind {
	case format.BodyJSON, format.BodyGraphQL:
		return jsonSecret.ReplaceAllStringFunc(text, func(m string) string {
			sub := jsonSecret.FindStringSubmatch(m)
			key := strings.TrimSpace(strings.SplitN(sub[1], ":", 2)[0])
			key = strings.Trim(key, `"`)
			if !Sensitive(key) {
				return m
			}
			masked := MaskValue(sub[2])
			return sub[1] + `"` + masked + `"`
		})
	}
	return text
}

// MaskAuth returns a copy of the authorization with the passwords, tokens and secrets masked.
func MaskAuth(a *format.Auth) *format.Auth {
	if a == nil {
		return nil
	}
	c := *a
	c.Password = MaskValue(c.Password)
	c.Token = MaskValue(c.Token)
	c.Value = MaskValue(c.Value)
	c.ClientSecret = MaskValue(c.ClientSecret)
	return &c
}

// MaskRequest returns a copy of the request that has no secret in it: the authorization fields, the sensitive headers, parameters and
// fields, the query of the URL and its password. {{variables}} are kept, so the copy can be sent again.
func MaskRequest(r format.Request) format.Request {
	r.URL = MaskURL(r.URL)
	r.Params = MaskPairs(r.Params)
	r.Headers = MaskPairs(r.Headers)
	r.Auth = MaskAuth(r.Auth)
	if r.Body != nil {
		b := *r.Body
		b.Fields = MaskPairs(b.Fields)
		r.Body = &b
	}
	return r
}
