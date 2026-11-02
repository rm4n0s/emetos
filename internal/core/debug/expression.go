package debug

import (
	"context"
	"strconv"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/errors"
)

// BaseChunkSize is how many children are fetched at once; a container with more indexed children shows them as chunks that are
// fetched when opened (variable paging, VS Code's issue 9537).
const BaseChunkSize = 100

// NotAvailable is the value of a watch expression that has not been evaluated (VS Code's "not available").
const NotAvailable = "not available"

// Node is what the Variables, Watch and Debug Console views show as a tree: a scope, a variable or an expression.
type Node interface {
	// ID is the id of the node, the same across stops so that a view keeps what is expanded.
	ID() string
	// Value is the value as the adapter gave it.
	Value() string
	// Type is the type name, or empty.
	Type() string
	// Reference is the variablesReference, 0 for a node without children.
	Reference() int
	// HasChildren tells whether the node can be expanded (and is not lazy).
	HasChildren() bool
	// Children loads the children, once; the answer is kept until the node's reference changes. A failure of the adapter gives one
	// child whose value is the message, as VS Code shows it. The error is only the end of ctx.
	Children(ctx context.Context) ([]*Variable, error)
	// ValueChanged is true when the value is not the one it had at the last stop.
	ValueChanged() bool
	// PresentationHint is the adapter's hint for the node.
	PresentationHint() *dap.VariablePresentationHint
	// NamedVariables and IndexedVariables are the numbers of children the adapter announced, for paging.
	NamedVariables() int
	IndexedVariables() int
	// MemoryReference is the adapter's reference of the node's memory, or empty.
	MemoryReference() string
	// Session is the session that gave the node, nil for an expression nobody has evaluated.
	Session() *Session
}

// childLoad is the loading of the children of a container, shared by the callers that ask while it runs.
type childLoad struct {
	done chan struct{}
	vars []*Variable
}

// container is the part of a scope, a variable and an expression that holds a value and loads children. Its members are guarded by
// its own lock, which is never held while a request runs.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (ExpressionContainer)
type container struct {
	id       string
	threadID int

	mu               sync.Mutex
	sess             *Session
	reference        int
	namedVariables   int
	indexedVariables int
	memoryReference  string
	startOfVariables int
	hint             *dap.VariablePresentationHint
	valueLocation    int
	value            string
	typ              string
	valueChanged     bool
	children         *childLoad
}

// ID is the id of the node.
func (c *container) ID() string { return c.id }

// Session is the session that gave the node.
func (c *container) Session() *Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sess
}

// Value is the value of the node.
func (c *container) Value() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.value
}

// Type is the type of the node.
func (c *container) Type() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.typ
}

// Reference is the variablesReference of the node.
func (c *container) Reference() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reference
}

// ValueChanged is true when the value differs from the one the node had before.
func (c *container) ValueChanged() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.valueChanged
}

// PresentationHint is the adapter's hint.
func (c *container) PresentationHint() *dap.VariablePresentationHint {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hint
}

// NamedVariables is the number of named children the adapter announced.
func (c *container) NamedVariables() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.namedVariables
}

// IndexedVariables is the number of indexed children the adapter announced.
func (c *container) IndexedVariables() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.indexedVariables
}

// MemoryReference is the adapter's memory reference.
func (c *container) MemoryReference() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.memoryReference
}

// HasChildren is true for a node with a reference that is not lazy.
func (c *container) HasChildren() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hasChildrenLocked()
}

func (c *container) hasChildrenLocked() bool {
	return c.reference > 0 && (c.hint == nil || !c.hint.Lazy)
}

// setValueLocked sets the value and notes whether it changed since the last time a node with this id had one.
func (c *container) setValueLocked(v string) {
	c.value = v
	if c.sess == nil {
		return
	}
	prev := c.sess.m.swapValue(c.id, v)
	c.valueChanged = prev != "" && prev != NotAvailable && prev != v
}

// setReferenceLocked changes the reference and forgets the children.
func (c *container) setReferenceLocked(ref int) {
	c.reference = ref
	c.children = nil
}

// Children loads the children of the node once. See Node.
func (c *container) Children(ctx context.Context) ([]*Variable, error) {
	c.mu.Lock()
	s := c.sess
	if s == nil {
		c.mu.Unlock()
		return nil, nil
	}
	if load := c.children; load != nil {
		c.mu.Unlock()
		select {
		case <-load.done:
			return load.vars, nil
		case <-ctx.Done():
			return nil, errors.NewErr("DebugChildrenCancelled", ctx.Err()).SetMetadata("node", c.id)
		}
	}
	if !c.hasChildrenLocked() {
		c.mu.Unlock()
		return nil, nil
	}
	load := &childLoad{done: make(chan struct{})}
	c.children = load
	c.mu.Unlock()

	vars := c.fetchAll(ctx, s)
	c.mu.Lock()
	load.vars = vars
	c.mu.Unlock()
	close(load.done)
	return vars, nil
}

// fetchAll is VS Code's doGetChildren: all the children at once, or the named ones and then the indexed ones in chunks.
func (c *container) fetchAll(ctx context.Context, s *Session) []*Variable {
	c.mu.Lock()
	named, indexed, start := c.namedVariables, c.indexedVariables, c.startOfVariables
	c.mu.Unlock()
	if indexed == 0 {
		return c.fetchVariables(ctx, s, 0, 0, "")
	}
	var children []*Variable
	if named > 0 {
		children = c.fetchVariables(ctx, s, 0, 0, "named")
	}
	chunk := BaseChunkSize
	for indexed > chunk*BaseChunkSize {
		chunk *= BaseChunkSize
	}
	if indexed > chunk {
		// There are a lot of children: fake intermediate values stand for chunks.
		n := (indexed + chunk - 1) / chunk
		for i := range n {
			from := start + i*chunk
			count := min(chunk, indexed-i*chunk)
			children = append(children, newVariable(s, c.threadID, c, variableSpec{
				ref: c.reference, name: "[" + strconv.Itoa(from) + ".." + strconv.Itoa(from+count-1) + "]", indexed: count,
				hint: &dap.VariablePresentationHint{Kind: "virtual"}, available: true, start: from,
			}))
		}
		return children
	}
	return append(children, c.fetchVariables(ctx, s, start, indexed, "indexed")...)
}

// fetchVariables asks for variables and makes Variables of them.
func (c *container) fetchVariables(ctx context.Context, s *Session, start, count int, filter string) []*Variable {
	ref := c.Reference()
	body, err := s.variables(ctx, ref, c.threadID, filter, start, count)
	if err != nil {
		return []*Variable{newVariable(s, c.threadID, c, variableSpec{value: err.Error(), hint: &dap.VariablePresentationHint{Kind: "virtual"}})}
	}
	nameCount := map[string]int{}
	out := make([]*Variable, 0, len(body.Variables))
	for _, v := range body.Variables {
		n := nameCount[v.Name]
		dup := ""
		if n > 0 {
			dup = strconv.Itoa(n)
		}
		nameCount[v.Name] = n + 1
		out = append(out, newVariable(s, c.threadID, c, variableSpec{
			ref: v.VariablesReference, name: v.Name, evaluateName: v.EvaluateName, value: v.Value, named: v.NamedVariables,
			indexed: v.IndexedVariables, memRef: v.MemoryReference, hint: v.PresentationHint, typ: v.Type, available: true, dup: dup,
			declLoc: v.DeclarationLocationReference, valueLoc: v.ValueLocationReference,
		}))
	}
	if s.autoExpandLazy() {
		var wg sync.WaitGroup
		for _, v := range out {
			if h := v.PresentationHint(); h != nil && h.Lazy {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_ = v.EvaluateLazy(ctx) // the failure shows when the user expands the variable
				}()
			}
		}
		wg.Wait()
	}
	return out
}

// EvaluateLazy asks the adapter for the value of a lazy variable and takes it over. It fails with the tags of the request.
func (c *container) EvaluateLazy(ctx context.Context) error {
	c.mu.Lock()
	s, ref := c.sess, c.reference
	c.mu.Unlock()
	if s == nil || ref == 0 {
		return nil
	}
	body, err := s.variables(ctx, ref, c.threadID, "", 0, 0)
	if err != nil {
		return err
	}
	if len(body.Variables) != 1 {
		return nil
	}
	d := body.Variables[0]
	c.mu.Lock()
	c.setValueLocked(d.Value)
	c.setReferenceLocked(d.VariablesReference)
	c.namedVariables, c.indexedVariables, c.memoryReference = d.NamedVariables, d.IndexedVariables, d.MemoryReference
	c.hint, c.valueLocation = d.PresentationHint, d.ValueLocationReference
	c.mu.Unlock()
	s.m.emit(Event{Kind: EventVariables, Session: s})
	return nil
}

// variableSpec is what newVariable needs.
type variableSpec struct {
	ref                int
	name, evaluateName string
	value              string
	named, indexed     int
	memRef             string
	hint               *dap.VariablePresentationHint
	typ                string
	available          bool
	start              int
	dup                string
	declLoc, valueLoc  int
}

// Variable is a variable or a property: a child of a scope, of another variable or of an expression.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (Variable)
type Variable struct {
	container
	parent *container
	// Name is the name; empty for a message the model shows in place of children.
	Name string
	// EvaluateName is the expression that evaluates to the variable, or empty.
	EvaluateName string
	// Available is false for the placeholder of a failure or an invalid variable.
	Available bool
	// Start is, for a chunk, the index of its first element.
	Start int
	// DeclarationLocationReference is the adapter's reference of where the variable is declared.
	DeclarationLocationReference int

	errMu    sync.Mutex
	errorMsg string
}

func newVariable(s *Session, threadID int, parent *container, sp variableSpec) *Variable {
	v := &Variable{
		parent: parent, Name: sp.name, EvaluateName: sp.evaluateName, Available: sp.available, Start: sp.start,
		DeclarationLocationReference: sp.declLoc,
	}
	pid := ""
	if parent != nil {
		pid = parent.id
	}
	v.id = "variable:" + pid + ":" + sp.name + ":" + sp.dup
	v.threadID = threadID
	v.sess = s
	v.reference, v.namedVariables, v.indexedVariables, v.memoryReference = sp.ref, sp.named, sp.indexed, sp.memRef
	v.startOfVariables, v.hint, v.valueLocation, v.typ = sp.start, sp.hint, sp.valueLoc, sp.typ
	v.mu.Lock()
	v.setValueLocked(sp.value)
	v.mu.Unlock()
	return v
}

// ErrorMessage is the message of the adapter when the last SetValue failed.
func (v *Variable) ErrorMessage() string {
	v.errMu.Lock()
	defer v.errMu.Unlock()
	return v.errorMsg
}

// ThreadID is the thread the variable was fetched for.
func (v *Variable) ThreadID() int { return v.threadID }

// String is "name: value", or the value when there is no name.
func (v *Variable) String() string {
	if v.Name != "" {
		return v.Name + ": " + v.Value()
	}
	return v.Value()
}

// SetValue changes the value in the debuggee with setVariable (or setExpression for an adapter that has only that) and takes the
// answer over; a failure of the adapter is kept in ErrorMessage and returned.
func (v *Variable) SetValue(ctx context.Context, frame *StackFrame, value string) error {
	s := v.Session()
	if s == nil {
		return nil
	}
	caps := s.Capabilities()
	if caps.SupportsSetExpression && !caps.SupportsSetVariable && v.EvaluateName != "" {
		return v.SetExpression(ctx, frame, value)
	}
	parentRef := 0
	if v.parent != nil {
		parentRef = v.parent.Reference()
	}
	body, err := s.setVariable(ctx, parentRef, v.Name, value)
	if err != nil {
		v.setError(err.Error())
		return err
	}
	v.takeSet(s, body.Value, body.Type, body.VariablesReference, body.NamedVariables, body.IndexedVariables, body.MemoryReference, nil, body.ValueLocationReference)
	return nil
}

// SetExpression changes the value with setExpression, for the variable's EvaluateName.
func (v *Variable) SetExpression(ctx context.Context, frame *StackFrame, value string) error {
	s := v.Session()
	if s == nil || v.EvaluateName == "" || frame == nil {
		return nil
	}
	body, err := s.setExpression(ctx, frame.ID, v.EvaluateName, value)
	if err != nil {
		v.setError(err.Error())
		return err
	}
	v.takeSet(s, body.Value, body.Type, body.VariablesReference, body.NamedVariables, body.IndexedVariables, body.MemoryReference, body.PresentationHint, body.ValueLocationReference)
	return nil
}

func (v *Variable) setError(msg string) {
	v.errMu.Lock()
	v.errorMsg = msg
	v.errMu.Unlock()
}

func (v *Variable) takeSet(s *Session, value, typ string, ref, named, indexed int, memRef string, hint *dap.VariablePresentationHint, valueLoc int) {
	v.setError("")
	v.mu.Lock()
	v.setValueLocked(value)
	if typ != "" {
		v.typ = typ
	}
	v.setReferenceLocked(ref)
	v.namedVariables, v.indexedVariables, v.memoryReference, v.valueLocation = named, indexed, memRef, valueLoc
	if hint != nil {
		v.hint = hint
	}
	v.mu.Unlock()
	s.m.emit(Event{Kind: EventVariables, Session: s})
}

// Scope is a named group of variables of a stack frame (locals, arguments, globals, ...).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (Scope)
type Scope struct {
	container
	// Frame is the stack frame the scope belongs to.
	Frame *StackFrame
	// Name is the name of the scope ("Locals").
	Name string
	// Hint is the adapter's presentation hint (arguments, locals, registers, returnValue).
	Hint string
	// Expensive says that the scope is slow to fetch, so a view loads it only when it is opened.
	Expensive bool
	// Range is the part of the source the scope covers, nil when the adapter did not say.
	Range *Range
	// Err is the message of an adapter that failed to give the scopes; the scope is then a message, not a scope.
	Err string
}

// ChildrenLoaded tells whether the children of the scope have been loaded.
func (s *Scope) ChildrenLoaded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.children != nil
}

// Range is a range of a source, counted from 1.
type Range struct {
	Line, Column, EndLine, EndColumn int
}

// Contains reports whether the range contains the other.
func (r Range) Contains(o Range) bool {
	if o.Line < r.Line || o.EndLine > r.EndLine {
		return false
	}
	if o.Line == r.Line && o.Column < r.Column {
		return false
	}
	if o.EndLine == r.EndLine && o.EndColumn > r.EndColumn {
		return false
	}
	return true
}

// Expression is a watch expression, or an expression the Debug Console evaluates: a name, evaluated in a frame.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (Expression)
type Expression struct {
	container
	// Name is the expression's text.
	Name string

	avMu      sync.Mutex
	available bool
}

func newExpression(name, id string) *Expression {
	e := &Expression{Name: name}
	e.id = id
	if name != "" {
		e.value = NotAvailable
	}
	return e
}

// NewExpression makes an expression to evaluate with Evaluate (the Debug Console's input); watch expressions come from the Model.
func NewExpression(name string) *Expression { return newExpression(name, newID()) }

// Available tells whether the last evaluation worked.
func (e *Expression) Available() bool {
	e.avMu.Lock()
	defer e.avMu.Unlock()
	return e.available
}

// Evaluate evaluates the expression in a frame (or, for the context "repl", in the session alone) and keeps the answer. A failure
// of the adapter is the value of the expression, as in VS Code; the error is that failure, for the callers that care.
func (e *Expression) Evaluate(ctx context.Context, s *Session, frame *StackFrame, evalContext string, keepLazy bool) error {
	e.mu.Lock()
	had := e.value == NotAvailable
	e.mu.Unlock()
	ok, err := e.evaluateExpression(ctx, s, frame, evalContext, keepLazy)
	e.avMu.Lock()
	e.available = ok
	e.avMu.Unlock()
	if s != nil && (had || e.ValueChanged()) {
		s.m.emit(Event{Kind: EventWatchValue, Session: s, Expression: e})
	}
	return err
}

// evaluateExpression is VS Code's ExpressionContainer.evaluateExpression.
func (e *Expression) evaluateExpression(ctx context.Context, s *Session, frame *StackFrame, evalContext string, keepLazy bool) (bool, error) {
	if s == nil {
		e.mu.Lock()
		e.value = NotAvailable
		if evalContext == dap.ContextRepl {
			e.value = "Please start a debug session to evaluate expressions"
		}
		e.reference = 0
		e.mu.Unlock()
		return false, nil
	}
	e.mu.Lock()
	e.sess = s
	if frame == nil && evalContext != dap.ContextRepl {
		e.setValueLocked(NotAvailable)
		e.setReferenceLocked(0)
		e.mu.Unlock()
		return false, nil
	}
	e.mu.Unlock()
	frameID := 0
	if frame != nil {
		frameID = frame.ID
	}
	body, err := s.evaluate(ctx, e.Name, frameID, evalContext)
	if err != nil {
		e.mu.Lock()
		e.setValueLocked(err.Error())
		e.setReferenceLocked(0)
		e.memoryReference = ""
		e.mu.Unlock()
		return false, err
	}
	e.mu.Lock()
	e.setValueLocked(body.Result)
	e.setReferenceLocked(body.VariablesReference)
	e.namedVariables, e.indexedVariables, e.memoryReference = body.NamedVariables, body.IndexedVariables, body.MemoryReference
	if body.Type != "" {
		e.typ = body.Type
	}
	e.hint = body.PresentationHint
	e.valueLocation = body.ValueLocationReference
	lazy := !keepLazy && body.PresentationHint != nil && body.PresentationHint.Lazy
	e.mu.Unlock()
	if lazy {
		_ = e.EvaluateLazy(ctx) // a failure leaves the lazy value; expanding it shows the failure
	}
	return true, nil
}

// SetValue changes the value of the expression in the debuggee with setExpression.
func (e *Expression) SetValue(ctx context.Context, frame *StackFrame, value string) error {
	s := e.Session()
	if s == nil || frame == nil {
		return nil
	}
	body, err := s.setExpression(ctx, frame.ID, e.Name, value)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.setValueLocked(body.Value)
	if body.Type != "" {
		e.typ = body.Type
	}
	e.setReferenceLocked(body.VariablesReference)
	e.namedVariables, e.indexedVariables, e.memoryReference = body.NamedVariables, body.IndexedVariables, body.MemoryReference
	e.mu.Unlock()
	return nil
}

// String is the name and the value on two lines.
func (e *Expression) String() string { return e.Name + "\n" + e.Value() }

// compile-time checks that the three kinds are nodes.
var (
	_ Node = (*Variable)(nil)
	_ Node = (*Scope)(nil)
	_ Node = (*Expression)(nil)
)
