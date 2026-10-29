// Package archtest checks the package boundaries and the forbidden imports of
// SPEC §5.3: the editor core never imports Glutter or the GUI, no cgo, errors go
// through github.com/rm4n0s/errors, built where the failure happens and never in a
// helper (SPEC §5.3 and §13), and the extensions compiled into Emetos import only
// the public package extension (SPEC §10.15).
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
	"slices"
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

	// extensionsPrefix is the folder of the extensions compiled into Emetos, and extensionPackage the public contract
	// they import (SPEC §10.15).
	extensionsPrefix = "extensions/"
	extensionPackage = "extension"
	// extensionUIPrefix is the package that gives extensions the widgets of the editor, the one place of the contract that may
	// import internal packages (SPEC §10.15 "As built (step 11.5)").
	extensionUIPrefix = "extension/ui"
	// emulatorFolder is the terminal emulator (SPEC §10.25), which has no UI and which any extension may import.
	emulatorFolder = "extensions/terminal/emulator"
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
	// RuleExtensionImportsInternal: a package under extensions/ imports a package of Emetos other than extension, a
	// subpackage of its own or one under extensions/internal (internal/..., cmd/...). The public package extension is the
	// only bridge to the editor (SPEC §10.15).
	RuleExtensionImportsInternal Rule = "ExtensionImportsInternal"
	// RuleExtensionImportsExtension: a package under extensions/<name>/ imports another extension, extensions/<other>/... (the terminal
	// emulator, extensions/terminal/emulator, is a library that every extension may import).
	RuleExtensionImportsExtension Rule = "ExtensionImportsExtension"
	// RuleExtensionImportsForbidden: a package under extensions/ imports a module that neither the standard library nor the
	// dependencies CLAUDE.md allows are, or a package inside Glutter's internal folders.
	RuleExtensionImportsForbidden Rule = "ExtensionImportsForbidden"
	// RuleExtensionContractImportsEmetos: a package of the public contract, extension/ but not extension/ui, imports a
	// package of Emetos other than the contract's own (internal/..., cmd/..., extensions/...). The contract imports nothing of
	// the editor (SPEC §10.15).
	RuleExtensionContractImportsEmetos Rule = "ExtensionContractImportsEmetos"
	// RuleExtensionUIImportsInternal: a package under extension/ui imports a package of Emetos other than the contract, its
	// own subpackages, internal/gui/kit and internal/gui/kit/fonts; the test harness extension/ui/uitest may also import
	// internal/gui/testutil. extension/ui is the one bridge of the contract to the widgets of the editor (SPEC §10.15).
	RuleExtensionUIImportsInternal Rule = "ExtensionUIImportsInternal"
	// RuleExtensionsImportedByInternal: a package under internal/ imports extensions/...; only cmd/ lists the extensions, so
	// the editor has no special case for a bundled one. The test files of internal/gui/ are the exception: a test of the
	// window that runs a real extension in it (step 15.4: Remote Work on two windows) imports that one extension; the
	// editor's own code never does.
	RuleExtensionsImportedByInternal Rule = "ExtensionsImportedByInternal"
	// RuleEmulatorImportsUI: a package of the terminal emulator, extensions/terminal/emulator, imports Glutter, a package of
	// Emetos or a module other than the standard library and golang.org/x/text. The emulator has no user interface and is
	// tested without one (SPEC §10.25 "The emulator"); its tests may also import the pseudo-terminal package
	// extensions/internal/pty, to record real programs, and golang.org/x/sys/unix, to time the emulator by the CPU clock.
	RuleEmulatorImportsUI Rule = "EmulatorImportsUI"
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
	inInternal := strings.HasPrefix(rel, "internal/")
	inExtensions := strings.HasPrefix(rel, extensionsPrefix)
	inContract := strings.HasPrefix(rel, extensionPackage+"/")
	inEmulator := rel == emulatorFolder || strings.HasPrefix(rel, emulatorFolder+"/")
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
		if inInternal && isOrUnder(path, emetosModule+"extensions") && (!strings.HasSuffix(rel, "_test.go") || !strings.HasPrefix(rel, "internal/gui/")) {
			add(imp.Pos(), RuleExtensionsImportedByInternal, path)
		}
		if inEmulator && emulatorImportBad(rel, path) {
			add(imp.Pos(), RuleEmulatorImportsUI, path)
		}
		if inExtensions {
			if rule, bad := extensionImportRule(rel, path); bad {
				add(imp.Pos(), rule, path)
			}
		}
		if inContract {
			if rule, bad := contractImportRule(rel, path); bad {
				add(imp.Pos(), rule, path)
			}
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

// emulatorImportBad reports whether an import of a file of the terminal emulator is not allowed: only the standard library
// and golang.org/x/text, and in test files also the pseudo-terminal package.
func emulatorImportBad(rel, path string) bool {
	if !strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
		return false // the standard library
	}
	if isOrUnder(path, "golang.org/x/text") {
		return false
	}
	if !strings.HasSuffix(rel, "_test.go") {
		return true
	}
	// a test may record real programs in a pseudo-terminal, and time the emulator by the CPU clock of its thread
	return path != emetosModule+"extensions/internal/pty" && path != "golang.org/x/sys/unix"
}

// allowedExtensionModules are the modules, besides the standard library and Emetos's own public packages, that a package
// under extensions/ may import: the dependencies CLAUDE.md allows. regexp2 is left to RuleRegexp2Outside.
var allowedExtensionModules = []string{
	errorsModule, glutterModule, "golang.org/x/text", "golang.org/x/image", "golang.org/x/sys",
	"github.com/go-text/typesetting", "golang.org/x/crypto", "github.com/quic-go/quic-go", "github.com/makiuchi-d/gozxing",
}

// extensionImportRule applies the rules of SPEC §10.15 to one import of a file under extensions/ (rel is its path
// relative to the root): the rule it breaks and true, or false when the import is allowed.
func extensionImportRule(rel, path string) (Rule, bool) {
	if isOrUnder(path, regexp2Module) || path == "C" {
		return "", false // other rules
	}
	if !isOrUnder(path, "github.com/rm4n0s/emetos") {
		if !strings.Contains(strings.SplitN(path, "/", 2)[0], ".") {
			return "", false // the standard library
		}
		if strings.Contains(path, "/internal/") && isOrUnder(path, glutterModule) {
			return RuleExtensionImportsForbidden, true
		}
		for _, m := range allowedExtensionModules {
			if isOrUnder(path, m) {
				return "", false
			}
		}
		return RuleExtensionImportsForbidden, true
	}
	inside := strings.TrimPrefix(path, emetosModule)
	switch {
	case path == strings.TrimSuffix(emetosModule, "/"):
		return RuleExtensionImportsInternal, true
	case inside == extensionPackage || strings.HasPrefix(inside, extensionPackage+"/"):
		return "", false
	case inside == "extensions" || strings.HasPrefix(inside, extensionsPrefix):
		// the list in extensions/ imports every extension; an extension imports its own subpackages and the shared
		// extensions/internal ones
		own := strings.SplitN(strings.TrimPrefix(rel, extensionsPrefix), "/", 2)
		if len(own) < 2 {
			return "", false // a file of extensions/ itself
		}
		target := strings.SplitN(strings.TrimPrefix(strings.TrimPrefix(inside, "extensions"), "/"), "/", 2)[0]
		if target == "" || target == own[0] || target == "internal" {
			return "", false
		}
		if isOrUnder(path, emetosModule+emulatorFolder) {
			// the terminal emulator is a library with no UI and no Emetos (RuleEmulatorImportsUI): the Terminals panel of Relay
			// draws a host's terminal with it (SPEC §10.25 "Relay", D27)
			return "", false
		}
		return RuleExtensionImportsExtension, true
	}
	return RuleExtensionImportsInternal, true
}

// uiAllowedImports are the packages of Emetos that extension/ui may import, and uitestAllowedImports the ones that its
// test harness may import besides.
var (
	uiAllowedImports     = []string{emetosModule + "internal/gui/kit", emetosModule + "internal/gui/kit/fonts"}
	uitestAllowedImports = []string{emetosModule + "internal/gui/testutil"}
)

// contractImportRule applies the rules of the public contract to one import of a file under extension/ (rel is its path
// relative to the root): the rule it breaks and true, or false when the import is allowed. Only packages of Emetos are
// looked at; the other imports of the contract are the business of review.
func contractImportRule(rel, path string) (Rule, bool) {
	if !isOrUnder(path, "github.com/rm4n0s/emetos") {
		return "", false
	}
	inside := strings.TrimPrefix(strings.TrimPrefix(path, strings.TrimSuffix(emetosModule, "/")), "/")
	if inside == extensionPackage || strings.HasPrefix(inside, extensionPackage+"/") {
		return "", false
	}
	if !isOrUnder(rel, extensionUIPrefix) {
		return RuleExtensionContractImportsEmetos, true
	}
	allowed := uiAllowedImports
	if isOrUnder(rel, extensionUIPrefix+"/uitest") {
		allowed = append(slices.Clone(allowed), uitestAllowedImports...)
	}
	if slices.Contains(allowed, path) {
		return "", false
	}
	return RuleExtensionUIImportsInternal, true
}

// isOrUnder reports whether path is module or a package below it.
func isOrUnder(path, module string) bool {
	return path == module || strings.HasPrefix(path, module+"/")
}
