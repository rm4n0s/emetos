package workbench

import "testing"

// TestExpandTemplate ports the cases of `template` in src/vs/base/test/common/labels.test.ts. A value
// that is absent in the VS Code case is the empty one here.
func TestExpandTemplate(t *testing.T) {
	const sep = " - "
	cases := []struct {
		name      string
		template  string
		values    map[string]string
		separator string
		want      string
	}{
		{"plain text", "Foo Bar", nil, "", "Foo Bar"},
		{"an empty variable", "Foo${}Bar", nil, "", "FooBar"},
		{"a dollar without braces", "$FooBar", nil, "", ""},
		{"a closing brace alone", "}FooBar", nil, "", "}FooBar"},
		{"one value", "Foo ${one} Bar", map[string]string{"one": "value"}, "", "Foo value Bar"},
		{"two values", "Foo ${one} Bar ${two}", map[string]string{"one": "value", "two": "other value"}, "", "Foo value Bar other value"},

		{"a separator that is not given", "Foo${separator}Bar", nil, "", "FooBar"},
		{"a separator between text", "Foo${separator}Bar", nil, sep, "Foo - Bar"},
		{"a separator at the start is dropped", "${separator}Foo${separator}Bar", map[string]string{"value": "something"}, sep, "Foo - Bar"},
		{"a value and a separator", "${value} Foo${separator}Bar", map[string]string{"value": "something"}, sep, "something Foo - Bar"},

		{"macOS: nothing", "${activeEditorShort}${separator}${rootName}", map[string]string{}, sep, ""},
		{"macOS: the root", "${activeEditorShort}${separator}${rootName}", map[string]string{"rootName": "root"}, sep, "root"},
		{"macOS: both", "${activeEditorShort}${separator}${rootName}", map[string]string{"activeEditorShort": "markdown.txt", "rootName": "root"}, sep, "markdown.txt - root"},

		{"other: nothing", "${dirty}${activeEditorShort}${separator}${rootName}${separator}${appName}", map[string]string{}, sep, ""},
		{"other: the app", "${dirty}${activeEditorShort}${separator}${rootName}${separator}${appName}", map[string]string{"appName": "Visual Studio Code"}, sep, "Visual Studio Code"},
		{"other: an untitled file", "${dirty}${activeEditorShort}${separator}${rootName}${separator}${appName}",
			map[string]string{"activeEditorShort": "Untitled-1", "appName": "Visual Studio Code"}, sep, "Untitled-1 - Visual Studio Code"},
		{"other: a folder", "${dirty}${activeEditorShort}${separator}${rootName}${separator}${appName}",
			map[string]string{"rootName": "monaco", "appName": "Visual Studio Code"}, sep, "monaco - Visual Studio Code"},
		{"other: a file in a folder", "${dirty}${activeEditorShort}${separator}${rootName}${separator}${appName}",
			map[string]string{"activeEditorShort": "somefile.txt", "rootName": "monaco", "appName": "Visual Studio Code"}, sep, "somefile.txt - monaco - Visual Studio Code"},
		{"other: dirty", "${dirty}${activeEditorShort}${separator}${rootName}${separator}${appName}",
			map[string]string{"dirty": "* ", "activeEditorShort": "somefile.txt", "rootName": "monaco", "appName": "Visual Studio Code"}, sep, "* somefile.txt - monaco - Visual Studio Code"},
		{"the short name of the root", "${dirty}${activeEditorShort}${separator}${rootNameShort}${separator}${appName}",
			map[string]string{"rootName": "monaco (Workspace)", "rootNameShort": "monaco", "appName": "Visual Studio Code"}, sep, "monaco - Visual Studio Code"},

		// not in VS Code's test
		{"two separators in a row are one", "${a}${separator}${separator}${b}", map[string]string{"a": "x", "b": "y"}, sep, "x - y"},
		{"a separator at the end is dropped", "${a}${separator}", map[string]string{"a": "x"}, sep, "x"},
		{"an unterminated variable is dropped", "Foo ${bar", map[string]string{"bar": "x"}, sep, "Foo "},
		{"a name that is not known is left out", "A${nope}B", map[string]string{"x": "y"}, sep, "AB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandTemplate(tc.template, tc.values, tc.separator); got != tc.want {
				t.Errorf("ExpandTemplate(%q) = %q, want %q", tc.template, got, tc.want)
			}
		})
	}
}

func TestTitleVariables(t *testing.T) {
	base := TitleInput{
		Separator: " | ", AppName: "Emetos", ProfileName: "Work", Home: "/home/ann", Folder: "/home/ann/proj",
		ActiveFile: "/home/ann/proj/src/main.go", LanguageID: "go", FocusedView: "Explorer",
	}
	cases := []struct {
		variable string
		mutate   func(*TitleInput)
		want     string
	}{
		{"activeEditorShort", nil, "main.go"},
		{"activeEditorMedium", nil, "src/main.go"},
		{"activeEditorLong", nil, "~/proj/src/main.go"},
		{"activeEditorLanguageId", nil, "go"},
		{"activeFolderShort", nil, "src"},
		{"activeFolderMedium", nil, "src"},
		{"activeFolderLong", nil, "~/proj/src"},
		{"rootName", nil, "proj"},
		{"rootNameShort", nil, "proj"},
		{"rootPath", nil, "~/proj"},
		{"folderName", nil, "proj"},
		{"folderPath", nil, "~/proj"},
		{"appName", nil, "Emetos"},
		{"profileName", nil, "Work"},
		{"focusedView", nil, "Explorer"},
		{"remoteName", nil, ""},
		{"activeEditorState", nil, ""},
		{"dirty", nil, ""},
		{"dirty", func(in *TitleInput) { in.Dirty = true }, TitleDirty},
		{"separator", nil, ""}, // a separator alone has nothing to separate

		// a file in the folder itself
		{"activeFolderMedium", func(in *TitleInput) { in.ActiveFile = "/home/ann/proj/main.go" }, ""},
		{"activeFolderShort", func(in *TitleInput) { in.ActiveFile = "/home/ann/proj/main.go" }, "proj"},
		{"activeEditorMedium", func(in *TitleInput) { in.ActiveFile = "/home/ann/proj/main.go" }, "main.go"},
		// a file outside of the folder is written with its whole path
		{"activeEditorMedium", func(in *TitleInput) { in.ActiveFile = "/etc/hosts" }, "/etc/hosts"},
		{"activeEditorLong", func(in *TitleInput) { in.ActiveFile = "/etc/hosts" }, "/etc/hosts"},
		{"activeFolderMedium", func(in *TitleInput) { in.ActiveFile = "/etc/hosts" }, "/etc"},
		// no folder: the whole path, with ~ for the home folder
		{"activeEditorMedium", func(in *TitleInput) { in.Folder = "" }, "~/proj/src/main.go"},
		{"rootName", func(in *TitleInput) { in.Folder = "" }, ""},
		{"rootPath", func(in *TitleInput) { in.Folder = "" }, ""},
		{"folderName", func(in *TitleInput) { in.Folder = "" }, ""},
		{"folderPath", func(in *TitleInput) { in.Folder = "" }, ""},
		// no home folder: no tilde
		{"activeEditorLong", func(in *TitleInput) { in.Home = "" }, "/home/ann/proj/src/main.go"},
		// the home folder is a folder, not a prefix of a name
		{"activeEditorLong", func(in *TitleInput) { in.Home = "/home/an" }, "/home/ann/proj/src/main.go"},
		// no editor: nothing of the file
		{"activeEditorShort", func(in *TitleInput) { in.ActiveFile = "" }, ""},
		{"activeEditorMedium", func(in *TitleInput) { in.ActiveFile = "" }, ""},
		{"activeEditorLong", func(in *TitleInput) { in.ActiveFile = "" }, ""},
		{"activeFolderShort", func(in *TitleInput) { in.ActiveFile = "" }, ""},
		{"activeFolderLong", func(in *TitleInput) { in.ActiveFile = "" }, ""},
		// an editor that is not a file has a name only
		{"activeEditorShort", func(in *TitleInput) { in.ActiveFile, in.ActiveName = "", "Untitled-1" }, "Untitled-1"},
		{"activeEditorMedium", func(in *TitleInput) { in.ActiveFile, in.ActiveName = "", "Untitled-1" }, "Untitled-1"},
		{"activeEditorLong", func(in *TitleInput) { in.ActiveFile, in.ActiveName = "", "Untitled-1" }, "Untitled-1"},
		{"activeFolderMedium", func(in *TitleInput) { in.ActiveFile, in.ActiveName = "", "Untitled-1" }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.variable, func(t *testing.T) {
			in := base
			if tc.mutate != nil {
				tc.mutate(&in)
			}
			in.Template = "${" + tc.variable + "}"
			// the app name keeps a title that is empty from falling back to it
			want := tc.want
			if want == "" {
				want = in.AppName
			}
			if got := in.Title(); got != want {
				t.Errorf("${%s} = %q, want %q", tc.variable, got, want)
			}
		})
	}
}

func TestTitle(t *testing.T) {
	const defaultTemplate = "${dirty}${activeEditorShort}${separator}${rootName}${separator}${profileName}${separator}${appName}"
	in := func(f func(*TitleInput)) TitleInput {
		t := TitleInput{Template: defaultTemplate, Separator: " - ", AppName: "Emetos", Home: "/home/ann"}
		if f != nil {
			f(&t)
		}
		return t
	}
	cases := []struct {
		name string
		in   TitleInput
		want string
	}{
		{"nothing open", in(nil), "Emetos"},
		{"a file", in(func(i *TitleInput) { i.ActiveFile = "/tmp/a.go" }), "a.go - Emetos"},
		{"a file in a folder", in(func(i *TitleInput) { i.ActiveFile, i.Folder = "/home/ann/p/a.go", "/home/ann/p" }), "a.go - p - Emetos"},
		{"a folder", in(func(i *TitleInput) { i.Folder = "/home/ann/p" }), "p - Emetos"},
		{"a dirty file", in(func(i *TitleInput) { i.ActiveFile, i.Dirty = "/tmp/a.go", true }), "● a.go - Emetos"},
		{"a profile", in(func(i *TitleInput) { i.ActiveFile, i.ProfileName = "/tmp/a.go", "Work" }), "a.go - Work - Emetos"},
		{"another separator", in(func(i *TitleInput) { i.ActiveFile, i.Separator = "/tmp/a.go", " — " }), "a.go — Emetos"},
		{"the empty template is the default", in(func(i *TitleInput) { i.Template, i.ActiveFile = "", "/tmp/a.go" }), "a.go - Emetos"},
		{"a template of text", in(func(i *TitleInput) { i.Template = "Hello" }), "Hello"},
		{"a template that comes out empty is the app", in(func(i *TitleInput) { i.Template = "${rootName}" }), "Emetos"},
		{"white space of a name is a space", in(func(i *TitleInput) { i.ActiveFile = "/tmp/a\nb.go" }), "a b.go - Emetos"},
		{"a template with the language", in(func(i *TitleInput) { i.Template, i.LanguageID = "${activeEditorLanguageId} ${appName}", "go" }), "go Emetos"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Title(); got != tc.want {
				t.Errorf("Title() = %q, want %q", got, tc.want)
			}
		})
	}
}
