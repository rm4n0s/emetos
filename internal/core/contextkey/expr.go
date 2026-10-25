package contextkey

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
)

// Context gives the value of a key; a key that is not set has the value nil.
type Context interface {
	Value(key string) any
}

// MapContext is a Context of a map, for tests and for evaluating against fixed values.
type MapContext map[string]any

// Value implements Context.
func (m MapContext) Value(key string) any { return m[key] }

// ExprType tells what an expression is; it also orders expressions of different kinds.
type ExprType int

// The kinds of expressions, in the order they sort in.
const (
	// TypeFalse is false.
	TypeFalse ExprType = iota
	// TypeTrue is true.
	TypeTrue
	// TypeDefined is a key alone: true when its value is truthy.
	TypeDefined
	// TypeNot is !key.
	TypeNot
	// TypeEquals is key == value.
	TypeEquals
	// TypeNotEquals is key != value.
	TypeNotEquals
	// TypeAnd is a && b.
	TypeAnd
	// TypeRegex is key =~ /regex/.
	TypeRegex
	// TypeNotRegex is the negation of a regex.
	TypeNotRegex
	// TypeOr is a || b.
	TypeOr
	// TypeIn is key in otherKey.
	TypeIn
	// TypeNotIn is key not in otherKey.
	TypeNotIn
	// TypeGreater is key > number.
	TypeGreater
	// TypeGreaterEquals is key >= number.
	TypeGreaterEquals
	// TypeSmaller is key < number.
	TypeSmaller
	// TypeSmallerEquals is key <= number.
	TypeSmallerEquals
)

// Expr is a when-clause expression in the normal form VS Code keeps: && and || are flat, sorted,
// without repeated terms, and || is the outermost operator (no parentheses). Build one with
// Parse or with the functions of this package. An Expr never changes, so it can be
// shared by goroutines.
//
// Ported from VS Code: src/vs/platform/contextkey/common/contextkey.ts
type Expr interface {
	// Type tells what kind of expression it is.
	Type() ExprType
	// Cmp orders expressions: negative, zero or positive.
	Cmp(other Expr) int
	// Equals tells whether two expressions are the same in normal form.
	Equals(other Expr) bool
	// SubstituteConstants replaces the keys with a fixed value (isLinux, isMac, …) by true or
	// false.
	SubstituteConstants() Expr
	// Evaluate tells whether the expression holds in a context.
	Evaluate(ctx Context) bool
	// Serialize writes the expression so that Parse reads it back.
	Serialize() string
	// Keys lists the keys the expression depends on, with repeats.
	Keys() []string
	// Negate returns the opposite expression, in normal form.
	Negate() Expr
}

// constantValues are the keys whose value never changes. Emetos runs on Linux only.
var constantValues = map[string]bool{
	"false":       false,
	"true":        true,
	"isMac":       false,
	"isLinux":     true,
	"isWindows":   false,
	"isChromeOS":  false,
	"isWeb":       false,
	"isMacNative": false,
	"isEdge":      false,
	"isFirefox":   false,
	"isChrome":    false,
	"isSafari":    false,
}

// negation caches the negation of an expression so that negating twice gives the first
// expression back.
type negation struct {
	mu sync.Mutex
	e  Expr
}

func (n *negation) get(make func() Expr) Expr {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.e == nil {
		n.e = make()
	}
	return n.e
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cmpKeyValue(k1 string, v1 any, k2 string, v2 any) int {
	if c := compareStrings(k1, k2); c != 0 {
		return c
	}
	return cmpValues(v1, v2)
}

// ---- false and true ----

type falseExpr struct{}
type trueExpr struct{}

// False is the expression that never holds.
func False() Expr { return falseExpr{} }

// True is the expression that always holds.
func True() Expr { return trueExpr{} }

func (falseExpr) Type() ExprType              { return TypeFalse }
func (falseExpr) Cmp(o Expr) int              { return int(TypeFalse) - int(o.Type()) }
func (falseExpr) Equals(o Expr) bool          { return o.Type() == TypeFalse }
func (e falseExpr) SubstituteConstants() Expr { return e }
func (falseExpr) Evaluate(Context) bool       { return false }
func (falseExpr) Serialize() string           { return "false" }
func (falseExpr) Keys() []string              { return nil }
func (falseExpr) Negate() Expr                { return trueExpr{} }
func (trueExpr) Type() ExprType               { return TypeTrue }
func (trueExpr) Cmp(o Expr) int               { return int(TypeTrue) - int(o.Type()) }
func (trueExpr) Equals(o Expr) bool           { return o.Type() == TypeTrue }
func (e trueExpr) SubstituteConstants() Expr  { return e }
func (trueExpr) Evaluate(Context) bool        { return true }
func (trueExpr) Serialize() string            { return "true" }
func (trueExpr) Keys() []string               { return nil }
func (trueExpr) Negate() Expr                 { return falseExpr{} }

func constant(key string) (value, ok bool) {
	value, ok = constantValues[key]
	return
}

func constExpr(v bool) Expr {
	if v {
		return trueExpr{}
	}
	return falseExpr{}
}

// truthy is !!value in JavaScript.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && x == x
	case string:
		return x != ""
	}
	return true
}

// ---- key ----

type definedExpr struct {
	key string
	neg *negation
}

// Has is the expression "key": it holds when the value of the key is truthy.
func Has(key string) Expr { return newDefined(key, nil) }

func newDefined(key string, negated Expr) Expr {
	if v, ok := constant(key); ok {
		return constExpr(v)
	}
	return &definedExpr{key: key, neg: &negation{e: negated}}
}

func (e *definedExpr) Type() ExprType { return TypeDefined }
func (e *definedExpr) Cmp(o Expr) int {
	if o.Type() != TypeDefined {
		return int(TypeDefined) - int(o.Type())
	}
	return compareStrings(e.key, o.(*definedExpr).key)
}
func (e *definedExpr) Equals(o Expr) bool {
	d, ok := o.(*definedExpr)
	return ok && d.key == e.key
}
func (e *definedExpr) SubstituteConstants() Expr {
	if v, ok := constant(e.key); ok {
		return constExpr(v)
	}
	return e
}
func (e *definedExpr) Evaluate(c Context) bool { return truthy(c.Value(e.key)) }
func (e *definedExpr) Serialize() string       { return e.key }
func (e *definedExpr) Keys() []string          { return []string{e.key} }
func (e *definedExpr) Negate() Expr {
	return e.neg.get(func() Expr { return newNot(e.key, e) })
}

type notExpr struct {
	key string
	neg *negation
}

// Not is the expression "!key".
func Not(key string) Expr { return newNot(key, nil) }

func newNot(key string, negated Expr) Expr {
	if v, ok := constant(key); ok {
		return constExpr(!v)
	}
	return &notExpr{key: key, neg: &negation{e: negated}}
}

func (e *notExpr) Type() ExprType { return TypeNot }
func (e *notExpr) Cmp(o Expr) int {
	if o.Type() != TypeNot {
		return int(TypeNot) - int(o.Type())
	}
	return compareStrings(e.key, o.(*notExpr).key)
}
func (e *notExpr) Equals(o Expr) bool {
	n, ok := o.(*notExpr)
	return ok && n.key == e.key
}
func (e *notExpr) SubstituteConstants() Expr {
	if v, ok := constant(e.key); ok {
		return constExpr(!v)
	}
	return e
}
func (e *notExpr) Evaluate(c Context) bool { return !truthy(c.Value(e.key)) }
func (e *notExpr) Serialize() string       { return "!" + e.key }
func (e *notExpr) Keys() []string          { return []string{e.key} }
func (e *notExpr) Negate() Expr {
	return e.neg.get(func() Expr { return newDefined(e.key, e) })
}

// ---- == and != ----

// normalizeValue turns the value of a comparison into a string, a float64 or a bool.
func normalizeValue(v any) any {
	switch x := v.(type) {
	case string, float64, bool:
		return v
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return jsString(v)
}

type equalsExpr struct {
	key   string
	value any
	neg   *negation
}

// Equals is the expression "key == value". A value of true or false gives "key" or "!key".
func Equals(key string, value any) Expr { return newEquals(key, value, nil) }

func newEquals(key string, value any, negated Expr) Expr {
	value = normalizeValue(value)
	if b, ok := value.(bool); ok {
		if b {
			return newDefined(key, negated)
		}
		return newNot(key, negated)
	}
	if v, ok := constant(key); ok {
		return constExpr(value == boolString(v))
	}
	return &equalsExpr{key: key, value: value, neg: &negation{e: negated}}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (e *equalsExpr) Type() ExprType { return TypeEquals }
func (e *equalsExpr) Cmp(o Expr) int {
	if o.Type() != TypeEquals {
		return int(TypeEquals) - int(o.Type())
	}
	x := o.(*equalsExpr)
	return cmpKeyValue(e.key, e.value, x.key, x.value)
}
func (e *equalsExpr) Equals(o Expr) bool {
	x, ok := o.(*equalsExpr)
	return ok && x.key == e.key && x.value == e.value
}
func (e *equalsExpr) SubstituteConstants() Expr {
	if v, ok := constant(e.key); ok {
		return constExpr(e.value == boolString(v))
	}
	return e
}
func (e *equalsExpr) Evaluate(c Context) bool { return looseEquals(c.Value(e.key), e.value) }
func (e *equalsExpr) Serialize() string       { return e.key + " == '" + jsString(e.value) + "'" }
func (e *equalsExpr) Keys() []string          { return []string{e.key} }
func (e *equalsExpr) Negate() Expr {
	return e.neg.get(func() Expr { return newNotEquals(e.key, e.value, e) })
}

type notEqualsExpr struct {
	key   string
	value any
	neg   *negation
}

// NotEquals is the expression "key != value".
func NotEquals(key string, value any) Expr { return newNotEquals(key, value, nil) }

func newNotEquals(key string, value any, negated Expr) Expr {
	value = normalizeValue(value)
	if b, ok := value.(bool); ok {
		if b {
			return newNot(key, negated)
		}
		return newDefined(key, negated)
	}
	if v, ok := constant(key); ok {
		return constExpr(value != boolString(v))
	}
	return &notEqualsExpr{key: key, value: value, neg: &negation{e: negated}}
}

func (e *notEqualsExpr) Type() ExprType { return TypeNotEquals }
func (e *notEqualsExpr) Cmp(o Expr) int {
	if o.Type() != TypeNotEquals {
		return int(TypeNotEquals) - int(o.Type())
	}
	x := o.(*notEqualsExpr)
	return cmpKeyValue(e.key, e.value, x.key, x.value)
}
func (e *notEqualsExpr) Equals(o Expr) bool {
	x, ok := o.(*notEqualsExpr)
	return ok && x.key == e.key && x.value == e.value
}
func (e *notEqualsExpr) SubstituteConstants() Expr {
	if v, ok := constant(e.key); ok {
		return constExpr(e.value != boolString(v))
	}
	return e
}
func (e *notEqualsExpr) Evaluate(c Context) bool { return !looseEquals(c.Value(e.key), e.value) }
func (e *notEqualsExpr) Serialize() string       { return e.key + " != '" + jsString(e.value) + "'" }
func (e *notEqualsExpr) Keys() []string          { return []string{e.key} }
func (e *notEqualsExpr) Negate() Expr {
	return e.neg.get(func() Expr { return newEquals(e.key, e.value, e) })
}

// ---- in and not in ----

type inExpr struct {
	key, valueKey string
	neg           *negation
}

// In is the expression "key in valueKey": the value of key is an element of the array, or a
// property of the object, that valueKey holds.
func In(key, valueKey string) Expr { return &inExpr{key: key, valueKey: valueKey, neg: &negation{}} }

func (e *inExpr) Type() ExprType { return TypeIn }
func (e *inExpr) Cmp(o Expr) int {
	if o.Type() != TypeIn {
		return int(TypeIn) - int(o.Type())
	}
	x := o.(*inExpr)
	return cmpKeyValue(e.key, e.valueKey, x.key, x.valueKey)
}
func (e *inExpr) Equals(o Expr) bool {
	x, ok := o.(*inExpr)
	return ok && x.key == e.key && x.valueKey == e.valueKey
}
func (e *inExpr) SubstituteConstants() Expr { return e }
func (e *inExpr) Evaluate(c Context) bool {
	source, item := c.Value(e.valueKey), c.Value(e.key)
	switch s := source.(type) {
	case []any:
		for _, x := range s {
			if strictEquals(x, item) {
				return true
			}
		}
		return false
	case map[string]any:
		if name, ok := item.(string); ok {
			_, has := s[name]
			return has
		}
	}
	return false
}
func (e *inExpr) Serialize() string { return e.key + " in '" + e.valueKey + "'" }
func (e *inExpr) Keys() []string    { return []string{e.key, e.valueKey} }
func (e *inExpr) Negate() Expr {
	return e.neg.get(func() Expr { return &notInExpr{key: e.key, valueKey: e.valueKey, negated: e} })
}

// strictEquals is a === b for context values.
func strictEquals(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case float64:
		y, ok := b.(float64)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	}
	return false
}

type notInExpr struct {
	key, valueKey string
	negated       *inExpr
}

// NotIn is the expression "key not in valueKey".
func NotIn(key, valueKey string) Expr {
	return &notInExpr{key: key, valueKey: valueKey, negated: In(key, valueKey).(*inExpr)}
}

func (e *notInExpr) Type() ExprType { return TypeNotIn }
func (e *notInExpr) Cmp(o Expr) int {
	if o.Type() != TypeNotIn {
		return int(TypeNotIn) - int(o.Type())
	}
	return e.negated.Cmp(o.(*notInExpr).negated)
}
func (e *notInExpr) Equals(o Expr) bool {
	x, ok := o.(*notInExpr)
	return ok && e.negated.Equals(x.negated)
}
func (e *notInExpr) SubstituteConstants() Expr { return e }
func (e *notInExpr) Evaluate(c Context) bool   { return !e.negated.Evaluate(c) }
func (e *notInExpr) Serialize() string         { return e.key + " not in '" + e.valueKey + "'" }
func (e *notInExpr) Keys() []string            { return e.negated.Keys() }
func (e *notInExpr) Negate() Expr              { return e.negated }

// ---- comparisons ----

// numberOrString converts the value of a comparison: a string that starts with a number
// becomes that number. It reports false for what is neither a number nor a string.
func numberOrString(v any) (any, bool) {
	switch x := v.(type) {
	case string:
		if n := jsParseFloat(x); n == n {
			return n, true
		}
		return x, true
	case float64:
		return x, true
	case int:
		return float64(x), true
	}
	return nil, false
}

type compareOp int

const (
	opGreater compareOp = iota
	opGreaterEquals
	opSmaller
	opSmallerEquals
)

type compareExpr struct {
	op    compareOp
	key   string
	value any // float64 or string
	neg   *negation
}

var compareTypes = [...]ExprType{TypeGreater, TypeGreaterEquals, TypeSmaller, TypeSmallerEquals}
var compareSigns = [...]string{">", ">=", "<", "<="}

// negatedOp is the operator of the opposite comparison.
var negatedOp = [...]compareOp{opSmallerEquals, opSmaller, opGreaterEquals, opGreater}

func newCompare(op compareOp, key string, value any, negated Expr) Expr {
	v, ok := numberOrString(value)
	if !ok {
		return falseExpr{}
	}
	return &compareExpr{op: op, key: key, value: v, neg: &negation{e: negated}}
}

// Greater is the expression "key > value". The value is a number, or a string that starts
// with one; with any other string the expression never holds.
func Greater(key string, value any) Expr { return newCompare(opGreater, key, value, nil) }

// GreaterEquals is the expression "key >= value".
func GreaterEquals(key string, value any) Expr { return newCompare(opGreaterEquals, key, value, nil) }

// Smaller is the expression "key < value".
func Smaller(key string, value any) Expr { return newCompare(opSmaller, key, value, nil) }

// SmallerEquals is the expression "key <= value".
func SmallerEquals(key string, value any) Expr { return newCompare(opSmallerEquals, key, value, nil) }

func (e *compareExpr) Type() ExprType { return compareTypes[e.op] }
func (e *compareExpr) Cmp(o Expr) int {
	if o.Type() != e.Type() {
		return int(e.Type()) - int(o.Type())
	}
	x := o.(*compareExpr)
	return cmpKeyValue(e.key, e.value, x.key, x.value)
}
func (e *compareExpr) Equals(o Expr) bool {
	x, ok := o.(*compareExpr)
	return ok && x.op == e.op && x.key == e.key && x.value == e.value
}
func (e *compareExpr) SubstituteConstants() Expr { return e }
func (e *compareExpr) Evaluate(c Context) bool {
	limit, ok := e.value.(float64)
	if !ok {
		return false
	}
	n := jsParseFloat(c.Value(e.key))
	switch e.op {
	case opGreater:
		return n > limit
	case opGreaterEquals:
		return n >= limit
	case opSmaller:
		return n < limit
	}
	return n <= limit
}
func (e *compareExpr) Serialize() string {
	return e.key + " " + compareSigns[e.op] + " " + jsString(e.value)
}
func (e *compareExpr) Keys() []string { return []string{e.key} }
func (e *compareExpr) Negate() Expr {
	return e.neg.get(func() Expr { return newCompare(negatedOp[e.op], e.key, e.value, e) })
}

// ---- regular expressions ----

type regexExpr struct {
	key    string
	source string // as RegExp#source writes it
	flags  string
	re     *regexp.Regexp // nil when the engine cannot run the pattern
	neg    *negation
}

// Regex is the expression "key =~ /source/flags". The pattern is written as in JavaScript;
// the flags i, m and s are used, g and y are dropped and u is kept but has no effect. It fails when the pattern or the
// flags are not valid.
func Regex(key, source, flags string) (Expr, error) {
	return newRegex(key, source, flags)
}

func newRegex(key, source, flags string) (*regexExpr, error) {
	canon, ok := canonicalFlags(flags)
	if !ok {
		return nil, errors.New("WhenRegexInvalid", "the regular expression is not valid", "source", source, "flags", flags)
	}
	canon = strings.NewReplacer("g", "", "y", "").Replace(canon)
	re, ok := compileRegex(source, canon)
	if !ok {
		return nil, errors.New("WhenRegexInvalid", "the regular expression is not valid", "source", source, "flags", flags)
	}
	return &regexExpr{key: key, source: jsRegexSource(source), flags: canon, re: re, neg: &negation{}}, nil
}

func (e *regexExpr) Type() ExprType { return TypeRegex }
func (e *regexExpr) Cmp(o Expr) int {
	if o.Type() != TypeRegex {
		return int(TypeRegex) - int(o.Type())
	}
	x := o.(*regexExpr)
	if c := compareStrings(e.key, x.key); c != 0 {
		return c
	}
	return compareStrings(e.source, x.source)
}
func (e *regexExpr) Equals(o Expr) bool {
	x, ok := o.(*regexExpr)
	return ok && x.key == e.key && x.source == e.source
}
func (e *regexExpr) SubstituteConstants() Expr { return e }
func (e *regexExpr) Evaluate(c Context) bool {
	return e.re != nil && e.re.MatchString(jsString(c.Value(e.key)))
}
func (e *regexExpr) Serialize() string { return e.key + " =~ /" + e.source + "/" + e.flags }
func (e *regexExpr) Keys() []string    { return []string{e.key} }
func (e *regexExpr) Negate() Expr {
	return e.neg.get(func() Expr { return &notRegexExpr{actual: e} })
}

type notRegexExpr struct{ actual *regexExpr }

func (e *notRegexExpr) Type() ExprType { return TypeNotRegex }
func (e *notRegexExpr) Cmp(o Expr) int {
	if o.Type() != TypeNotRegex {
		return int(TypeNotRegex) - int(o.Type())
	}
	return e.actual.Cmp(o.(*notRegexExpr).actual)
}
func (e *notRegexExpr) Equals(o Expr) bool {
	x, ok := o.(*notRegexExpr)
	return ok && e.actual.Equals(x.actual)
}
func (e *notRegexExpr) SubstituteConstants() Expr { return e }
func (e *notRegexExpr) Evaluate(c Context) bool   { return !e.actual.Evaluate(c) }
func (e *notRegexExpr) Serialize() string         { return "!(" + e.actual.Serialize() + ")" }
func (e *notRegexExpr) Keys() []string            { return e.actual.Keys() }
func (e *notRegexExpr) Negate() Expr              { return e.actual }

// ---- && and || ----

type andExpr struct {
	exprs []Expr
	neg   *negation
}

type orExpr struct {
	exprs []Expr
	neg   *negation
}

// And joins expressions with &&, in normal form: nil terms are dropped, true terms are
// dropped, a false term makes the result false, repeated terms are merged, and a || inside
// is distributed (a && (b || c) is a && b || a && c). It returns nil when there is no term.
func And(exprs ...Expr) Expr { return normalizeAnd(exprs, nil, true) }

// Or joins expressions with ||, in normal form. It returns nil when there is no term.
func Or(exprs ...Expr) Expr { return normalizeOr(exprs, nil, true) }

func cmpLists(a, b []Expr) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	for i := range a {
		if c := a[i].Cmp(b[i]); c != 0 {
			return c
		}
	}
	return 0
}

func equalLists(a, b []Expr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equals(b[i]) {
			return false
		}
	}
	return true
}

// substituteAll substitutes the constants of the terms; changed tells whether one changed.
func substituteAll(exprs []Expr) (out []Expr, changed bool) {
	out = make([]Expr, len(exprs))
	for i, e := range exprs {
		out[i] = e.SubstituteConstants()
		if out[i] != e {
			changed = true
		}
	}
	return out, changed
}

func sortExprs(exprs []Expr) {
	sort.SliceStable(exprs, func(i, j int) bool { return exprs[i].Cmp(exprs[j]) < 0 })
}

func dedupe(exprs []Expr) []Expr {
	for i := 1; i < len(exprs); i++ {
		if exprs[i-1].Equals(exprs[i]) {
			exprs = slices.Delete(exprs, i, i+1)
			i--
		}
	}
	return exprs
}

func normalizeAnd(in []Expr, negated Expr, extraRedundantCheck bool) Expr {
	var exprs []Expr
	hasTrue := false
	for _, e := range in {
		switch {
		case e == nil:
			continue
		case e.Type() == TypeTrue:
			hasTrue = true
		case e.Type() == TypeFalse:
			return falseExpr{}
		case e.Type() == TypeAnd:
			exprs = append(exprs, e.(*andExpr).exprs...)
		default:
			exprs = append(exprs, e)
		}
	}
	if len(exprs) == 0 {
		if hasTrue {
			return trueExpr{}
		}
		return nil
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	sortExprs(exprs)
	exprs = dedupe(exprs)
	if len(exprs) == 1 {
		return exprs[0]
	}

	// Distribute an || over the rest: there are no parentheses. The ors are at the end.
	for len(exprs) > 1 {
		last := exprs[len(exprs)-1]
		if last.Type() != TypeOr {
			break
		}
		exprs = exprs[:len(exprs)-1]
		secondToLast := exprs[len(exprs)-1]
		exprs = exprs[:len(exprs)-1]
		finished := len(exprs) == 0
		terms := last.(*orExpr).exprs
		distributed := make([]Expr, len(terms))
		for i, t := range terms {
			distributed[i] = normalizeAnd([]Expr{t, secondToLast}, nil, extraRedundantCheck)
		}
		if result := normalizeOr(distributed, nil, finished); result != nil {
			exprs = append(exprs, result)
			sortExprs(exprs)
		}
	}
	if len(exprs) == 1 {
		return exprs[0]
	}

	if extraRedundantCheck {
		for i := range exprs {
			for j := i + 1; j < len(exprs); j++ {
				if exprs[i].Negate().Equals(exprs[j]) { // a && !a
					return falseExpr{}
				}
			}
		}
		if len(exprs) == 1 {
			return exprs[0]
		}
	}
	return &andExpr{exprs: exprs, neg: &negation{e: negated}}
}

func normalizeOr(in []Expr, negated Expr, extraRedundantCheck bool) Expr {
	var exprs []Expr
	hasFalse := false
	for _, e := range in {
		switch {
		case e == nil:
			continue
		case e.Type() == TypeFalse:
			hasFalse = true
		case e.Type() == TypeTrue:
			return trueExpr{}
		case e.Type() == TypeOr:
			exprs = append(exprs, e.(*orExpr).exprs...)
		default:
			exprs = append(exprs, e)
		}
	}
	if len(exprs) == 0 && hasFalse {
		return falseExpr{}
	}
	sortExprs(exprs)
	if len(exprs) == 0 {
		return nil
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	exprs = dedupe(exprs)
	if len(exprs) == 1 {
		return exprs[0]
	}
	if extraRedundantCheck {
		for i := range exprs {
			for j := i + 1; j < len(exprs); j++ {
				if exprs[i].Negate().Equals(exprs[j]) { // a || !a
					return trueExpr{}
				}
			}
		}
		if len(exprs) == 1 {
			return exprs[0]
		}
	}
	return &orExpr{exprs: exprs, neg: &negation{e: negated}}
}

func (e *andExpr) Type() ExprType { return TypeAnd }
func (e *andExpr) Cmp(o Expr) int {
	if o.Type() != TypeAnd {
		return int(TypeAnd) - int(o.Type())
	}
	return cmpLists(e.exprs, o.(*andExpr).exprs)
}
func (e *andExpr) Equals(o Expr) bool {
	x, ok := o.(*andExpr)
	return ok && equalLists(e.exprs, x.exprs)
}
func (e *andExpr) SubstituteConstants() Expr {
	out, changed := substituteAll(e.exprs)
	if !changed {
		return e
	}
	return normalizeAnd(out, e.neg.e, false)
}
func (e *andExpr) Evaluate(c Context) bool {
	for _, x := range e.exprs {
		if !x.Evaluate(c) {
			return false
		}
	}
	return true
}
func (e *andExpr) Serialize() string {
	parts := make([]string, len(e.exprs))
	for i, x := range e.exprs {
		parts[i] = x.Serialize()
	}
	return strings.Join(parts, " && ")
}
func (e *andExpr) Keys() []string {
	var keys []string
	for _, x := range e.exprs {
		keys = append(keys, x.Keys()...)
	}
	return keys
}
func (e *andExpr) Negate() Expr {
	return e.neg.get(func() Expr {
		negs := make([]Expr, len(e.exprs))
		for i, x := range e.exprs {
			negs[i] = x.Negate()
		}
		return normalizeOr(negs, e, true)
	})
}

func (e *orExpr) Type() ExprType { return TypeOr }
func (e *orExpr) Cmp(o Expr) int {
	if o.Type() != TypeOr {
		return int(TypeOr) - int(o.Type())
	}
	return cmpLists(e.exprs, o.(*orExpr).exprs)
}
func (e *orExpr) Equals(o Expr) bool {
	x, ok := o.(*orExpr)
	return ok && equalLists(e.exprs, x.exprs)
}
func (e *orExpr) SubstituteConstants() Expr {
	out, changed := substituteAll(e.exprs)
	if !changed {
		return e
	}
	return normalizeOr(out, e.neg.e, false)
}
func (e *orExpr) Evaluate(c Context) bool {
	for _, x := range e.exprs {
		if x.Evaluate(c) {
			return true
		}
	}
	return false
}
func (e *orExpr) Serialize() string {
	parts := make([]string, len(e.exprs))
	for i, x := range e.exprs {
		parts[i] = x.Serialize()
	}
	return strings.Join(parts, " || ")
}
func (e *orExpr) Keys() []string {
	var keys []string
	for _, x := range e.exprs {
		keys = append(keys, x.Keys()...)
	}
	return keys
}
func (e *orExpr) Negate() Expr {
	return e.neg.get(func() Expr {
		negs := make([]Expr, len(e.exprs))
		for i, x := range e.exprs {
			negs[i] = x.Negate()
		}
		// There are no parentheses: distribute the && over the terms of the ors, taking
		// the first two each time.
		for len(negs) > 1 {
			left, right := negs[0], negs[1]
			negs = negs[2:]
			var all []Expr
			for _, l := range terminals(left) {
				for _, r := range terminals(right) {
					all = append(all, normalizeAnd([]Expr{l, r}, nil, false))
				}
			}
			negs = append([]Expr{normalizeOr(all, nil, false)}, negs...)
		}
		return normalizeOr(negs, e, true)
	})
}

func terminals(e Expr) []Expr {
	if o, ok := e.(*orExpr); ok {
		return o.exprs
	}
	return []Expr{e}
}

// Implies tells whether it can be shown that p implies q (whenever p holds, q holds). A
// false answer means "not shown", not "does not".
func Implies(p, q Expr) bool {
	if p.Type() == TypeFalse || q.Type() == TypeTrue {
		return true
	}
	if po, ok := p.(*orExpr); ok {
		if qo, ok := q.(*orExpr); ok {
			return allIncluded(po.exprs, qo.exprs)
		}
		return false
	}
	if qo, ok := q.(*orExpr); ok {
		for _, e := range qo.exprs {
			if Implies(p, e) {
				return true
			}
		}
		return false
	}
	if pa, ok := p.(*andExpr); ok {
		if qa, ok := q.(*andExpr); ok {
			return allIncluded(qa.exprs, pa.exprs)
		}
		for _, e := range pa.exprs {
			if Implies(e, q) {
				return true
			}
		}
		return false
	}
	return p.Equals(q)
}

// allIncluded tells whether every element of p is in q; both are sorted.
func allIncluded(p, q []Expr) bool {
	pi, qi := 0, 0
	for pi < len(p) && qi < len(q) {
		switch c := p[pi].Cmp(q[qi]); {
		case c < 0:
			return false
		case c == 0:
			pi++
			qi++
		default:
			qi++
		}
	}
	return pi == len(p)
}

// EqualWithConstants tells whether two expressions, either may be nil, are the same once
// their constants are replaced.
func EqualWithConstants(a, b Expr) bool {
	var ae, be Expr
	if a != nil {
		ae = a.SubstituteConstants()
	}
	if b != nil {
		be = b.SubstituteConstants()
	}
	switch {
	case ae == nil && be == nil:
		return true
	case ae == nil || be == nil:
		return false
	}
	return ae.Equals(be)
}
