package debug

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// Replacement is one ${name} or ${name:arg} found in a text.
//
// Ported from VS Code: src/vs/platform/configuration/common/configurationVariables.ts
type Replacement struct {
	// ID is the whole text, ${name:arg}.
	ID string
	// Inner is name:arg.
	Inner string
	// Name is the part before the first colon.
	Name string
	// Arg is the part after it; HasArg tells an empty one from none.
	Arg    string
	HasArg bool
}

// parseVariable reads the variable that starts at start (the "$" of "${"), counting nested braces. It reports false when the
// braces do not close.
func parseVariable(value string, start int) (r Replacement, end int, ok bool) {
	if start+1 >= len(value) || value[start] != '$' || value[start+1] != '{' {
		return Replacement{}, 0, false
	}
	end = start + 2
	depth := 1
	for end < len(value) {
		switch value[end] {
		case '{':
			depth++
		case '}':
			depth--
		}
		if depth == 0 {
			break
		}
		end++
	}
	if depth != 0 {
		return Replacement{}, 0, false
	}
	r.ID = value[start : end+1]
	r.Inner = value[start+2 : end]
	if i := strings.IndexByte(r.Inner, ':'); i >= 0 {
		r.Name, r.Arg, r.HasArg = r.Inner[:i], r.Inner[i+1:], true
	} else {
		r.Name = r.Inner
	}
	return r, end, true
}

// Variables lists the variables of a text in the order they appear, each once.
func Variables(value string) []Replacement {
	var out []Replacement
	seen := map[string]bool{}
	for pos := 0; pos < len(value); {
		i := strings.Index(value[pos:], "${")
		if i < 0 {
			break
		}
		start := pos + i
		r, end, ok := parseVariable(value, start)
		if !ok {
			pos = start + 2
			continue
		}
		pos = end + 1
		if !seen[r.ID] {
			seen[r.ID] = true
			out = append(out, r)
		}
	}
	return out
}

// Folder is a folder of the workspace for ${workspaceFolder:name}.
type Folder struct {
	Name string
	Path string
}

// PromptRequest asks the user for a text (an input of type promptString).
type PromptRequest struct {
	// ID is the id of the input.
	ID          string
	Description string
	// Value is the text to start with: the last one given, else the default.
	Value    string
	Password bool
}

// PickItem is a choice of a pickString input.
type PickItem struct {
	Label       string
	Description string
	Value       string
}

// PickRequest asks the user to pick one of the items; the first is the one to start on.
type PickRequest struct {
	ID          string
	Placeholder string
	Items       []PickItem
}

// VariableContext gives the values the variables read: the window fills what it has, and a variable whose value is missing fails
// with the reason VS Code gives (no editor, no folder, no selection). Every member is optional.
type VariableContext struct {
	// Folders are the folders of the workspace; the launch's own folder is given to the resolver.
	Folders []Folder
	// ActiveFile is the path of the file of the active editor, or "".
	ActiveFile string
	// SelectedText, Line and Column are those of the active editor (Line and Column count from 1; 0 is none).
	SelectedText string
	Line, Column int
	// Cwd is the working folder of Emetos, for ${cwd} without a folder; empty reads os.Getwd.
	Cwd string
	// ExecPath is the program of Emetos, InstallFolder its folder, UserHome the home folder.
	ExecPath, InstallFolder, UserHome string
	// Env reads an environment variable; nil reads the process's.
	Env func(name string) (string, bool)
	// Setting reads a setting for ${config:name}; the folder is the launch's folder.
	Setting func(folder, key string) (any, bool)
	// Command runs a command of an extension for ${command:id} and for an input of type command, and gives its result: a string,
	// or nil when the user cancelled it (which cancels the launch). arg is the configuration for ${command:id} and the args of the
	// input otherwise. A result that is not a string fails with VariableCommandResultInvalid.
	Command func(ctx context.Context, id string, arg any) (any, error)
	// PromptString asks for the text of a promptString input; ok is false when the user cancelled.
	PromptString func(ctx context.Context, req PromptRequest) (value string, ok bool, err error)
	// PickString asks for the choice of a pickString input; ok is false when the user cancelled.
	PickString func(ctx context.Context, req PickRequest) (value string, ok bool, err error)
}

// InputMemory remembers the last value of each input, so that a prompt starts on it (VS Code keeps the last five).
type InputMemory interface {
	// Get returns the value remembered under key.
	Get(key string) (string, bool)
	// Set remembers a value.
	Set(key, value string)
}

// ResolveOptions are what a resolution needs besides the values.
type ResolveOptions struct {
	// Folder is the folder of the launch (an absolute path), or "".
	Folder string
	// Interactive lets ${command:...} and ${input:...} run commands and ask the user; without it they stay as they are.
	Interactive bool
	// Inputs are the inputs of the launch file; nil means the file has none, and ${input:id} then fails.
	Inputs []Input
	// Commands maps the names of ${command:name} to command ids: the interactive variables of the debugger (SPEC §10.31
	// "Contributes.Debuggers... Variables").
	Commands map[string]string
	// Section names the setting an input belongs to ("launch"); it is part of the key under which the last value is remembered.
	Section string
	// Memory remembers the values of inputs; nil forgets them.
	Memory InputMemory
}

// ResolveConfig substitutes the variables in every string of the configuration, in the names of its attributes as well, and returns
// the substituted copy; the platform attributes (linux) are applied first and the others dropped. A variable it cannot resolve
// stops the launch with the reason in an error (the tags of resolveBuiltin, command and input); ok is false, with no error, when
// the user cancelled an input or a command. A variable the resolver does not know stays as written.
//
// Ported from VS Code: src/vs/workbench/services/configurationResolver/common/configurationResolverExpression.ts,
// common/variableResolver.ts and browser/baseConfigurationResolverService.ts
func ResolveConfig(ctx context.Context, vc *VariableContext, cfg Config, opts ResolveOptions) (resolved Config, ok bool, err error) {
	if vc == nil {
		vc = &VariableContext{}
	}
	root := cloneObject(applyPlatform(cfg))
	e := &engine{vc: vc, opts: opts, values: map[string]*resolvedValue{}}
	out, ok, err := e.object(ctx, root, nil)
	if err != nil || !ok {
		return nil, false, err
	}
	return Config(out), true, nil
}

// ResolveString is ResolveConfig for one text.
func ResolveString(ctx context.Context, vc *VariableContext, value string, opts ResolveOptions) (resolved string, ok bool, err error) {
	if vc == nil {
		vc = &VariableContext{}
	}
	e := &engine{vc: vc, opts: opts, values: map[string]*resolvedValue{}}
	return e.text(ctx, value, nil)
}

// applyPlatform copies the attributes of "linux" over the configuration and drops windows, osx and linux.
func applyPlatform(cfg Config) map[string]any {
	out := map[string]any(cfg)
	if lin, ok := out["linux"].(map[string]any); ok {
		merged := make(map[string]any, len(out))
		for k, v := range out {
			merged[k] = v
		}
		for k, v := range lin {
			merged[k] = v
		}
		out = merged
	}
	trimmed := make(map[string]any, len(out))
	for k, v := range out {
		if k == "windows" || k == "osx" || k == "linux" {
			continue
		}
		trimmed[k] = v
	}
	return trimmed
}

// resolvedValue is the cached resolution of one variable: found is false for a variable that stays as written.
type resolvedValue struct {
	value string
	found bool
}

type engine struct {
	vc     *VariableContext
	opts   ResolveOptions
	values map[string]*resolvedValue
}

// object substitutes in the names and values of an object, in the order of the sorted names (the names of a JSON object have no
// order in Go; this keeps the questions to the user in a fixed order).
func (e *engine) object(ctx context.Context, m map[string]any, path []string) (map[string]any, bool, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(m))
	for _, k := range keys {
		nk, ok, err := e.text(ctx, k, path)
		if err != nil || !ok {
			return nil, false, err
		}
		v, ok, err := e.value(ctx, m[k], path)
		if err != nil || !ok {
			return nil, false, err
		}
		out[nk] = v
	}
	return out, true, nil
}

func (e *engine) value(ctx context.Context, v any, path []string) (any, bool, error) {
	switch t := v.(type) {
	case string:
		return e.text(ctx, t, path)
	case map[string]any:
		return e.object(ctx, t, path)
	case []any:
		out := make([]any, len(t))
		for i, el := range t {
			nv, ok, err := e.value(ctx, el, path)
			if err != nil || !ok {
				return nil, false, err
			}
			out[i] = nv
		}
		return out, true, nil
	}
	return v, true, nil
}

// text substitutes the variables of one string. path holds the variables being substituted, so that a value that contains its own
// variable (${env:FOO} whose value is "${env:FOO}") is not substituted again.
func (e *engine) text(ctx context.Context, s string, path []string) (string, bool, error) {
	if !strings.Contains(s, "${") {
		return s, true, nil
	}
	var b strings.Builder
	pos := 0
	for pos < len(s) {
		i := strings.Index(s[pos:], "${")
		if i < 0 {
			break
		}
		start := pos + i
		r, end, ok := parseVariable(s, start)
		if !ok {
			b.WriteString(s[pos : start+2])
			pos = start + 2
			continue
		}
		b.WriteString(s[pos:start])
		pos = end + 1
		if containsString(path, r.ID) {
			b.WriteString(r.ID)
			continue
		}
		rv, err := e.resolve(ctx, r)
		if err != nil {
			return "", false, err
		}
		if rv == nil {
			return "", false, nil // cancelled
		}
		if !rv.found {
			b.WriteString(r.ID)
			continue
		}
		sub, ok, err := e.text(ctx, rv.value, append(path[:len(path):len(path)], r.ID))
		if err != nil || !ok {
			return "", false, err
		}
		b.WriteString(sub)
	}
	b.WriteString(s[pos:])
	return b.String(), true, nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// resolve gives the value of a variable once per resolution. A nil result means the user cancelled.
func (e *engine) resolve(ctx context.Context, r Replacement) (*resolvedValue, error) {
	if v, ok := e.values[r.ID]; ok {
		return v, nil
	}
	var (
		val   string
		found bool
		err   error
	)
	switch r.Name {
	case "command":
		if !e.opts.Interactive {
			e.values[r.ID] = &resolvedValue{}
			return e.values[r.ID], nil
		}
		var cancelled bool
		val, found, cancelled, err = e.command(ctx, r)
		if err == nil && cancelled {
			return nil, nil
		}
	case "input":
		if !e.opts.Interactive {
			e.values[r.ID] = &resolvedValue{}
			return e.values[r.ID], nil
		}
		var cancelled bool
		val, found, cancelled, err = e.input(ctx, r)
		if err == nil && cancelled {
			return nil, nil
		}
	default:
		val, found, err = e.resolveBuiltin(r)
	}
	if err != nil {
		return nil, err
	}
	rv := &resolvedValue{value: val, found: found}
	e.values[r.ID] = rv
	return rv, nil
}

// command runs ${command:name}: the name is mapped through the debugger's variables, else it is the id of the command.
func (e *engine) command(ctx context.Context, r Replacement) (value string, found, cancelled bool, err error) {
	if !r.HasArg {
		return "", false, false, nil
	}
	id := r.Arg
	if mapped, ok := e.opts.Commands[r.Arg]; ok && mapped != "" {
		id = mapped
	}
	if e.vc.Command == nil {
		return "", false, false, nil
	}
	res, cerr := e.vc.Command(ctx, id, nil)
	if cerr != nil {
		if _, ok := errors.FromError(cerr); ok {
			return "", false, false, cerr
		}
		return "", false, false, errors.NewErr("VariableCommandFailed", cerr).SetMetadata("variable", r.ID, "command", id)
	}
	switch t := res.(type) {
	case nil:
		return "", false, true, nil
	case string:
		return t, true, false, nil
	}
	return "", false, false, errors.New("VariableCommandResultInvalid", "Cannot substitute command variable '"+id+"' because command did not return a result of type string.", "variable", r.ID, "command", id)
}

// input runs ${input:id} for the input of that id.
func (e *engine) input(ctx context.Context, r Replacement) (value string, found, cancelled bool, err error) {
	if !r.HasArg {
		return "", false, false, nil
	}
	if e.opts.Inputs == nil {
		return "", false, false, errors.New("InputSectionMissing", "Variable '"+r.Arg+"' must be defined in an 'inputs' section of the debug or task configuration.", "variable", r.ID)
	}
	var info *Input
	for i := range e.opts.Inputs {
		if e.opts.Inputs[i].ID == r.Arg {
			info = &e.opts.Inputs[i] // the last of that id wins, as in VS Code
		}
	}
	if info == nil {
		return "", false, false, errors.New("InputUndefined", "Undefined input variable '"+r.Arg+"' encountered. Remove or define '"+r.Arg+"' to continue.", "variable", r.ID)
	}
	key := e.opts.Section + "." + r.Arg
	remembered := ""
	if e.opts.Memory != nil {
		remembered, _ = e.opts.Memory.Get(key)
	}
	switch info.Type {
	case "promptString":
		if info.Description == "" {
			return "", false, false, errors.New("InputAttributeMissing", "Input variable '"+r.Arg+"' is of type 'promptString' and must include 'description'.", "variable", r.ID, "attribute", "description")
		}
		if e.vc.PromptString == nil {
			return "", false, false, nil
		}
		start := remembered
		if start == "" {
			start = info.Default
		}
		v, ok, perr := e.vc.PromptString(ctx, PromptRequest{ID: info.ID, Description: info.Description, Value: start, Password: info.Password})
		if perr != nil {
			if _, isApp := errors.FromError(perr); isApp {
				return "", false, false, perr
			}
			return "", false, false, errors.NewErr("InputPromptFailed", perr).SetMetadata("variable", r.ID)
		}
		if !ok {
			return "", false, true, nil
		}
		if e.opts.Memory != nil && !info.Password {
			e.opts.Memory.Set(key, v)
		}
		return v, true, false, nil
	case "pickString":
		if info.Description == "" {
			return "", false, false, errors.New("InputAttributeMissing", "Input variable '"+r.Arg+"' is of type 'pickString' and must include 'description'.", "variable", r.ID, "attribute", "description")
		}
		if info.Options == nil {
			return "", false, false, errors.New("InputAttributeMissing", "Input variable '"+r.Arg+"' is of type 'pickString' and must include 'options'.", "variable", r.ID, "attribute", "options")
		}
		if e.vc.PickString == nil {
			return "", false, false, nil
		}
		top := remembered
		if top == "" {
			top = info.Default
		}
		var items []PickItem
		for _, o := range info.Options {
			label := o.Value
			if o.Label != "" {
				label = o.Label + ": " + o.Value
			}
			it := PickItem{Label: label, Value: o.Value}
			switch {
			case o.Value == info.Default && info.Default != "":
				it.Description = "(Default)"
				items = append([]PickItem{it}, items...)
			case o.Value == top && top != "":
				items = append([]PickItem{it}, items...)
			default:
				items = append(items, it)
			}
		}
		v, ok, perr := e.vc.PickString(ctx, PickRequest{ID: info.ID, Placeholder: info.Description, Items: items})
		if perr != nil {
			if _, isApp := errors.FromError(perr); isApp {
				return "", false, false, perr
			}
			return "", false, false, errors.NewErr("InputPromptFailed", perr).SetMetadata("variable", r.ID)
		}
		if !ok {
			return "", false, true, nil
		}
		if e.opts.Memory != nil {
			e.opts.Memory.Set(key, v)
		}
		return v, true, false, nil
	case "command":
		if info.Command == "" {
			return "", false, false, errors.New("InputAttributeMissing", "Input variable '"+r.Arg+"' is of type 'command' and must include 'command'.", "variable", r.ID, "attribute", "command")
		}
		if e.vc.Command == nil {
			return "", false, false, nil
		}
		res, cerr := e.vc.Command(ctx, info.Command, info.Args)
		if cerr != nil {
			if _, ok := errors.FromError(cerr); ok {
				return "", false, false, cerr
			}
			return "", false, false, errors.NewErr("VariableCommandFailed", cerr).SetMetadata("variable", r.ID, "command", info.Command)
		}
		switch t := res.(type) {
		case nil:
			return "", false, true, nil
		case string:
			return t, true, false, nil
		}
		return "", false, false, errors.New("InputCommandResultInvalid", "Cannot substitute input variable '"+r.Arg+"' because command '"+info.Command+"' did not return a result of type string.", "variable", r.ID, "command", info.Command)
	}
	return "", false, false, errors.New("InputTypeUnknown", "Input variable '"+r.Arg+"' can only be of type 'promptString', 'pickString', or 'command'.", "variable", r.ID, "type", info.Type)
}

// nodeExtname is the extension of a name as Node's path.extname gives it: from the last dot, empty when the only dot is the first
// character.
func nodeExtname(name string) string {
	base := filepath.Base(name)
	i := strings.LastIndexByte(base, '.')
	if i <= 0 {
		return ""
	}
	return base[i:]
}

// resolveBuiltin gives the value of the variables of VS Code's list. found is false for a name it does not know, which stays as
// written.
func (e *engine) resolveBuiltin(r Replacement) (string, bool, error) {
	vc := e.vc
	needFile := func() (string, bool) { return vc.ActiveFile, vc.ActiveFile != "" }
	folderFor := func() (string, bool, int) {
		if r.HasArg && r.Arg != "" {
			for _, f := range vc.Folders {
				if f.Name == r.Arg {
					return f.Path, true, 0
				}
			}
			return "", false, 1 // no such folder
		}
		if e.opts.Folder != "" {
			return e.opts.Folder, true, 0
		}
		if len(vc.Folders) > 1 {
			return "", false, 2 // several folders
		}
		return "", false, 3 // none
	}

	switch r.Name {
	case "env":
		if !r.HasArg || r.Arg == "" {
			return "", false, errors.New("VariableEnvNameMissing", "Variable "+r.ID+" can not be resolved because no environment variable name is given.", "variable", r.ID)
		}
		lookup := vc.Env
		if lookup == nil {
			lookup = os.LookupEnv
		}
		v, _ := lookup(r.Arg)
		return v, true, nil
	case "config":
		if !r.HasArg || r.Arg == "" {
			return "", false, errors.New("VariableSettingNameMissing", "Variable "+r.ID+" can not be resolved because no settings name is given.", "variable", r.ID)
		}
		var v any
		var has bool
		if vc.Setting != nil {
			v, has = vc.Setting(e.opts.Folder, r.Arg)
		}
		if !has || v == nil {
			return "", false, errors.New("VariableSettingNotFound", "Variable "+r.ID+" can not be resolved because setting '"+r.Arg+"' not found.", "variable", r.ID, "setting", r.Arg)
		}
		switch t := v.(type) {
		case string:
			return t, true, nil
		case bool:
			return strconv.FormatBool(t), true, nil
		case float64:
			return strconv.FormatFloat(t, 'f', -1, 64), true, nil
		case int:
			return strconv.Itoa(t), true, nil
		}
		return "", false, errors.New("VariableSettingStructured", "Variable "+r.ID+" can not be resolved because '"+r.Arg+"' is a structured value.", "variable", r.ID, "setting", r.Arg)
	case "workspaceRoot", "workspaceFolder":
		p, ok, why := folderFor()
		if !ok {
			switch why {
			case 1:
				return "", false, errors.New("VariableFolderNotFound", "Variable workspaceFolder can not be resolved. No such folder '"+r.Arg+"'.", "variable", r.ID, "folder", r.Arg)
			case 2:
				return "", false, errors.New("VariableFolderAmbiguous", "Variable workspaceFolder can not be resolved in a multi folder workspace. Scope this variable using ':' and a workspace folder name.", "variable", r.ID)
			}
			return "", false, errors.New("VariableNoFolder", "Variable workspaceFolder can not be resolved. Please open a folder.", "variable", r.ID)
		}
		return p, true, nil
	case "cwd":
		if e.opts.Folder == "" && (!r.HasArg || r.Arg == "") {
			if vc.Cwd != "" {
				return vc.Cwd, true, nil
			}
			wd, err := os.Getwd()
			if err != nil {
				return "", false, errors.NewErr("VariableCwdFailed", err).SetMetadata("variable", r.ID)
			}
			return wd, true, nil
		}
		p, ok, why := folderFor()
		if !ok {
			switch why {
			case 1:
				return "", false, errors.New("VariableFolderNotFound", "Variable cwd can not be resolved. No such folder '"+r.Arg+"'.", "variable", r.ID, "folder", r.Arg)
			case 2:
				return "", false, errors.New("VariableFolderAmbiguous", "Variable cwd can not be resolved in a multi folder workspace. Scope this variable using ':' and a workspace folder name.", "variable", r.ID)
			}
			return "", false, errors.New("VariableNoFolder", "Variable cwd can not be resolved. Please open a folder.", "variable", r.ID)
		}
		return p, true, nil
	case "workspaceRootFolderName", "workspaceFolderBasename":
		p, ok, why := folderFor()
		if !ok {
			switch why {
			case 1:
				return "", false, errors.New("VariableFolderNotFound", "Variable workspaceFolderBasename can not be resolved. No such folder '"+r.Arg+"'.", "variable", r.ID, "folder", r.Arg)
			case 2:
				return "", false, errors.New("VariableFolderAmbiguous", "Variable workspaceFolderBasename can not be resolved in a multi folder workspace. Scope this variable using ':' and a workspace folder name.", "variable", r.ID)
			}
			return "", false, errors.New("VariableNoFolder", "Variable workspaceFolderBasename can not be resolved. Please open a folder.", "variable", r.ID)
		}
		return filepath.Base(p), true, nil
	case "userHome":
		if vc.UserHome != "" {
			return vc.UserHome, true, nil
		}
		return "", false, errors.New("VariableNoUserHome", "Variable "+r.ID+" can not be resolved. UserHome path is not defined", "variable", r.ID)
	case "lineNumber":
		if vc.Line > 0 {
			return strconv.Itoa(vc.Line), true, nil
		}
		return "", false, errors.New("VariableNoLine", "Variable "+r.ID+" can not be resolved. Make sure to have a line selected in the active editor.", "variable", r.ID)
	case "columnNumber":
		if vc.Column > 0 {
			return strconv.Itoa(vc.Column), true, nil
		}
		return "", false, errors.New("VariableNoColumn", "Variable "+r.ID+" can not be resolved. Make sure to have a column selected in the active editor.", "variable", r.ID)
	case "selectedText":
		if vc.SelectedText != "" {
			return vc.SelectedText, true, nil
		}
		return "", false, errors.New("VariableNoSelection", "Variable "+r.ID+" can not be resolved. Make sure to have some text selected in the active editor.", "variable", r.ID)
	case "file", "fileWorkspaceFolder", "fileWorkspaceFolderBasename", "relativeFile", "relativeFileDirname", "fileDirname", "fileExtname", "fileBasename", "fileBasenameNoExtension", "fileDirnameBasename":
		file, ok := needFile()
		if !ok {
			return "", false, errors.New("VariableNoEditor", "Variable "+r.ID+" can not be resolved. Please open an editor.", "variable", r.ID)
		}
		switch r.Name {
		case "file":
			return file, true, nil
		case "fileWorkspaceFolder", "fileWorkspaceFolderBasename":
			for _, f := range vc.Folders {
				if rel, err := filepath.Rel(f.Path, file); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					if r.Name == "fileWorkspaceFolderBasename" {
						return filepath.Base(f.Path), true, nil
					}
					return f.Path, true, nil
				}
			}
			return "", false, errors.New("VariableNoFolderOfFile", "Variable "+r.ID+": can not find workspace folder of '"+filepath.Base(file)+"'.", "variable", r.ID, "file", file)
		case "relativeFile", "relativeFileDirname":
			target := file
			if r.Name == "relativeFileDirname" {
				target = filepath.Dir(file)
			}
			if e.opts.Folder != "" || (r.HasArg && r.Arg != "") {
				base, ok, why := folderFor()
				if !ok {
					if why == 1 {
						return "", false, errors.New("VariableFolderNotFound", "Variable "+r.Name+" can not be resolved. No such folder '"+r.Arg+"'.", "variable", r.ID, "folder", r.Arg)
					}
					return "", false, errors.New("VariableNoFolder", "Variable "+r.Name+" can not be resolved. Please open a folder.", "variable", r.ID)
				}
				rel, err := filepath.Rel(base, target)
				if err != nil {
					return target, true, nil
				}
				if r.Name == "relativeFileDirname" && rel == "" {
					rel = "."
				}
				if rel == "." && r.Name == "relativeFile" {
					rel = ""
				}
				return rel, true, nil
			}
			return target, true, nil
		case "fileDirname":
			return filepath.Dir(file), true, nil
		case "fileExtname":
			return nodeExtname(file), true, nil
		case "fileBasename":
			return filepath.Base(file), true, nil
		case "fileBasenameNoExtension":
			base := filepath.Base(file)
			return base[:len(base)-len(nodeExtname(base))], true, nil
		case "fileDirnameBasename":
			return filepath.Base(filepath.Dir(file)), true, nil
		}
	case "execPath":
		if vc.ExecPath != "" {
			return vc.ExecPath, true, nil
		}
		return r.ID, true, nil
	case "execInstallFolder":
		if vc.InstallFolder != "" {
			return vc.InstallFolder, true, nil
		}
		return r.ID, true, nil
	case "pathSeparator", "/":
		return string(filepath.Separator), true, nil
	}
	return "", false, nil
}
