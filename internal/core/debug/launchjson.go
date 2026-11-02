package debug

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
	"github.com/rm4n0s/errors"
)

// LaunchFileName is the path of the launch file below a folder (SPEC §10.31, D41): VS Code's format in the folder of Emetos.
// ".vscode/launch.json" is not read (D12).
const LaunchFileName = ".emetos/launch.json"

// LaunchFilePath is the launch file of a folder.
func LaunchFilePath(folder string) string {
	return filepath.Join(folder, filepath.FromSlash(LaunchFileName))
}

// Severity says how bad a Problem is.
type Severity int

const (
	// SeverityError is a file that cannot be read as it was meant: a syntax error, a value of the wrong type.
	SeverityError Severity = iota + 1
	// SeverityWarning is a file that works but is probably wrong: a missing attribute, a name used twice.
	SeverityWarning
)

// Problem is something wrong in a launch file, with where it is. Step 25.3 shows them in the Problems view (owner launch.json).
type Problem struct {
	Severity Severity
	Message  string
	// Offset and Length are the place in the text in bytes; Offset is -1 when the launch comes from a setting, which has no text.
	Offset, Length int
	// Line and Column are the start of the place, counted from zero, the column in bytes; -1 without text.
	Line, Column int
	// Path is where in the JSON, like configurations[1].type.
	Path string
}

// LaunchFile is the content of a launch.json or of the launch setting: VS Code's version, configurations, compounds and inputs.
type LaunchFile struct {
	Version        string
	Configurations []Config
	Compounds      []Compound
	Inputs         []Input
}

// ParseLaunch reads the text of a launch.json (JSONC: comments and trailing commas are allowed). It is tolerant: it returns what
// it could read, and a Problem for each thing that is wrong, with its place.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugSchemas.ts (launchSchema), for the checks.
func ParseLaunch(text string) (LaunchFile, []Problem) {
	root, parseErrs := jsonc.ParseTree(text, jsonc.ParseOptions{})
	v := &launchValidator{text: text, root: root}
	for _, e := range parseErrs {
		v.add(SeverityError, e.Message(), e.Offset, e.Length, "")
	}
	if root == nil {
		if len(parseErrs) == 0 {
			v.add(SeverityError, "The launch file is empty", 0, 0, "")
		}
		return LaunchFile{}, v.problems
	}
	return v.file(jsonc.NodeValue(root)), v.problems
}

// LaunchFromSetting reads the value of the launch setting (the user's or the workspace's) as a launch file. The Problems have no
// place in a text (Offset is -1).
func LaunchFromSetting(value any) (LaunchFile, []Problem) {
	v := &launchValidator{}
	return v.file(value), v.problems
}

type launchValidator struct {
	text     string
	root     *jsonc.Node
	problems []Problem
}

// at is the place of a path in the text, or -1.
func (v *launchValidator) at(path jsonc.Path) (offset, length int) {
	if v.root == nil {
		return -1, 0
	}
	if len(path) == 0 {
		return v.root.Offset, v.root.Length
	}
	if n := jsonc.FindNodeAtLocation(v.root, path); n != nil {
		return n.Offset, n.Length
	}
	// A missing member is reported on its object.
	if n := jsonc.FindNodeAtLocation(v.root, path[:len(path)-1]); n != nil {
		return n.Offset, n.Length
	}
	return v.root.Offset, v.root.Length
}

func (v *launchValidator) report(sev Severity, msg string, path jsonc.Path) {
	off, length := v.at(path)
	v.add(sev, msg, off, length, pathString(path))
}

func (v *launchValidator) add(sev Severity, msg string, off, length int, path string) {
	p := Problem{Severity: sev, Message: msg, Offset: off, Length: length, Line: -1, Column: -1, Path: path}
	if off >= 0 {
		p.Line, p.Column = lineColumn(v.text, off)
	}
	v.problems = append(v.problems, p)
}

// lineColumn is the zero-based line and byte column of an offset.
func lineColumn(text string, off int) (line, col int) {
	if off > len(text) {
		off = len(text)
	}
	start := 0
	for i := 0; i < off; i++ {
		if text[i] == '\n' {
			line++
			start = i + 1
		}
	}
	return line, off - start
}

func pathString(path jsonc.Path) string {
	s := ""
	for _, seg := range path {
		switch t := seg.(type) {
		case string:
			if s != "" {
				s += "."
			}
			s += t
		case int:
			s += "[" + strconv.Itoa(t) + "]"
		}
	}
	return s
}

// requireString reads a string member of an object, reporting a wrong type; required adds a warning when it is missing.
func (v *launchValidator) requireString(obj map[string]any, key string, path jsonc.Path, required bool) (string, bool) {
	val, ok := obj[key]
	if !ok {
		if required {
			v.report(SeverityWarning, `Missing property "`+key+`".`, append(append(jsonc.Path{}, path...), key))
		}
		return "", false
	}
	s, isStr := val.(string)
	if !isStr {
		v.report(SeverityError, `Incorrect type. Expected "string".`, append(append(jsonc.Path{}, path...), key))
		return "", false
	}
	return s, true
}

func (v *launchValidator) file(value any) LaunchFile {
	var f LaunchFile
	obj, ok := value.(map[string]any)
	if !ok {
		v.report(SeverityError, `Incorrect type. Expected "object".`, nil)
		return f
	}
	f.Version, _ = v.requireString(obj, "version", nil, false)
	if raw, ok := obj["configurations"]; ok {
		list, isList := raw.([]any)
		if !isList {
			v.report(SeverityError, `Incorrect type. Expected "array".`, jsonc.Path{"configurations"})
		}
		names := map[string]bool{}
		for i, e := range list {
			path := jsonc.Path{"configurations", i}
			m, isObj := e.(map[string]any)
			if !isObj {
				v.report(SeverityError, `Incorrect type. Expected "object".`, path)
				continue
			}
			name, hasName := v.requireString(m, "name", path, true)
			v.requireString(m, "type", path, true)
			v.requireString(m, "request", path, true)
			if hasName {
				if names[name] {
					v.report(SeverityWarning, "Duplicate configuration name '"+name+"'; the later one is shown as '"+name+" (1)'.", append(append(jsonc.Path{}, path...), "name"))
				}
				names[name] = true
			}
			f.Configurations = append(f.Configurations, Config(m))
		}
	}
	if raw, ok := obj["compounds"]; ok {
		list, isList := raw.([]any)
		if !isList {
			v.report(SeverityError, `Incorrect type. Expected "array".`, jsonc.Path{"compounds"})
		}
		for i, e := range list {
			if c, ok := v.compound(e, jsonc.Path{"compounds", i}); ok {
				f.Compounds = append(f.Compounds, c)
			}
		}
	}
	if raw, ok := obj["inputs"]; ok {
		list, isList := raw.([]any)
		if !isList {
			v.report(SeverityError, `Incorrect type. Expected "array".`, jsonc.Path{"inputs"})
		}
		for i, e := range list {
			if in, ok := v.input(e, jsonc.Path{"inputs", i}); ok {
				f.Inputs = append(f.Inputs, in)
			}
		}
	}
	return f
}

func (v *launchValidator) compound(e any, path jsonc.Path) (Compound, bool) {
	m, ok := e.(map[string]any)
	if !ok {
		v.report(SeverityError, `Incorrect type. Expected "object".`, path)
		return Compound{}, false
	}
	var c Compound
	c.Name, _ = v.requireString(m, "name", path, true)
	c.StopAll, _ = m["stopAll"].(bool)
	c.PreLaunchTask = taskOf(m["preLaunchTask"])
	c.Presentation = presentationOf(m["presentation"])
	raw, has := m["configurations"]
	if !has {
		v.report(SeverityWarning, `Missing property "configurations".`, append(append(jsonc.Path{}, path...), "configurations"))
		return c, true
	}
	list, isList := raw.([]any)
	if !isList {
		v.report(SeverityError, `Incorrect type. Expected "array".`, append(append(jsonc.Path{}, path...), "configurations"))
		return c, true
	}
	for i, r := range list {
		rpath := append(append(jsonc.Path{}, path...), "configurations", i)
		switch t := r.(type) {
		case string:
			c.Configurations = append(c.Configurations, CompoundRef{Name: t})
		case map[string]any:
			name, _ := v.requireString(t, "name", rpath, true)
			folder, _ := v.requireString(t, "folder", rpath, false)
			c.Configurations = append(c.Configurations, CompoundRef{Name: name, Folder: folder})
		default:
			v.report(SeverityError, "Incorrect type. Expected a configuration name or an object with name and folder.", rpath)
		}
	}
	return c, true
}

func (v *launchValidator) input(e any, path jsonc.Path) (Input, bool) {
	m, ok := e.(map[string]any)
	if !ok {
		v.report(SeverityError, `Incorrect type. Expected "object".`, path)
		return Input{}, false
	}
	var in Input
	in.ID, _ = v.requireString(m, "id", path, true)
	in.Type, _ = v.requireString(m, "type", path, true)
	in.Description, _ = v.requireString(m, "description", path, false)
	in.Default, _ = v.requireString(m, "default", path, false)
	in.Password, _ = m["password"].(bool)
	switch in.Type {
	case "promptString":
		if _, ok := m["description"]; !ok {
			v.report(SeverityWarning, `Missing property "description".`, append(append(jsonc.Path{}, path...), "description"))
		}
	case "pickString":
		if _, ok := m["description"]; !ok {
			v.report(SeverityWarning, `Missing property "description".`, append(append(jsonc.Path{}, path...), "description"))
		}
		opts, has := m["options"]
		if !has {
			v.report(SeverityWarning, `Missing property "options".`, append(append(jsonc.Path{}, path...), "options"))
			break
		}
		list, isList := opts.([]any)
		if !isList {
			v.report(SeverityError, `Incorrect type. Expected "array".`, append(append(jsonc.Path{}, path...), "options"))
			break
		}
		for i, o := range list {
			opath := append(append(jsonc.Path{}, path...), "options", i)
			switch t := o.(type) {
			case string:
				in.Options = append(in.Options, InputOption{Value: t})
			case map[string]any:
				val, ok := t["value"].(string)
				if !ok {
					v.report(SeverityWarning, `Missing property "value".`, append(append(jsonc.Path{}, opath...), "value"))
				}
				label, _ := t["label"].(string)
				in.Options = append(in.Options, InputOption{Label: label, Value: val})
			default:
				v.report(SeverityError, "Incorrect type. Expected a string or an object with value and label.", opath)
			}
		}
	case "command":
		in.Command, _ = v.requireString(m, "command", path, true)
		in.Args = m["args"]
	case "":
		// Reported by requireString.
	default:
		v.report(SeverityError, "Input variable '"+in.ID+"' can only be of type 'promptString', 'pickString', or 'command'.", append(append(jsonc.Path{}, path...), "type"))
	}
	return in, true
}

// ReadLaunchFile reads <folder>/.emetos/launch.json. found is false when the file does not exist (that is not an error: a folder
// without a launch file has no configurations). It fails with the tag LaunchFileReadFailed.
func ReadLaunchFile(ctx context.Context, folder string) (f LaunchFile, problems []Problem, found bool, err error) {
	if err := ctx.Err(); err != nil {
		return LaunchFile{}, nil, false, errors.NewErr("LaunchFileReadFailed", err).SetMetadata("folder", folder)
	}
	data, rerr := os.ReadFile(LaunchFilePath(folder))
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return LaunchFile{}, nil, false, nil
		}
		return LaunchFile{}, nil, false, errors.NewErr("LaunchFileReadFailed", rerr).SetMetadata("folder", folder)
	}
	f, problems = ParseLaunch(string(data))
	return f, problems, true, nil
}
