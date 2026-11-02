package debug

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Config is a launch configuration: a JSON object whose values are the Go values of encoding/json (string, float64, bool, nil,
// []any and map[string]any). The attributes are the debugger's; the model reads name, type, request, presentation, the task
// attributes and noDebug.
type Config map[string]any

// Name is the name of the configuration, or empty.
func (c Config) Name() string { s, _ := c["name"].(string); return s }

// Type is the debug type of the configuration, or empty.
func (c Config) Type() string { s, _ := c["type"].(string); return s }

// Request is "launch" or "attach" (or what the file says).
func (c Config) Request() string { s, _ := c["request"].(string); return s }

// NoDebug reports whether the configuration runs without debugging.
func (c Config) NoDebug() bool { b, _ := c["noDebug"].(bool); return b }

// String is the string attribute key, or empty.
func (c Config) String(key string) string { s, _ := c[key].(string); return s }

// Clone is a deep copy of the configuration.
func (c Config) Clone() Config {
	if c == nil {
		return nil
	}
	return Config(cloneObject(c))
}

// Presentation is the presentation attribute of a configuration or compound: where it shows in the dropdown.
type Presentation struct {
	// Hidden hides the configuration from the dropdown and the command palette.
	Hidden bool
	// Group groups and sorts the configurations.
	Group string
	// Order sorts the configurations of a group; nil is "no order".
	Order *float64
}

// Presentation reads the presentation attribute; a configuration without one gives nil.
func (c Config) Presentation() *Presentation { return presentationOf(c["presentation"]) }

func presentationOf(v any) *Presentation {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	p := &Presentation{}
	p.Hidden, _ = m["hidden"].(bool)
	p.Group, _ = m["group"].(string)
	if n, ok := m["order"].(float64); ok {
		p.Order = &n
	}
	return p
}

// TaskRef names a task of the Tasks extension: preLaunchTask, postDebugTask, preRestartTask and postRestartTask are a label or an
// object that identifies the task by its type and properties.
type TaskRef struct {
	// Label is the label of the task when the attribute is a string.
	Label string
	// Identifier is the object when the attribute is one.
	Identifier map[string]any
}

// IsZero reports whether the reference names no task.
func (t TaskRef) IsZero() bool { return t.Label == "" && len(t.Identifier) == 0 }

// String is the label, or the identifier's type and label.
func (t TaskRef) String() string {
	if t.Label != "" {
		return t.Label
	}
	typ, _ := t.Identifier["type"].(string)
	lbl, _ := t.Identifier["label"].(string)
	return strings.TrimSpace(typ + " " + lbl)
}

// Task reads a task attribute (preLaunchTask, postDebugTask, preRestartTask, postRestartTask); a missing or empty one gives the
// zero TaskRef.
func (c Config) Task(key string) TaskRef { return taskOf(c[key]) }

func taskOf(v any) TaskRef {
	switch t := v.(type) {
	case string:
		return TaskRef{Label: t}
	case map[string]any:
		return TaskRef{Identifier: t}
	}
	return TaskRef{}
}

// CompoundRef is an entry of the configurations of a compound: a name, with the folder it is in when the entry is an object.
type CompoundRef struct {
	Name   string
	Folder string
}

// Compound starts several configurations together.
type Compound struct {
	Name string
	// StopAll makes the end of one session stop all the sessions of the compound.
	StopAll bool
	// PreLaunchTask runs before any configuration of the compound starts.
	PreLaunchTask TaskRef
	// Configurations are the entries to start.
	Configurations []CompoundRef
	Presentation   *Presentation
}

// Input is an entry of the inputs of a launch file (${input:id}).
type Input struct {
	ID   string
	Type string // promptString, pickString, command
	// Description is the prompt or the placeholder.
	Description string
	Default     string
	Password    bool
	Options     []InputOption
	// Command and Args are the command of a command input.
	Command string
	Args    any
}

// InputOption is an option of a pickString input.
type InputOption struct {
	Label string
	Value string
}

// effectiveForPlatform applies the platform attributes (linux, as Emetos runs on Linux only) over the configuration and drops
// the attributes of the other platforms.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugUtils.ts (getEffectiveConfigForPlatform)
func effectiveForPlatform(c Config) Config {
	out := c.Clone()
	if lin, ok := c["linux"].(map[string]any); ok {
		for k, v := range lin {
			if k == "presentation" {
				continue
			}
			out[k] = cloneValue(v)
		}
		if lp := presentationOf(lin["presentation"]); lp != nil {
			merged := map[string]any{}
			if base, ok := c["presentation"].(map[string]any); ok {
				for k, v := range base {
					merged[k] = v
				}
			}
			if pm, ok := lin["presentation"].(map[string]any); ok {
				for k, v := range pm {
					merged[k] = v
				}
			}
			out["presentation"] = cloneValue(merged)
		}
	}
	return out
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneObject(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneValue(e)
		}
		return out
	}
	return v
}

func cloneObject(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

// jsonEqual compares two configurations by value.
func jsonEqual(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(x) == string(y)
}

// visibleAndSorted drops the hidden items and orders the rest by group and order, as VS Code does; the sort is stable so the
// order of the file decides among equals.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugUtils.ts (getVisibleAndSorted)
func visibleAndSorted[T any](items []T, presentation func(T) *Presentation) []T {
	visible := make([]T, 0, len(items))
	for _, it := range items {
		if p := presentation(it); p != nil && p.Hidden {
			continue
		}
		visible = append(visible, it)
	}
	sort.SliceStable(visible, func(i, j int) bool {
		return comparePresentation(presentation(visible[i]), presentation(visible[j])) < 0
	})
	return visible
}

func comparePresentation(a, b *Presentation) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return 1
	}
	if b == nil {
		return -1
	}
	if a.Group == "" {
		if b.Group == "" {
			return compareOrders(a.Order, b.Order)
		}
		return 1
	}
	if b.Group == "" {
		return -1
	}
	if a.Group != b.Group {
		return strings.Compare(a.Group, b.Group)
	}
	return compareOrders(a.Order, b.Order)
}

func compareOrders(a, b *float64) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case *a < *b:
		return -1
	case *a > *b:
		return 1
	}
	return 0
}

// distinguishByName gives the second and later entries of a name the suffix " (1)", " (2)", ... so that names are unique.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugConfigurationManager.ts (distinguishConfigsByName)
func distinguishByName(names []string) []string {
	seen := map[string]int{}
	out := make([]string, len(names))
	for i, n := range names {
		no := seen[n]
		seen[n] = no + 1
		if no == 0 {
			out[i] = n
		} else {
			out[i] = n + " (" + strconv.Itoa(no) + ")"
		}
	}
	return out
}
