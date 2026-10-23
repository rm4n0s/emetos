package contextkey

import (
	"reflect"
	"slices"
	"sync"
)

// ChangeEvent tells which keys changed in a scope.
type ChangeEvent struct {
	// Keys are the keys whose value, as the scope sees it, changed.
	Keys []string
}

// AffectsSome tells whether one of the keys changed.
func (e ChangeEvent) AffectsSome(keys []string) bool {
	return slices.ContainsFunc(keys, func(k string) bool { return slices.Contains(e.Keys, k) })
}

// AllKeysContainedIn tells whether every changed key is one of keys.
func (e ChangeEvent) AllKeysContainedIn(keys []string) bool {
	return !slices.ContainsFunc(e.Keys, func(k string) bool { return !slices.Contains(keys, k) })
}

// Scope holds context keys for a part of the window (SPEC §11.2). Scopes form a tree:
// window, part, widget. A widget gets a child of its parent's scope and sets its keys in it;
// a lookup walks up the tree, so a key set in a scope is seen by its descendants unless
// they set it themselves. A Scope is safe for concurrent use; its listeners are called
// in the goroutine that changed a key, with no lock held.
//
// Ported from VS Code: src/vs/platform/contextkey/browser/contextKeyService.ts (ContextKeyService)
type Scope struct {
	root *Scope

	mu        sync.Mutex
	parent    *Scope
	values    map[string]any
	children  []*Scope
	listeners []*func(ChangeEvent)
	disposed  bool

	// root only: events held back by Batch.
	batchDepth int
	pending    map[*Scope][]string
	order      []*Scope
}

// NewRoot makes the root scope of a window.
func NewRoot() *Scope {
	s := &Scope{values: map[string]any{}}
	s.root = s
	return s
}

// Child makes a scope below this one. A child of a disposed scope is disposed.
func (s *Scope) Child() *Scope {
	c := &Scope{root: s.root, parent: s, values: map[string]any{}}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disposed {
		c.disposed = true
		return c
	}
	s.children = append(s.children, c)
	return c
}

// normalize turns a Go value into a context value: numbers become float64, slices []any and
// maps with string keys map[string]any.
func normalize(v any) any {
	switch x := v.(type) {
	case nil, bool, float64, string:
		return v
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = normalize(e)
		}
		return out
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.Bool:
		return rv.Bool()
	case reflect.String:
		return rv.String()
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = normalize(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			out := make(map[string]any, rv.Len())
			for _, k := range rv.MapKeys() {
				out[k.String()] = normalize(rv.MapIndex(k).Interface())
			}
			return out
		}
	}
	return v
}

// Set gives a key a value in this scope. The value may be a bool, a number, a string, a
// slice or a map with string keys (anything else counts as an object). Listeners of this
// scope and of the descendants that do not set the key themselves are told if the value, as
// they see it, changed.
func (s *Scope) Set(key string, value any) {
	value = normalize(value)
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return
	}
	old, had := s.values[key]
	s.values[key] = value
	same := had && reflect.DeepEqual(old, value)
	if !had {
		// The value this scope saw before came from its ancestors.
		same = reflect.DeepEqual(s.parent.Get(key), value)
	}
	s.mu.Unlock()
	if !same {
		s.changed(key)
	}
}

// Reset removes the key from this scope, so the value of an ancestor shows again.
func (s *Scope) Reset(key string) {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return
	}
	old, had := s.values[key]
	if !had {
		s.mu.Unlock()
		return
	}
	delete(s.values, key)
	s.mu.Unlock()
	if !reflect.DeepEqual(old, s.parent.Get(key)) {
		s.changed(key)
	}
}

// Get returns the value of a key as this scope sees it: its own, else the closest
// ancestor's, else nil.
func (s *Scope) Get(key string) any {
	for cur := s; cur != nil; cur = cur.parentOf() {
		cur.mu.Lock()
		v, ok := cur.values[key]
		cur.mu.Unlock()
		if ok {
			return v
		}
	}
	return nil
}

// parentOf returns the parent, which never changes after the scope is made.
func (s *Scope) parentOf() *Scope { return s.parent }

// Value implements Context.
func (s *Scope) Value(key string) any { return s.Get(key) }

// Matches tells whether an expression holds in this scope. A nil expression always does.
func (s *Scope) Matches(expr Expr) bool {
	return expr == nil || expr.Evaluate(s)
}

// OnChange registers fn to be called when a key changes in this scope or above it
// without being hidden by this scope. The returned function removes it.
func (s *Scope) OnChange(fn func(ChangeEvent)) (unsubscribe func()) {
	p := &fn
	s.mu.Lock()
	if !s.disposed {
		s.listeners = append(s.listeners, p)
	}
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.listeners = slices.DeleteFunc(s.listeners, func(x *func(ChangeEvent)) bool { return x == p })
	}
}

// Batch runs fn and holds the change events until it returns, so a listener hears of all
// the keys that fn changed in one event. Batches of the same tree nest.
func (s *Scope) Batch(fn func()) {
	root := s.root
	root.mu.Lock()
	root.batchDepth++
	root.mu.Unlock()
	defer func() {
		root.mu.Lock()
		root.batchDepth--
		var flush map[*Scope][]string
		var order []*Scope
		if root.batchDepth == 0 {
			flush, order = root.pending, root.order
			root.pending, root.order = nil, nil
		}
		root.mu.Unlock()
		for _, sc := range order {
			sc.notify(flush[sc])
		}
	}()
	fn()
}

// Dispose removes the scope from its parent, disposes the scopes below it and forgets its
// keys and listeners. A disposed scope ignores Set and Reset.
func (s *Scope) Dispose() {
	s.mu.Lock()
	if s.disposed {
		s.mu.Unlock()
		return
	}
	s.disposed = true
	children := s.children
	s.children, s.listeners, s.values = nil, nil, map[string]any{}
	s.mu.Unlock()
	for _, c := range children {
		c.Dispose()
	}
	if p := s.parent; p != nil {
		p.mu.Lock()
		p.children = slices.DeleteFunc(p.children, func(x *Scope) bool { return x == s })
		p.mu.Unlock()
	}
}

// changed tells the listeners of this scope and of the scopes below it that see the key
// through this one.
func (s *Scope) changed(key string) {
	var affected []*Scope
	var walk func(sc *Scope)
	walk = func(sc *Scope) {
		affected = append(affected, sc)
		sc.mu.Lock()
		children := slices.Clone(sc.children)
		sc.mu.Unlock()
		for _, c := range children {
			c.mu.Lock()
			_, shadowed := c.values[key]
			c.mu.Unlock()
			if !shadowed {
				walk(c)
			}
		}
	}
	walk(s)
	root := s.root
	for _, sc := range affected {
		root.mu.Lock()
		if root.batchDepth > 0 {
			if root.pending == nil {
				root.pending = map[*Scope][]string{}
			}
			if _, ok := root.pending[sc]; !ok {
				root.order = append(root.order, sc)
			}
			if !slices.Contains(root.pending[sc], key) {
				root.pending[sc] = append(root.pending[sc], key)
			}
			root.mu.Unlock()
			continue
		}
		root.mu.Unlock()
		sc.notify([]string{key})
	}
}

func (s *Scope) notify(keys []string) {
	s.mu.Lock()
	listeners := slices.Clone(s.listeners)
	s.mu.Unlock()
	for _, l := range listeners {
		(*l)(ChangeEvent{Keys: slices.Clone(keys)})
	}
}
