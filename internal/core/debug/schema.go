package debug

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

// DebuggerSchema is what the checks and the completion of launch.json need to know of a debug type: the JSON schemas of its attributes
// (the debugger's configurationAttributes) and the configurations it suggests. The extensions' manifests (Contributes.Debuggers) are
// turned into these.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugger.ts (the schema built from the contribution) and
// src/vs/workbench/contrib/debug/common/debugSchemas.ts (launchSchema)
type DebuggerSchema struct {
	// Type is the debug type.
	Type string
	// Label is its name for the user.
	Label string
	// Attributes are the JSON schemas of the attributes of a configuration, by request ("launch" and "attach"). The schema is an
	// object schema with properties, and may have required, additionalProperties, description and the members described at
	// CheckAttributes.
	Attributes map[string]map[string]any
	// Snippets are the configurations that a new item of "configurations" is offered.
	Snippets []Snippet
}

// Snippet is a configuration of a debugger that "Add Configuration" and the completion of launch.json offer; the strings of Body may
// use the snippet syntax (${1:name}).
type Snippet struct {
	Label       string
	Description string
	Body        Config
}

// commonAttributes are the attributes every configuration may have, whatever its debugger (VS Code's launchSchema and the attributes
// the model reads).
var commonAttributes = map[string]any{
	"type":    map[string]any{"type": "string", "description": "Type of configuration."},
	"request": map[string]any{"type": "string", "enum": []any{"launch", "attach"}, "description": "Request type of configuration. Can be \"launch\" or \"attach\"."},
	"name": map[string]any{"type": "string", "description": "Name of configuration; appears in the launch configuration dropdown menu.",
		"default": "Launch"},
	"presentation": map[string]any{"type": "object", "description": "Presentation options on how to show this configuration in the debug configuration dropdown and the command palette.",
		"properties": map[string]any{
			"hidden": map[string]any{"type": "boolean", "default": false, "description": "Controls if this configuration should be shown in the configuration dropdown and the command palette."},
			"group":  map[string]any{"type": "string", "default": "", "description": "Group that this configuration belongs to. Used for grouping and sorting in the configuration dropdown and the command palette."},
			"order":  map[string]any{"type": "number", "default": 1, "description": "Order of this configuration within a group. Used for grouping and sorting in the configuration dropdown and the command palette."},
		}},
	"preLaunchTask":                  map[string]any{"type": []any{"string", "object"}, "description": "Task to run before debug session starts."},
	"postDebugTask":                  map[string]any{"type": []any{"string", "object"}, "description": "Task to run after debug session ends."},
	"preRestartTask":                 map[string]any{"type": []any{"string", "object"}, "description": "Task to run before the debug session restarts."},
	"postRestartTask":                map[string]any{"type": []any{"string", "object"}, "description": "Task to run after the debug session restarts."},
	"internalConsoleOptions":         map[string]any{"type": "string", "enum": []any{"neverOpen", "openOnSessionStart", "openOnFirstSessionStart"}, "default": "openOnFirstSessionStart", "description": "Controls when the internal Debug Console should open."},
	"suppressMultipleSessionWarning": map[string]any{"type": "boolean", "default": false, "description": "Disable the warning when trying to start the same debug configuration more than once."},
	"noDebug":                        map[string]any{"type": "boolean", "description": "Run without debugging."},
	"debugServer":                    map[string]any{"type": "number", "description": "For debug extension development only: if a port is specified, the debug adapter is not started and a connection is attempted to a server running on the port."},
	"linux":                          map[string]any{"type": "object", "description": "Linux specific attributes."},
	"windows":                        map[string]any{"type": "object", "description": "Windows specific attributes."},
	"osx":                            map[string]any{"type": "object", "description": "macOS specific attributes."},
}

// effectiveSchema is the schema of a configuration: the schema of the debugger's request with the common attributes added.
func effectiveSchema(schema map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range schema {
		out[k] = v
	}
	props := map[string]any{}
	for k, v := range commonAttributes {
		props[k] = v
	}
	if own, ok := schema["properties"].(map[string]any); ok {
		for k, v := range own {
			props[k] = v
		}
	}
	out["properties"] = props
	return out
}

func byType(debuggers []DebuggerSchema) map[string]DebuggerSchema {
	m := make(map[string]DebuggerSchema, len(debuggers))
	for _, d := range debuggers {
		m[d.Type] = d
	}
	return m
}

func typeNames(debuggers []DebuggerSchema) []string {
	var out []string
	for _, d := range debuggers {
		out = append(out, d.Type)
	}
	sort.Strings(out)
	return out
}

func quoteList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = `"` + s + `"`
	}
	return strings.Join(q, ", ")
}

// ---- checking

// CheckAttributes checks the configurations of a launch.json against the attribute schemas of their debuggers: a type no debugger
// has, a request the debugger does not support, a required attribute that is missing, an attribute of the wrong type, a value that
// the enum of the schema does not have, and an attribute the schema does not allow (when it says additionalProperties false). What
// ParseLaunch reports (a missing name, type or request, the syntax) is not repeated. The problems are warnings, as the JSON language
// service of VS Code shows schema violations.
//
// The schemas understand type (one or several), enum, const, properties, required, additionalProperties (false or a schema), items,
// allOf, anyOf, oneOf, minimum, maximum and pattern. A string that holds a variable (${...}) is accepted where another type is
// expected: the variable is substituted before the debugger reads it.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugSchemas.ts (the schema of launch.json), for the checks
func CheckAttributes(text string, debuggers []DebuggerSchema) []Problem {
	root, _ := jsonc.ParseTree(text, jsonc.ParseOptions{})
	if root == nil || root.Type != jsonc.ObjectNode {
		return nil
	}
	list := jsonc.FindNodeAtLocation(root, jsonc.Path{"configurations"})
	if list == nil || list.Type != jsonc.ArrayNode {
		return nil
	}
	c := &schemaChecker{v: &launchValidator{text: text}, debuggers: byType(debuggers), all: debuggers}
	for i, n := range list.Children {
		if n.Type == jsonc.ObjectNode {
			c.config(n, jsonc.Path{"configurations", i})
		}
	}
	return c.v.problems
}

type schemaChecker struct {
	v         *launchValidator
	debuggers map[string]DebuggerSchema
	all       []DebuggerSchema
}

func (c *schemaChecker) warn(msg string, off, length int, path jsonc.Path) {
	c.v.add(SeverityWarning, msg, off, length, pathString(path))
}

// members maps the names of an object node to its property nodes.
func members(obj *jsonc.Node) map[string]*jsonc.Node {
	out := map[string]*jsonc.Node{}
	for _, p := range obj.Children {
		if p.Type == jsonc.PropertyNode && len(p.Children) > 0 {
			if name, ok := p.Children[0].Value.(string); ok {
				out[name] = p
			}
		}
	}
	return out
}

func valueOf(p *jsonc.Node) *jsonc.Node {
	if p == nil || len(p.Children) < 2 {
		return nil
	}
	return p.Children[1]
}

func (c *schemaChecker) config(n *jsonc.Node, path jsonc.Path) {
	props := members(n)
	typeNode := valueOf(props["type"])
	if typeNode == nil || typeNode.Type != jsonc.StringNode {
		return
	}
	t, _ := typeNode.Value.(string)
	if t == "" {
		return
	}
	dbg, ok := c.debuggers[t]
	if !ok {
		msg := fmt.Sprintf("Unknown debug type '%s'.", t)
		if names := typeNames(c.all); len(names) > 0 {
			msg += " The debuggers are: " + strings.Join(names, ", ") + "."
		}
		c.warn(msg, typeNode.Offset, typeNode.Length, append(append(jsonc.Path{}, path...), "type"))
		return
	}
	reqNode := valueOf(props["request"])
	if reqNode == nil || reqNode.Type != jsonc.StringNode || len(dbg.Attributes) == 0 {
		return
	}
	req, _ := reqNode.Value.(string)
	schema, has := dbg.Attributes[req]
	if !has {
		var reqs []string
		for r := range dbg.Attributes {
			reqs = append(reqs, r)
		}
		sort.Strings(reqs)
		c.warn(fmt.Sprintf("The debugger '%s' does not support the request '%s'. Use one of: %s.", t, req, strings.Join(reqs, ", ")),
			reqNode.Offset, reqNode.Length, append(append(jsonc.Path{}, path...), "request"))
		return
	}
	c.validate(effectiveSchema(schema), n, path)
}

// validate checks a node against a schema and reports each violation; it returns how many it reported.
func (c *schemaChecker) validate(schema map[string]any, n *jsonc.Node, path jsonc.Path) int {
	before := len(c.v.problems)
	if t, ok := schema["type"]; ok {
		if !c.typeMatches(t, n) {
			c.warn(typeMessage(t), n.Offset, n.Length, path)
			return len(c.v.problems) - before
		}
	}
	if lit, ok := literalOf(n); ok {
		if enum, ok := schema["enum"].([]any); ok && !containsValue(enum, lit) {
			c.warn("Value is not accepted. Valid values: "+valueList(enum)+".", n.Offset, n.Length, path)
		}
		if k, ok := schema["const"]; ok && !jsonEqual(k, lit) {
			c.warn("Value must be "+valueText(k)+".", n.Offset, n.Length, path)
		}
	}
	switch n.Type {
	case jsonc.NumberNode:
		f, _ := n.Value.(float64)
		if m, ok := schema["minimum"].(float64); ok && f < m {
			c.warn(fmt.Sprintf("Value is below the minimum of %v.", m), n.Offset, n.Length, path)
		}
		if m, ok := schema["maximum"].(float64); ok && f > m {
			c.warn(fmt.Sprintf("Value is above the maximum of %v.", m), n.Offset, n.Length, path)
		}
	case jsonc.StringNode:
		if p, ok := schema["pattern"].(string); ok {
			s, _ := n.Value.(string)
			if re, err := regexp.Compile(p); err == nil && !re.MatchString(s) && !strings.Contains(s, "${") {
				c.warn(fmt.Sprintf("String does not match the pattern of \"%s\".", p), n.Offset, n.Length, path)
			}
		}
	case jsonc.ObjectNode:
		c.object(schema, n, path)
	case jsonc.ArrayNode:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range n.Children {
				c.validate(items, e, append(append(jsonc.Path{}, path...), i))
			}
		}
	}
	if all, ok := schema["allOf"].([]any); ok {
		for _, s := range all {
			if sub, ok := s.(map[string]any); ok {
				c.validate(sub, n, path)
			}
		}
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		alts, ok := schema[key].([]any)
		if !ok || len(alts) == 0 {
			continue
		}
		best, bestProblems, matched := -1, []Problem(nil), false
		for _, a := range alts {
			sub, ok := a.(map[string]any)
			if !ok {
				continue
			}
			mark := len(c.v.problems)
			count := c.validate(sub, n, path)
			found := append([]Problem(nil), c.v.problems[mark:]...)
			c.v.problems = c.v.problems[:mark]
			if count == 0 {
				matched = true
				break
			}
			// the alternative whose type fits is the closest one, whatever else is wrong with the value
			cost := count
			for _, p := range found {
				if p.Path == pathString(path) && strings.HasPrefix(p.Message, "Incorrect type") {
					cost += 100
				}
			}
			if best < 0 || cost < best {
				best, bestProblems = cost, found
			}
		}
		if !matched {
			c.v.problems = append(c.v.problems, bestProblems...)
		}
	}
	return len(c.v.problems) - before
}

func (c *schemaChecker) object(schema map[string]any, n *jsonc.Node, path jsonc.Path) {
	props := members(n)
	declared, _ := schema["properties"].(map[string]any)
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			name, _ := r.(string)
			if _, has := props[name]; name != "" && !has {
				c.warn(fmt.Sprintf("Missing property \"%s\".", name), n.Offset, 1, path)
			}
		}
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := props[name]
		val := valueOf(p)
		sub, isDeclared := declared[name].(map[string]any)
		child := append(append(jsonc.Path{}, path...), name)
		switch {
		case isDeclared && val != nil:
			c.validate(sub, val, child)
		case !isDeclared:
			switch ap := schema["additionalProperties"].(type) {
			case bool:
				if !ap && !isCommonOnly(name) {
					key := p.Children[0]
					c.warn(fmt.Sprintf("Property %s is not allowed.", name), key.Offset, key.Length, child)
				}
			case map[string]any:
				if val != nil {
					c.validate(ap, val, child)
				}
			}
		}
	}
}

// isCommonOnly tells whether a name is a common attribute, which a debugger's additionalProperties false does not forbid.
func isCommonOnly(name string) bool { _, ok := commonAttributes[name]; return ok }

func (c *schemaChecker) typeMatches(t any, n *jsonc.Node) bool {
	var want []string
	switch tt := t.(type) {
	case string:
		want = []string{tt}
	case []any:
		for _, x := range tt {
			if s, ok := x.(string); ok {
				want = append(want, s)
			}
		}
	}
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		switch w {
		case "string":
			if n.Type == jsonc.StringNode {
				return true
			}
		case "number":
			if n.Type == jsonc.NumberNode {
				return true
			}
		case "integer":
			if f, ok := n.Value.(float64); ok && n.Type == jsonc.NumberNode && f == float64(int64(f)) {
				return true
			}
		case "boolean":
			if n.Type == jsonc.BooleanNode {
				return true
			}
		case "object":
			if n.Type == jsonc.ObjectNode {
				return true
			}
		case "array":
			if n.Type == jsonc.ArrayNode {
				return true
			}
		case "null":
			if n.Type == jsonc.NullNode {
				return true
			}
		default:
			return true
		}
	}
	// a variable stands for a value of any type
	if s, ok := n.Value.(string); ok && n.Type == jsonc.StringNode && strings.Contains(s, "${") {
		return true
	}
	return false
}

func typeMessage(t any) string {
	switch tt := t.(type) {
	case string:
		return `Incorrect type. Expected "` + tt + `".`
	case []any:
		var names []string
		for _, x := range tt {
			if s, ok := x.(string); ok {
				names = append(names, s)
			}
		}
		return "Incorrect type. Expected one of " + quoteList(names) + "."
	}
	return "Incorrect type."
}

func literalOf(n *jsonc.Node) (any, bool) {
	switch n.Type {
	case jsonc.StringNode, jsonc.NumberNode, jsonc.BooleanNode:
		return n.Value, true
	case jsonc.NullNode:
		return nil, true
	}
	return nil, false
}

func containsValue(list []any, v any) bool {
	for _, x := range list {
		if jsonEqual(x, v) {
			return true
		}
	}
	return false
}

func valueText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func valueList(list []any) string {
	parts := make([]string, len(list))
	for i, v := range list {
		parts[i] = valueText(v)
	}
	return strings.Join(parts, ", ")
}

// ---- completion

// CompletionKind says what a LaunchCompletion offers.
type CompletionKind string

// The kinds of a LaunchCompletion.
const (
	// CompleteProperty is the name of an attribute.
	CompleteProperty CompletionKind = "property"
	// CompleteValue is a value: an enum member, a boolean, a default, a debug type or a request.
	CompleteValue CompletionKind = "value"
	// CompleteSnippet is a whole configuration of a debugger.
	CompleteSnippet CompletionKind = "snippet"
)

// LaunchCompletion is a suggestion of the completion of launch.json.
type LaunchCompletion struct {
	Label         string
	Detail        string
	Documentation string
	Kind          CompletionKind
	// InsertText replaces the bytes from Start to End of the text; with Snippet it uses the snippet syntax.
	InsertText string
	Snippet    bool
	Start, End int
}

// CompleteLaunch suggests what can be typed at an offset of a launch.json: the keys of the file, the debug types and the requests,
// the attributes of the debugger of a configuration with their defaults, the values an attribute accepts (its enum, true and false,
// its default), and, where a configuration may start, the snippets of the debuggers. It is the JSON language's completion fed with
// the schemas the debuggers contribute.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugSchemas.ts and the JSON language service (the completion from a
// schema), for the same suggestions
func CompleteLaunch(text string, offset int, debuggers []DebuggerSchema) []LaunchCompletion {
	if offset < 0 || offset > len(text) {
		return nil
	}
	root, _ := jsonc.ParseTree(text, jsonc.ParseOptions{})
	loc := jsonc.GetLocation(text, offset)
	start, end := offset, offset
	if n := jsonc.FindNodeAtOffset(rootOrEmpty(root), offset, true); n != nil && n.Type == jsonc.StringNode {
		start, end = n.Offset, n.End()
	}
	cx := &completer{text: text, root: root, debuggers: debuggers, by: byType(debuggers), start: start, end: end}
	path := loc.Path
	if len(path) == 0 {
		return nil
	}
	if s, ok := path[0].(string); ok && s != "configurations" {
		if len(path) == 1 && loc.IsAtPropertyKey {
			return cx.fileKeys(root)
		}
		return nil
	}
	if len(path) == 1 && path[0] == "" {
		return cx.fileKeys(root)
	}
	if len(path) < 2 || path[0] != "configurations" {
		return nil
	}
	index, isIndex := path[1].(int)
	if !isIndex {
		return nil
	}
	if len(path) == 2 {
		return cx.snippets()
	}
	return cx.inConfig(index, path, loc.IsAtPropertyKey)
}

func rootOrEmpty(n *jsonc.Node) *jsonc.Node {
	if n == nil {
		return &jsonc.Node{Type: jsonc.ObjectNode, Offset: 0, Length: 0, ColonOffset: -1}
	}
	return n
}

type completer struct {
	text       string
	root       *jsonc.Node
	debuggers  []DebuggerSchema
	by         map[string]DebuggerSchema
	start, end int
}

func (cx *completer) fileKeys(root *jsonc.Node) []LaunchCompletion {
	have := map[string]bool{}
	if root != nil && root.Type == jsonc.ObjectNode {
		for name := range members(root) {
			have[name] = true
		}
	}
	var out []LaunchCompletion
	for _, k := range []struct{ name, snippet, doc string }{
		{"version", `"version": "${1:0.2.0}"`, "Version of this file format."},
		{"configurations", `"configurations": [$0]`, "List of configurations. Add new configurations or edit existing ones by using IntelliSense."},
		{"compounds", `"compounds": [$0]`, "List of compounds. Each compound references multiple configurations which will get launched together."},
		{"inputs", `"inputs": [$0]`, "List of inputs that can be referenced by ${input:id} in a configuration."},
	} {
		if !have[k.name] {
			out = append(out, LaunchCompletion{Label: k.name, Documentation: k.doc, Kind: CompleteProperty, InsertText: k.snippet, Snippet: true, Start: cx.start, End: cx.end})
		}
	}
	return out
}

// snippets are the configurations of the debuggers, offered where a new item of "configurations" starts.
func (cx *completer) snippets() []LaunchCompletion {
	var out []LaunchCompletion
	for _, d := range cx.debuggers {
		for _, s := range d.Snippets {
			out = append(out, LaunchCompletion{
				Label: s.Label, Detail: d.Label, Documentation: s.Description, Kind: CompleteSnippet,
				InsertText: snippetJSON(s.Body), Snippet: true, Start: cx.start, End: cx.end,
			})
		}
	}
	return out
}

// snippetJSON writes a configuration as the text of a snippet: name, type and request first.
func snippetJSON(body Config) string {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	keys := make([]string, 0, len(body))
	for k := range body {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, rj := rank(keys[i]), rank(keys[j])
		if ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	for i, k := range keys {
		var v bytes.Buffer
		enc := json.NewEncoder(&v)
		enc.SetEscapeHTML(false)
		enc.SetIndent("\t", "\t")
		var raws []string
		if err := enc.Encode(rawStrings(body[k], &raws)); err != nil {
			continue
		}
		text := strings.TrimRight(v.String(), "\n")
		for n, inner := range raws {
			text = strings.Replace(text, `"`+rawToken(n)+`"`, `"`+inner+`"`, 1)
		}
		name, _ := json.Marshal(k)
		buf.WriteString("\t" + string(name) + ": " + text)
		if i < len(keys)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}")
	return buf.String()
}

// rawToken is the text that stands for the n-th raw string of a snippet while the value is encoded.
func rawToken(n int) string { return fmt.Sprintf("@@raw%d@@", n) }

// rawStrings replaces the strings of v that are written `^"text"` by tokens and appends their text (the part between the quotes) to
// raws: VS Code's convention for the snippets of debuggers, which writes the text in the snippet's own syntax and not as a JSON string,
// so that `\${workspaceFolder}` inserts the characters ${workspaceFolder} and not the variable of the snippet.
//
// (The snippets of js-debug's package.json use it, and VS Code's own debuggers read them so.)
func rawStrings(v any, raws *[]string) any {
	switch x := v.(type) {
	case string:
		if len(x) >= 3 && strings.HasPrefix(x, `^"`) && strings.HasSuffix(x, `"`) {
			*raws = append(*raws, x[2:len(x)-1])
			return rawToken(len(*raws) - 1)
		}
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = rawStrings(e, raws)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = rawStrings(e, raws)
		}
		return out
	}
	return v
}

func rank(k string) int {
	switch k {
	case "name":
		return 0
	case "type":
		return 1
	case "request":
		return 2
	}
	return 3
}

// inConfig completes inside the object of configurations[index]; path is the location's path and atKey whether a name is expected.
func (cx *completer) inConfig(index int, path jsonc.Path, atKey bool) []LaunchCompletion {
	cfg := jsonc.FindNodeAtLocation(cx.root, jsonc.Path{"configurations", index})
	var props map[string]*jsonc.Node
	typ, req := "", ""
	if cfg != nil && cfg.Type == jsonc.ObjectNode {
		props = members(cfg)
		if v := valueOf(props["type"]); v != nil {
			typ, _ = v.Value.(string)
		}
		if v := valueOf(props["request"]); v != nil {
			req, _ = v.Value.(string)
		}
	}
	dbg, known := cx.by[typ]
	var schema map[string]any
	if known {
		r := req
		if r == "" || dbg.Attributes[r] == nil {
			r = "launch"
		}
		if s := dbg.Attributes[r]; s != nil {
			schema = effectiveSchema(s)
		}
	}
	if schema == nil {
		schema = map[string]any{"properties": commonAttributes}
	}
	rest := path[2:]
	if atKey {
		// the names of the object that contains the cursor
		sub := schemaAt(schema, rest[:len(rest)-1])
		have := map[string]bool{}
		if len(rest) == 1 {
			for name := range props {
				have[name] = true
			}
		} else if obj := jsonc.FindNodeAtLocation(cx.root, append(jsonc.Path{"configurations", index}, rest[:len(rest)-1]...)); obj != nil && obj.Type == jsonc.ObjectNode {
			for name := range members(obj) {
				have[name] = true
			}
		}
		return cx.propertyNames(sub, have, len(rest) == 1)
	}
	// a value: the attribute is the last name of the path
	name, _ := rest[len(rest)-1].(string)
	sub := schemaAt(schema, rest)
	if len(rest) == 1 {
		switch name {
		case "type":
			return cx.typeValues()
		case "request":
			return cx.requestValues(dbg, known)
		}
	}
	return cx.values(sub)
}

// schemaAt follows a path of property names and array indices through a schema; it returns nil when the schema does not say.
func schemaAt(schema map[string]any, path jsonc.Path) map[string]any {
	cur := schema
	for _, seg := range path {
		if cur == nil {
			return nil
		}
		switch s := seg.(type) {
		case string:
			props, _ := cur["properties"].(map[string]any)
			next, _ := props[s].(map[string]any)
			cur = next
		case int:
			next, _ := cur["items"].(map[string]any)
			cur = next
		}
	}
	return cur
}

func (cx *completer) propertyNames(schema map[string]any, have map[string]bool, top bool) []LaunchCompletion {
	props, _ := schema["properties"].(map[string]any)
	names := make([]string, 0, len(props))
	for name := range props {
		if !have[name] {
			names = append(names, name)
		}
	}
	// the debugger's own attributes come first, in alphabetical order, then the common ones
	sort.Slice(names, func(i, j int) bool {
		ci, cj := isCommonOnly(names[i]) && top, isCommonOnly(names[j]) && top
		if ci != cj {
			return !ci
		}
		return names[i] < names[j]
	})
	colon := cx.colonFollows()
	out := make([]LaunchCompletion, 0, len(names))
	for _, name := range names {
		sub, _ := props[name].(map[string]any)
		item := LaunchCompletion{Label: name, Kind: CompleteProperty, Start: cx.start, End: cx.end, Documentation: describe(sub), Detail: typeDetail(sub)}
		key, _ := json.Marshal(name)
		if colon {
			item.InsertText = string(key)
		} else {
			item.InsertText = string(key) + ": " + valueSnippet(sub)
			item.Snippet = true
		}
		out = append(out, item)
	}
	return out
}

// colonFollows tells whether the name being completed already has its colon.
func (cx *completer) colonFollows() bool {
	rest := strings.TrimLeft(cx.text[cx.end:], " \t")
	return strings.HasPrefix(rest, ":")
}

func (cx *completer) typeValues() []LaunchCompletion {
	var out []LaunchCompletion
	for _, d := range cx.debuggers {
		doc := ""
		if d.Label != "" && d.Label != d.Type {
			doc = d.Label
		}
		q, _ := json.Marshal(d.Type)
		out = append(out, LaunchCompletion{Label: d.Type, Detail: doc, Kind: CompleteValue, InsertText: string(q), Start: cx.start, End: cx.end})
	}
	return out
}

func (cx *completer) requestValues(d DebuggerSchema, known bool) []LaunchCompletion {
	reqs := []string{"launch", "attach"}
	if known && len(d.Attributes) > 0 {
		reqs = reqs[:0]
		for r := range d.Attributes {
			reqs = append(reqs, r)
		}
		sort.Strings(reqs)
	}
	var out []LaunchCompletion
	for _, r := range reqs {
		q, _ := json.Marshal(r)
		out = append(out, LaunchCompletion{Label: r, Kind: CompleteValue, InsertText: string(q), Start: cx.start, End: cx.end})
	}
	return out
}

// values are the values an attribute with the schema accepts.
func (cx *completer) values(schema map[string]any) []LaunchCompletion {
	if schema == nil {
		return nil
	}
	var out []LaunchCompletion
	add := func(v any, detail string) {
		text := valueText(v)
		out = append(out, LaunchCompletion{Label: strings.Trim(text, `"`), Detail: detail, Kind: CompleteValue, InsertText: text, Start: cx.start, End: cx.end})
	}
	if enum, ok := schema["enum"].([]any); ok {
		for _, v := range enum {
			add(v, "")
		}
		return out
	}
	if def, ok := schema["default"]; ok {
		add(def, "default")
	}
	if typeIncludes(schema["type"], "boolean") {
		if def, ok := schema["default"].(bool); !ok || !def {
			add(true, "")
		}
		if def, ok := schema["default"].(bool); !ok || def {
			add(false, "")
		}
	}
	return out
}

func typeIncludes(t any, want string) bool {
	switch tt := t.(type) {
	case string:
		return tt == want
	case []any:
		for _, x := range tt {
			if s, ok := x.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

func describe(schema map[string]any) string {
	if s, ok := schema["markdownDescription"].(string); ok {
		return s
	}
	s, _ := schema["description"].(string)
	return s
}

func typeDetail(schema map[string]any) string {
	switch t := schema["type"].(type) {
	case string:
		return t
	case []any:
		var names []string
		for _, x := range t {
			if s, ok := x.(string); ok {
				names = append(names, s)
			}
		}
		return strings.Join(names, " | ")
	}
	return ""
}

// snippetEscape escapes the characters of a text that have a meaning in the snippet syntax.
func snippetEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `$`, `\$`, `}`, `\}`).Replace(s)
}

// valueSnippet is the text, in the snippet syntax, that follows "name": for an attribute with the schema: its default when it has
// one, else a placeholder of its type.
func valueSnippet(schema map[string]any) string {
	if def, ok := schema["default"]; ok {
		switch d := def.(type) {
		case string:
			return `"${1:` + snippetEscape(strings.ReplaceAll(strings.ReplaceAll(d, `\`, `\\`), `"`, `\"`)) + `}"`
		default:
			return "${1:" + snippetEscape(valueText(def)) + "}"
		}
	}
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		allSimple := true
		var choices []string
		for _, v := range enum {
			s, isStr := v.(string)
			if !isStr || strings.ContainsAny(s, ",|\"\\$}") {
				allSimple = false
				break
			}
			choices = append(choices, s)
		}
		if allSimple {
			return `"${1|` + strings.Join(choices, ",") + `|}"`
		}
	}
	switch {
	case typeIncludes(schema["type"], "boolean"):
		return "${1|true,false|}"
	case typeIncludes(schema["type"], "number"), typeIncludes(schema["type"], "integer"):
		return "${1:0}"
	case typeIncludes(schema["type"], "array"):
		return "[$1]"
	case typeIncludes(schema["type"], "object"):
		return "{$1}"
	}
	return `"$1"`
}
