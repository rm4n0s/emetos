package checks

import (
	"regexp"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// Types are the names isType understands.
var Types = []string{"string", "number", "boolean", "null", "object", "array"}

// Result is how one check came out.
type Result struct {
	// Index is the place of the check in the request's list.
	Index int
	Check format.Check
	// Passed says that the response satisfies the check.
	Passed bool
	// What is the check in words ("json $.id"), Expected what it asks for ("equals 17") and Actual what the response has.
	What, Expected, Actual string
	// Problem says why the check could not be made (a path that matches nothing, a bad regular expression); it is set only
	// for a check that failed.
	Problem string
}

// Run applies the checks to the response, each on its own.
//
//	subject   what is looked at
//	status    the status code, a number
//	time      the whole time of the exchange in milliseconds, a number
//	header    the header named in header (the values of a repeated header joined by ", "), text
//	body      the body, text
//	json      the first value the JSONPath in path selects in the body ("$" when empty); any JSON value
//
//	op          passes when
//	equals      the value is the one in value (numbers by value: the text "200" equals 200; objects and arrays by content)
//	notEquals   it does not
//	contains    the text holds the text of value, the array has the element value, or the object has the key value
//	matches     the text of the value has a match of the regular expression in value (Go's syntax, not anchored)
//	lessThan    the value is a number below value (greaterThan: above)
//	exists      the header, the path or the cookie is there, the body is not empty (status and time always are)
//	notExists   it is not
//	isType      the value is of the type named in value: string, number, boolean, null, object or array
//
// A check whose value cannot be had (a missing header, a path that matches nothing, a body that is not JSON) fails with a Problem,
// except for exists and notExists, for which that is the answer.
func Run(resp *engine.Response, checks []format.Check) []Result {
	s := newSubject(resp)
	out := make([]Result, len(checks))
	for i, c := range checks {
		r := Result{Index: i, Check: c, What: What(c), Expected: Expected(c)}
		got, found, err := s.value(c)
		switch {
		case err != nil && c.Op != "exists" && c.Op != "notExists":
			r.Problem = ProblemText(err)
			r.Actual = ""
		default:
			if err != nil { // exists and notExists: a body that is not JSON has no such path
				found = false
			}
			if found {
				r.Actual = clip(text(got))
			} else {
				r.Actual = "(missing)"
			}
			pass, perr := compare(c, got, found)
			if perr != nil {
				r.Problem = ProblemText(perr)
			}
			r.Passed = pass && perr == nil
		}
		out[i] = r
	}
	return out
}

// What names what a check looks at: "status", "time", "header Content-Type", "body", "json $.id".
func What(c format.Check) string {
	switch c.Subject {
	case "header":
		return "header " + c.Header
	case "json":
		p := strings.TrimSpace(c.Path)
		if p == "" {
			p = "$"
		}
		return "json " + p
	}
	return c.Subject
}

// Expected says what a check asks for in words: "equals 17", "exists".
func Expected(c format.Check) string {
	switch c.Op {
	case "exists", "notExists":
		return opWords(c.Op)
	}
	v := text(c.Value)
	if _, isString := c.Value.(string); isString {
		v = `"` + v + `"`
	}
	return opWords(c.Op) + " " + v
}

func opWords(op string) string {
	switch op {
	case "notEquals":
		return "does not equal"
	case "lessThan":
		return "is less than"
	case "greaterThan":
		return "is greater than"
	case "notExists":
		return "does not exist"
	case "isType":
		return "is of type"
	}
	return op
}

// value is what the check looks at. found is false for a header, path or cookie that is not there; err is the reason when the
// value cannot be had at all. Its tags: ChecksUnknownSubject, ChecksSourceMissing, ChecksHeaderMissing, ChecksNoMatch and the ones of
// subject.jsonValue and subject.text.
func (s *subject) value(c format.Check) (v any, found bool, err error) {
	switch c.Subject {
	case "status":
		return s.resp.StatusCode, true, nil
	case "time":
		return float64(s.resp.Timings.Total.Microseconds()) / 1000, true, nil
	case "header":
		if strings.TrimSpace(c.Header) == "" {
			return nil, false, errors.New("ChecksSourceMissing", "the check has no header name", "subject", c.Subject)
		}
		h, ok := s.header(c.Header)
		if !ok {
			return nil, false, errors.New("ChecksHeaderMissing", "the response has no such header", "header", c.Header)
		}
		return h, true, nil
	case "body":
		data, err := s.text()
		if err != nil {
			return nil, false, err
		}
		return string(data), true, nil
	case "json":
		v, ok, err := s.jsonValue(c.Path)
		if err != nil {
			return nil, false, err
		}
		if !ok {
			return nil, false, errors.New("ChecksNoMatch", "the path matches nothing in the body", "path", c.Path)
		}
		return v, true, nil
	}
	return nil, false, errors.New("ChecksUnknownSubject", "the check looks at an unknown place", "subject", c.Subject)
}

// compare applies the operator. Its tags: ChecksUnknownOp, ChecksNotANumber, ChecksRegexInvalid and
// ChecksUnknownType.
func compare(c format.Check, got any, found bool) (bool, error) {
	switch c.Op {
	case "exists":
		return found && (c.Subject != "body" || got != ""), nil
	case "notExists":
		return !found || c.Subject == "body" && got == "", nil
	}
	if !found { // value fails with an error for what is not there, so this does not happen; nothing to compare is no pass
		return false, nil
	}
	switch c.Op {
	case "equals":
		return equal(got, c.Value), nil
	case "notEquals":
		return !equal(got, c.Value), nil
	case "contains":
		switch x := got.(type) {
		case []any:
			return slices.ContainsFunc(x, func(e any) bool { return equal(e, c.Value) }), nil
		case map[string]any:
			_, has := x[text(c.Value)]
			return has, nil
		}
		return strings.Contains(text(got), text(c.Value)), nil
	case "matches":
		re, err := regexp.Compile(text(c.Value))
		if err != nil {
			return false, errors.NewErr("ChecksRegexInvalid", err).SetMetadata("regex", text(c.Value))
		}
		return re.MatchString(text(got)), nil
	case "lessThan", "greaterThan":
		a, ok := number(got)
		if !ok {
			return false, errors.New("ChecksNotANumber", "the value is not a number", "value", clip(text(got)))
		}
		b, ok := number(c.Value)
		if !ok {
			return false, errors.New("ChecksNotANumber", "the value of the check is not a number", "value", clip(text(c.Value)))
		}
		if c.Op == "lessThan" {
			return a < b, nil
		}
		return a > b, nil
	case "isType":
		want, _ := c.Value.(string)
		if !slices.Contains(Types, want) {
			return false, errors.New("ChecksUnknownType", "a type is one of string, number, boolean, null, object and array", "type", text(c.Value))
		}
		return kind(got) == want, nil
	}
	return false, errors.New("ChecksUnknownOp", "the check has an unknown operator", "op", c.Op)
}
