package variables

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// Source says where a variable's value came from.
type Source string

// The places a value comes from, in the order they win.
const (
	SourceLocal              Source = "local"
	SourceEnvironmentCurrent Source = "environment current value"
	SourceEnvironmentInitial Source = "environment"
	SourceFolder             Source = "folder"
	SourceCollectionCurrent  Source = "collection current value"
	SourceCollectionInitial  Source = "collection"
	SourceGlobal             Source = "global"
)

const (
	maxDepth            = 10
	openMark, closeMark = "{{", "}}"
)

// Scope is a place that defines variables: an environment, a collection or a folder.
type Scope struct {
	// Owner is the owner of its current values and secrets (EnvironmentOwner, CollectionOwner); empty for a folder, whose
	// variables have no current value.
	Owner string
	// Name is shown with the source ("Dev", "Users").
	Name      string
	Variables []format.Variable
}

// Input is what a request resolves against.
type Input struct {
	// Locals are the values extracted by the request in this run.
	Locals map[string]string
	// Environment is the active environment, or nil.
	Environment *Scope
	// Folders are the folders of the request, the outermost first.
	Folders []Scope
	// Collection is the collection of the request, or nil.
	Collection *Scope
	// Globals are the variables of the user.
	Globals []Entry
	// Values gives the current values and the secrets of the environment and the collection; nil: only the initial values.
	Values *Values
	// Dynamic gives the dynamic variables.
	Dynamic Dynamic
}

// found is a variable with a value.
type found struct {
	value  string
	source Source
	scope  string
	secret bool
	// missing is true for a secret variable that the keyring has no value for.
	missing bool
}

// Resolver resolves text against the variables it was built with. It does no I/O (Build read the secrets), so it may be used
// from any goroutine.
type Resolver struct {
	layers  []map[string]found
	dynamic Dynamic
}

// Build reads the current values and the secrets of the input and makes the Resolver. A keyring that fails is the error: the
// resolver is still returned and holds every value that could be read; the secrets that could not are missing.
func Build(ctx context.Context, in Input) (*Resolver, error) {
	r := &Resolver{dynamic: in.Dynamic}
	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	locals := map[string]found{}
	for k, v := range in.Locals {
		locals[k] = found{value: v, source: SourceLocal}
	}
	r.layers = append(r.layers, locals)
	if e := in.Environment; e != nil {
		cur, initial, err := scopeLayers(ctx, in.Values, *e, SourceEnvironmentCurrent, SourceEnvironmentInitial)
		note(err)
		r.layers = append(r.layers, cur, initial)
	}
	for i := len(in.Folders) - 1; i >= 0; i-- {
		_, initial, err := scopeLayers(ctx, nil, in.Folders[i], SourceFolder, SourceFolder)
		note(err)
		r.layers = append(r.layers, initial)
	}
	if c := in.Collection; c != nil {
		cur, initial, err := scopeLayers(ctx, in.Values, *c, SourceCollectionCurrent, SourceCollectionInitial)
		note(err)
		r.layers = append(r.layers, cur, initial)
	}
	globals := map[string]found{}
	for _, g := range in.Globals {
		if g.Enabled {
			globals[g.Name] = found{value: g.Value, source: SourceGlobal, secret: g.Secret}
		}
	}
	r.layers = append(r.layers, globals)
	return r, firstErr
}

// scopeLayers makes the layer of the current values of a scope and the layer of its initial values. A disabled variable is
// skipped. A secret variable's value comes from the keyring, and is a current value.
func scopeLayers(ctx context.Context, vals *Values, s Scope, curSrc, initSrc Source) (cur, initial map[string]found, err error) {
	cur, initial = map[string]found{}, map[string]found{}
	var currentValues map[string]string
	if vals != nil && s.Owner != "" {
		currentValues = vals.All(s.Owner)
	}
	for _, v := range s.Variables {
		if !v.Enabled {
			continue
		}
		f := found{source: initSrc, scope: s.Name, secret: v.Secret}
		if v.Secret {
			f.source = curSrc
			f.missing = true
			if vals != nil && s.Owner != "" {
				secret, ok, serr := vals.Secret(ctx, s.Owner, v.Name)
				if serr != nil && err == nil {
					err = serr
				}
				if ok {
					f.value, f.missing = secret, false
				}
			}
			cur[v.Name] = f
			continue
		}
		if c, ok := currentValues[v.Name]; ok {
			cur[v.Name] = found{value: c, source: curSrc, scope: s.Name}
		}
		f.value = v.Value
		initial[v.Name] = f
	}
	// a current value that an extraction rule made for a name the scope does not declare is a variable of the scope too (Postman's
	// pm.environment.set does the same); a name that is declared but turned off stays off
	names := declaredNames(s.Variables)
	for name, c := range currentValues {
		if _, declared := names[name]; !declared {
			cur[name] = found{value: c, source: curSrc, scope: s.Name}
		}
	}
	return cur, initial, err
}

// Lookup returns the value a name has and where it came from.
func (r *Resolver) Lookup(name string) (value string, source Source, ok bool) {
	for _, layer := range r.layers {
		if f, has := layer[name]; has && !f.missing {
			return f.value, f.source, true
		}
	}
	return "", "", false
}

// Result is the text after the variables in it were replaced.
type Result struct {
	// Text is the text, with every name that was found replaced and the others left as they were ({{name}}).
	Text string
	// Unresolved are the names that were not found (in the order of the text, once each); a dynamic variable that is unknown or
	// not allowed is listed with its "$".
	Unresolved []string
	// MissingSecrets are the names of secret variables that the keyring has no value for: the caller asks the user.
	MissingSecrets []string
}

// Resolve replaces the {{name}} of the text. A value that has {{names}} itself is resolved too (to a depth of ten; a name that
// leads back to itself stays unresolved). Dynamic variables are replaced by a new value at each place.
func (r *Resolver) Resolve(text string) Result {
	var res Result
	res.Text = r.expand(text, nil, &res)
	return res
}

func (r *Resolver) expand(text string, stack []string, res *Result) string {
	if !strings.Contains(text, openMark) {
		return text
	}
	var b strings.Builder
	for {
		start := strings.Index(text, openMark)
		if start < 0 {
			break
		}
		end := strings.Index(text[start+len(openMark):], closeMark)
		if end < 0 {
			break
		}
		end += start + len(openMark)
		b.WriteString(text[:start])
		raw := text[start : end+len(closeMark)]
		expr := strings.TrimSpace(text[start+len(openMark) : end])
		b.WriteString(r.one(expr, raw, stack, res))
		text = text[end+len(closeMark):]
	}
	b.WriteString(text)
	return b.String()
}

// one resolves one {{expr}}; raw is what to leave when it cannot be resolved.
func (r *Resolver) one(expr, raw string, stack []string, res *Result) string {
	if strings.HasPrefix(expr, "$") {
		if v, ok := r.dynamic.value(expr); ok {
			return v
		}
		addOnce(&res.Unresolved, expr)
		return raw
	}
	if expr == "" || len(stack) >= maxDepth || containsString(stack, expr) {
		addOnce(&res.Unresolved, expr)
		return raw
	}
	for _, layer := range r.layers {
		f, has := layer[expr]
		if !has {
			continue
		}
		if f.missing {
			addOnce(&res.MissingSecrets, expr)
			return raw
		}
		return r.expand(f.value, append(stack[:len(stack):len(stack)], expr), res)
	}
	addOnce(&res.Unresolved, expr)
	return raw
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func addOnce(list *[]string, s string) {
	if !containsString(*list, s) {
		*list = append(*list, s)
	}
}

// declaredNames are the names a scope declares, the ones that are turned off included.
func declaredNames(vars []format.Variable) map[string]struct{} {
	out := make(map[string]struct{}, len(vars))
	for _, v := range vars {
		out[v.Name] = struct{}{}
	}
	return out
}
