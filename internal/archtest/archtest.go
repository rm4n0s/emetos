// Package archtest checks the package boundaries and the forbidden imports of
// SPEC §5.3: the editor core never imports Glutter or the GUI, no cgo, and errors
// go through github.com/rm4n0s/errors,
// built where the failure happens and never in a helper (SPEC §5.3 and §13).
// ErrorTags and UntestedErrorTags list the error Tags of the code and the ones no
// test asserts (scripts/error-tags.sh).
//
// Check parses every Go file under a root folder, so each rule is tested on small
// fixture trees under testdata, and the real repository is checked by a test too.
// Its tests also hold the repository's shell scripts to their rules (SPEC §15.1),
// such as scripts/isolated.sh never letting a run reach the user's folders.
package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

const (
	errorsModule  = "github.com/rm4n0s/errors"
	emetosModule  = "github.com/rm4n0s/emetos/"
	glutterModule = "github.com/rm4n0s/glutter"
	guiPackage    = "github.com/rm4n0s/emetos/internal/gui"
	regexp2Module = "github.com/dlclark/regexp2"

	corePrefix  = "internal/core/"
	regexPrefix = "internal/core/regex/"
)

// Rule names a boundary rule of SPEC §5.3.
type Rule string

// The rules Check enforces.
const (
	// RuleCoreImportsUI: a package under internal/core imports Glutter or internal/gui.
	RuleCoreImportsUI Rule = "CoreImportsUI"
	// RuleImportC: a file contains import "C".
	RuleImportC Rule = "ImportC"
	// RuleRegexp2Outside: a package other than internal/core/regex imports regexp2.
	RuleRegexp2Outside Rule = "Regexp2Outside"
	// RuleStdErrors: a file imports the standard library errors package.
	RuleStdErrors Rule = "StdErrors"
	// RuleFmtErrorf: a file calls fmt.Errorf.
	RuleFmtErrorf Rule = "FmtErrorf"
	// RulePackageLevelError: a package-level declaration calls errors.New, errors.NewErr or errors.NewErrf.
	RulePackageLevelError Rule = "PackageLevelError"
	// RuleErrorHelper: a function whose only job is to build and return an error.
	RuleErrorHelper Rule = "ErrorHelper"
	// RuleProjectErrorRewrapped: errors.NewErr or errors.NewErrf wraps an error that a function of another package of Emetos returned.
	RuleProjectErrorRewrapped Rule = "ProjectErrorRewrapped"
	// RuleNonLiteralTag: the tag of errors.New, errors.NewErr or errors.NewErrf in non-test code is not a string literal.
	RuleNonLiteralTag Rule = "NonLiteralTag"
)

// Violation is one broken rule at one place.
type Violation struct {
	// File is the slash-separated path of the file relative to the root.
	File string
	// Line is the line of the import or call.
	Line int
	// Rule is the rule that is broken.
	Rule Rule
	// Detail says what was imported or called.
	Detail string
}

// skipDirs are folders that are never checked: the pinned VS Code sources, the
// fixture trees of this package, and tool folders.
var skipDirs = map[string]bool{
	"reference":    true,
	"testdata":     true,
	"vendor":       true,
	"node_modules": true,
	".git":         true,
}

// Check parses every Go file under root and returns the violations of SPEC §5.3,
// sorted by file and line. A folder named reference, testdata, vendor,
// node_modules or .git below root is skipped. Paths in the rules are relative to
// root, so a fixture tree mirrors the repository layout.
//
// Errors carry the tags ArchWalkFailed (a folder or file can't be read) and
// ArchParseFailed (a Go file doesn't parse).
func Check(root string) ([]Violation, error) {
	var found []Violation
	folders := map[string]*helperScan{}
	err := walkGo(root, func(rel string, fset *token.FileSet, file *ast.File) {
		found = append(found, checkFile(fset, rel, file)...)
		if strings.HasSuffix(rel, "_test.go") {
			return
		}
		dir := path.Dir(rel)
		if folders[dir] == nil {
			folders[dir] = &helperScan{}
		}
		folders[dir].scanFile(fset, rel, file)
	})
	if err != nil {
		return nil, err
	}
	for _, s := range folders {
		found = append(found, s.violations()...)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].File != found[j].File {
			return found[i].File < found[j].File
		}
		return found[i].Line < found[j].Line
	})
	return found, nil
}

// walkGo parses every Go file under root, skipping the folders of skipDirs, and
// calls visit with the slash path of the file relative to root. It returns the
// errors of Check.
func walkGo(root string, visit func(rel string, fset *token.FileSet, file *ast.File)) error {
	fset := token.NewFileSet()
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return errors.NewErr("ArchWalkFailed", err).SetMetadata("path", path)
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return errors.NewErr("ArchWalkFailed", err).SetMetadata("path", path)
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			return errors.NewErr("ArchParseFailed", err).SetMetadata("path", path)
		}
		visit(filepath.ToSlash(rel), fset, file)
		return nil
	})
}

// checkFile applies every rule to one parsed file at the slash path rel.
func checkFile(fset *token.FileSet, rel string, file *ast.File) []Violation {
	var found []Violation
	add := func(pos token.Pos, rule Rule, detail string) {
		found = append(found, Violation{File: rel, Line: fset.Position(pos).Line, Rule: rule, Detail: detail})
	}
	inCore := strings.HasPrefix(rel, corePrefix)
	fmtNames := map[string]bool{}
	errorsName := ""
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue // the parser accepted the file, so this cannot happen
		}
		switch {
		case path == "fmt":
			name := "fmt"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name != "_" && name != "." {
				fmtNames[name] = true
			}
		case path == "errors":
			add(imp.Pos(), RuleStdErrors, path)
		case path == errorsModule:
			errorsName = "errors"
			if imp.Name != nil {
				errorsName = imp.Name.Name
			}
		case path == "C":
			add(imp.Pos(), RuleImportC, path)
		case isOrUnder(path, regexp2Module) && !strings.HasPrefix(rel, regexPrefix):
			add(imp.Pos(), RuleRegexp2Outside, path)
		}
		if inCore && (isOrUnder(path, glutterModule) || isOrUnder(path, guiPackage)) {
			add(imp.Pos(), RuleCoreImportsUI, path)
		}
	}
	if len(fmtNames) > 0 {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Errorf" {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && fmtNames[id.Name] {
				add(call.Pos(), RuleFmtErrorf, id.Name+".Errorf")
			}
			return true
		})
	}
	if errorsName != "" && errorsName != "_" && errorsName != "." {
		found = append(found, checkErrorRules(fset, rel, file, errorsName)...)
	}
	return found
}

// isOrUnder reports whether path is module or a package below it.
func isOrUnder(path, module string) bool {
	return path == module || strings.HasPrefix(path, module+"/")
}
