package httpclient

import (
	"context"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/checks"
	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
)

// The scopes of an extraction rule (SPEC §10.33 "Extraction and checks"). A rule without a scope writes to the environment.
const (
	scopeEnvironment = "environment"
	scopeCollection  = "collection"
	scopeGlobal      = "global"
)

// extractOutcome is what happened to an extraction rule after a response: what it found, and where it put it.
type extractOutcome struct {
	checks.Extracted
	// Scope is where the value went (the default filled in), Secret says that the variable is a secret and its value is in the keyring.
	Scope  string
	Secret bool
	// Stored says that the value was written.
	Stored bool
}

// shown is the value as the Extract tab shows it: a secret is never shown.
func (o extractOutcome) shown() string {
	switch {
	case !o.OK():
		return ""
	case o.Secret:
		return "********"
	}
	return o.Value
}

// extractAndStore takes the values the rules ask for out of the response and writes them into the current values of their scopes
// (Emetos's state, never a file of the project; a global goes to the globals of the user; the value of a secret variable goes to the
// keyring). A rule that finds nothing or cannot store its value leaves the variable as it was, and says why in its outcome. It does
// I/O (the keyring, the globals file) and runs off the UI thread.
func (e *Extension) extractAndStore(ctx context.Context, p place, resp *engine.Response, rules []format.ExtractRule) []extractOutcome {
	if len(rules) == 0 {
		return nil
	}
	found := checks.Extract(resp, rules)
	out := make([]extractOutcome, len(found))
	e.extractMu.Lock()
	defer e.extractMu.Unlock()
	for i, x := range found {
		out[i] = extractOutcome{Extracted: x, Scope: x.Rule.Scope}
		if out[i].Scope == "" {
			out[i].Scope = scopeEnvironment
		}
		if !x.OK() {
			continue
		}
		if err := e.storeExtracted(ctx, p, &out[i]); err != nil {
			e.api.Log().Warn("an extracted value cannot be stored", "err", err, "variable", x.Rule.To)
			out[i].Value, out[i].Problem = "", checks.ProblemText(err)
			continue
		}
		out[i].Stored = true
	}
	return out
}

// storeExtracted writes the value of one outcome into its scope. It fails with HttpExtractNoEnvironment, HttpExtractNoCollection and
// HttpExtractScopeUnknown, and with the errors of variables.Values.Set and of the globals.
func (e *Extension) storeExtracted(ctx context.Context, p place, o *extractOutcome) error {
	name, value := strings.TrimSpace(o.Rule.To), o.Value
	switch o.Scope {
	case scopeEnvironment:
		if p.env == nil {
			return errors.New("HttpExtractNoEnvironment", "no environment is selected: select one, or give the rule the scope collection or global", "variable", name)
		}
		o.Secret = declaredSecret(p.env, name)
		return e.values.Set(ctx, p.env.Owner, name, value, o.Secret)
	case scopeCollection:
		if p.collection == nil {
			return errors.New("HttpExtractNoCollection", "the request is not in a collection: save it in one, or give the rule the scope environment or global", "variable", name)
		}
		o.Secret = declaredSecret(p.collection, name)
		return e.values.Set(ctx, p.collection.Owner, name, value, o.Secret)
	case scopeGlobal:
		secret, err := e.setGlobal(ctx, name, value)
		o.Secret = secret
		return err
	}
	return errors.New("HttpExtractScopeUnknown", "the scope of a rule is environment, collection or global", "scope", o.Scope)
}

// declaredSecret says that the scope declares the variable as a secret.
func declaredSecret(s *variables.Scope, name string) bool {
	return slices.ContainsFunc(s.Variables, func(v format.Variable) bool { return v.Name == name && v.Secret })
}

// setGlobal makes value the value of the global variable (made when it is not there), and says whether the global is a secret. The
// globals are in the state of the user; the value of a secret one goes to the keyring.
func (e *Extension) setGlobal(ctx context.Context, name, value string) (secret bool, err error) {
	path, err := variables.GlobalsPath()
	if err != nil {
		return false, err
	}
	g := variables.NewGlobals(path, e.opts.Keyring)
	entries, _, err := g.Load(ctx)
	if err != nil {
		return false, err
	}
	i := slices.IndexFunc(entries, func(x variables.Entry) bool { return x.Name == name })
	if i < 0 {
		entries = append(entries, variables.Entry{Name: name, Value: value, Enabled: true})
		return false, g.Save(ctx, entries)
	}
	entries[i].Value = value
	return entries[i].Secret, g.Save(ctx, entries)
}

// valuesChanged tells what shows current values that some changed (an extraction rule wrote them): the environment editors without
// edits read them again. It runs on the UI thread.
func (e *Extension) valuesChanged() {
	for _, t := range e.envTabs {
		t.storeChanged()
	}
}
