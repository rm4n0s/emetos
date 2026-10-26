package golang

import (
	"context"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// "Go: Toggle Test File" (SPEC §10.17 "As built (step 14.2)"): foo.go and foo_test.go, whichever is in front, show the other. A file
// that is not there is made with the package clause alone, so that the editor can show it.

// ToggleTestFileCommand is the command that shows the test file of a file and the file of a test file.
const ToggleTestFileCommand = ID + ".toggleTestFile"

// counterpart is the path of the file that goes with the Go file: the test file of a file, the file of a test file.
func counterpart(path string) string {
	if base, ok := strings.CutSuffix(path, "_test.go"); ok {
		return base + ".go"
	}
	return testFilePath(path)
}

// packageName is the name in the package clause of the source of a Go file; empty when it has none.
func packageName(src string) string {
	f, err := parser.ParseFile(token.NewFileSet(), "document.go", src, parser.PackageClauseOnly)
	if err != nil || f == nil || f.Name == nil {
		return ""
	}
	return f.Name.Name
}

// toggleTestFileCommand is the command "Go: Toggle Test File".
func (g *golang) toggleTestFileCommand(ctx context.Context, _ any) error {
	const what = "Toggle Test File"
	file, ok := g.activeGoFile(ctx, what)
	if !ok {
		return nil
	}
	target := counterpart(file.path)
	uri := fileURI(target)
	base := filepath.Base(target)
	if !g.exists(target, uri) {
		pkg := packageName(file.text)
		if strings.HasSuffix(file.path, "_test.go") {
			pkg = strings.TrimSuffix(pkg, "_test")
		}
		if pkg == "" {
			g.api.Notifications().Warn(what + ": " + filepath.Base(file.path) + " has no package clause, so " + base + " was not made.")
			return nil
		}
		if err := writeNewFile(target, "package "+pkg+"\n"); err != nil {
			g.api.Log().Warn("the file cannot be made", "path", target, "err", err)
			g.api.Notifications().Error(what + ": " + base + " cannot be made: " + describe(err))
			return nil
		}
		g.api.Notifications().Info(what + ": " + base + " did not exist; it was made with the package clause.")
	}
	if err := g.api.Documents().Show(uri, 1, 1, false); err != nil {
		g.api.Log().Warn("the file cannot be shown", "uri", uri, "err", err)
		g.api.Notifications().Error(what + ": " + base + " cannot be opened: " + describe(err))
	}
	return nil
}
