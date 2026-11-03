package format

import (
	"slices"
	"strconv"
)

// problem is a value of a document that the format does not allow: the key of the top level it is under and what is wrong.
type problem struct {
	key string
	msg string
}

var (
	authTypes     = []string{AuthInherit, AuthNone, AuthBasic, AuthBearer, AuthAPIKey, AuthDigest, AuthOAuth2}
	bodyTypes     = []string{BodyNone, BodyJSON, BodyXML, BodyText, BodyHTML, BodyJavaScript, BodyGraphQL, BodyForm, BodyMultipart, BodyBinary}
	extractFrom   = []string{"body", "header", "status", "cookie"}
	extractScopes = []string{"", "environment", "collection", "global"}
	checkSubjects = []string{"status", "time", "header", "body", "json"}
	checkOps      = []string{"equals", "notEquals", "contains", "matches", "lessThan", "greaterThan", "exists", "notExists", "isType"}
)

// validate looks for the first value of the document that the format does not allow.
func validate(doc any) *problem {
	switch d := doc.(type) {
	case Collection:
		return firstProblem(
			checkOrder(d.Order), checkVariables("variables", d.Variables), checkAuth(d.Auth), checkPairs("headers", d.Headers))
	case Environment:
		return checkVariables("variables", d.Variables)
	case Request:
		return firstProblem(
			checkPairs("params", d.Params), checkPairs("headers", d.Headers), checkAuth(d.Auth), checkBody(d.Body),
			checkSettings(d.Settings), checkExtract(d.Extract), checkChecks(d.Checks))
	}
	return nil
}

func firstProblem(ps ...*problem) *problem {
	for _, p := range ps {
		if p != nil {
			return p
		}
	}
	return nil
}

func checkOrder(order []string) *problem {
	for i, name := range order {
		if !ValidSiblingName(name) {
			return &problem{"order", "entry " + strconv.Itoa(i+1) + " is not a file or folder name"}
		}
	}
	return nil
}

func checkVariables(key string, vars []Variable) *problem {
	for i, v := range vars {
		if v.Name == "" {
			return &problem{key, "variable " + strconv.Itoa(i+1) + " has no name"}
		}
	}
	return nil
}

func checkPairs(key string, pairs []Pair) *problem {
	for i, p := range pairs {
		if p.File != "" && p.Value != "" {
			return &problem{key, "entry " + strconv.Itoa(i+1) + " has both a value and a file"}
		}
	}
	return nil
}

func checkAuth(a *Auth) *problem {
	if a == nil {
		return nil
	}
	if !slices.Contains(authTypes, a.Type) {
		return &problem{"auth", "the type " + strconv.Quote(a.Type) + " is not one of " + join(authTypes)}
	}
	if a.Type == AuthAPIKey && a.In != "" && a.In != "header" && a.In != "query" {
		return &problem{"auth", `"in" must be "header" or "query"`}
	}
	return nil
}

func checkBody(b *Body) *problem {
	if b == nil {
		return nil
	}
	if !slices.Contains(bodyTypes, b.Type) {
		return &problem{"body", "the type " + strconv.Quote(b.Type) + " is not one of " + join(bodyTypes)}
	}
	if b.File != "" && !ValidSiblingName(b.File) {
		return &problem{"body", `"file" must be the name of a file next to the request`}
	}
	if b.Variables != "" && !ValidSiblingName(b.Variables) {
		return &problem{"body", `"variables" must be the name of a file next to the request`}
	}
	return checkPairs("body", b.Fields)
}

func checkSettings(s *Settings) *problem {
	if s == nil {
		return nil
	}
	if s.TimeoutMs != nil && *s.TimeoutMs < 0 {
		return &problem{"settings", `"timeoutMs" must not be negative`}
	}
	if s.MaxRedirects != nil && *s.MaxRedirects < 0 {
		return &problem{"settings", `"maxRedirects" must not be negative`}
	}
	return nil
}

func checkExtract(rules []ExtractRule) *problem {
	for i, r := range rules {
		switch {
		case !slices.Contains(extractFrom, r.From):
			return &problem{"extract", "rule " + strconv.Itoa(i+1) + `: "from" must be one of ` + join(extractFrom)}
		case r.To == "":
			return &problem{"extract", "rule " + strconv.Itoa(i+1) + `: "to" is the name of the variable and is missing`}
		case !slices.Contains(extractScopes, r.Scope):
			return &problem{"extract", "rule " + strconv.Itoa(i+1) + `: "scope" must be one of ` + join(extractScopes[1:])}
		}
	}
	return nil
}

func checkChecks(checks []Check) *problem {
	for i, c := range checks {
		switch {
		case !slices.Contains(checkSubjects, c.Subject):
			return &problem{"checks", "check " + strconv.Itoa(i+1) + `: "subject" must be one of ` + join(checkSubjects)}
		case !slices.Contains(checkOps, c.Op):
			return &problem{"checks", "check " + strconv.Itoa(i+1) + `: "op" must be one of ` + join(checkOps)}
		}
	}
	return nil
}

func join(list []string) string {
	out := ""
	for i, s := range list {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
