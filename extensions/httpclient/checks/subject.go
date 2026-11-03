package checks

import (
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/jsonpath"
)

// MaxBody is the largest body that is read for a rule or a check (the JSON of a body is decoded in memory); a larger one is a
// problem of the rule, not a failure of the program.
const MaxBody = 16 << 20

// subject is a response being read: its body is read once, and decoded as JSON once, for all the rules.
type subject struct {
	resp *engine.Response

	read bool
	body []byte
	err  error

	decoded bool
	doc     any
	docErr  error
}

func newSubject(resp *engine.Response) *subject { return &subject{resp: resp} }

// text is the body of the response. It fails with ChecksBodyTooLarge, or with the tag of the engine's EngineBodyReadFailed.
func (s *subject) text() ([]byte, error) {
	if s.read {
		return s.body, s.err
	}
	s.read = true
	b := s.resp.Body
	switch {
	case b == nil:
		return nil, nil
	case b.Size() > MaxBody:
		s.err = errors.New("ChecksBodyTooLarge", "the body is too large to be read by a rule or a check", "size", b.Size(), "limit", int64(MaxBody))
	default:
		s.body, s.err = b.Bytes()
	}
	return s.body, s.err
}

// json is the body decoded. It fails with JSONPathBadDocument (the body is not JSON), and with the errors of text.
func (s *subject) json() (any, error) {
	if s.decoded {
		return s.doc, s.docErr
	}
	s.decoded = true
	data, err := s.text()
	if err != nil {
		s.docErr = err
		return nil, err
	}
	doc, derr := jsonpath.Decode(data)
	if derr != nil {
		s.docErr = derr // it has the tag JSONPathBadDocument already
		return nil, derr
	}
	s.doc = doc
	return doc, nil
}

// header is the value of a header of the response (several values are joined by ", "); found is false when it has none.
func (s *subject) header(name string) (string, bool) {
	vals := s.resp.Header.Values(strings.TrimSpace(name))
	if len(vals) == 0 {
		return "", false
	}
	return strings.Join(vals, ", "), true
}

// cookie is the value of the cookie the response set; found is false when it set none of that name.
func (s *subject) cookie(name string) (string, bool) {
	name = strings.TrimSpace(name)
	for _, c := range s.resp.Cookies {
		if c.Name == name {
			return c.Value, true
		}
	}
	return "", false
}
