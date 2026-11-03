package checks

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/jsonpath"
)

// Extracted is what one extraction rule found in a response.
type Extracted struct {
	// Index is the place of the rule in the request's list.
	Index int
	Rule  format.ExtractRule
	// Value is the text taken out of the response; empty when Problem is not.
	Value string
	// Problem says why the rule found nothing (the text of its error); empty when it did.
	Problem string
}

// OK says that the rule found a value.
func (x Extracted) OK() bool { return x.Problem == "" }

// Extract applies the rules to the response, each on its own: a rule that fails does not stop the next. The value of a rule is text:
//   - from "body": the first value the JSONPath in path selects (a string as it is, a number as written, a container as compact
//     JSON); with no path, the body itself;
//   - from "header": the header named in header (the values of a repeated header joined by ", ");
//   - from "status": the status code;
//   - from "cookie": the value of the cookie named in cookie that the response set;
//
// and then, when regex is set, the first group of the first match of the regular expression (Go's syntax) in that text, or the whole
// match when it has no group. The caller stores the values (the variable To, in the scope Scope).
func Extract(resp *engine.Response, rules []format.ExtractRule) []Extracted {
	s := newSubject(resp)
	out := make([]Extracted, len(rules))
	for i, r := range rules {
		v, err := s.extract(r)
		out[i] = Extracted{Index: i, Rule: r, Value: v}
		if err != nil {
			out[i].Value, out[i].Problem = "", ProblemText(err)
		}
	}
	return out
}

// extract applies a rule. The tags of its failures: ChecksTargetMissing, ChecksUnknownSource, ChecksSourceMissing, ChecksNoMatch,
// ChecksHeaderMissing, ChecksCookieMissing, ChecksRegexInvalid and ChecksRegexNoMatch, and the ones of subject.json, jsonpath.Compile
// and subject.text.
func (s *subject) extract(r format.ExtractRule) (string, error) {
	if strings.TrimSpace(r.To) == "" {
		return "", errors.New("ChecksTargetMissing", "the rule has no variable to put the value in")
	}
	var val string
	switch r.From {
	case "body":
		if strings.TrimSpace(r.Path) == "" {
			data, err := s.text()
			if err != nil {
				return "", err
			}
			val = string(data)
			break
		}
		v, found, err := s.jsonValue(r.Path)
		if err != nil {
			return "", err
		}
		if !found {
			return "", errors.New("ChecksNoMatch", "the path matches nothing in the body", "path", r.Path)
		}
		val = text(v)
	case "header":
		if strings.TrimSpace(r.Header) == "" {
			return "", errors.New("ChecksSourceMissing", "the rule has no header name", "from", r.From)
		}
		h, found := s.header(r.Header)
		if !found {
			return "", errors.New("ChecksHeaderMissing", "the response has no such header", "header", r.Header)
		}
		val = h
	case "status":
		val = strconv.Itoa(s.resp.StatusCode)
	case "cookie":
		if strings.TrimSpace(r.Cookie) == "" {
			return "", errors.New("ChecksSourceMissing", "the rule has no cookie name", "from", r.From)
		}
		c, found := s.cookie(r.Cookie)
		if !found {
			return "", errors.New("ChecksCookieMissing", "the response set no such cookie", "cookie", r.Cookie)
		}
		val = c
	default:
		return "", errors.New("ChecksUnknownSource", "the rule takes its value from an unknown place", "from", r.From)
	}
	if r.Regex == "" {
		return val, nil
	}
	re, err := regexp.Compile(r.Regex)
	if err != nil {
		return "", errors.NewErr("ChecksRegexInvalid", err).SetMetadata("regex", r.Regex)
	}
	m := re.FindStringSubmatch(val)
	switch {
	case m == nil:
		return "", errors.New("ChecksRegexNoMatch", "the regular expression matches nothing in the value", "regex", r.Regex)
	case len(m) > 1:
		return m[1], nil
	}
	return m[0], nil
}

// jsonValue is the first value a JSONPath selects in the body; found is false when it selects none. The path "" is "$".
func (s *subject) jsonValue(path string) (v any, found bool, err error) {
	if strings.TrimSpace(path) == "" {
		path = "$"
	}
	p, err := jsonpath.Compile(path)
	if err != nil {
		return nil, false, err
	}
	doc, err := s.json()
	if err != nil {
		return nil, false, err
	}
	v, found = p.First(doc)
	return v, found, nil
}
