package gitscm

import (
	"strconv"

	"github.com/rm4n0s/emetos/extension"
)

// The diagnostics hook of the commit (git.diagnosticsCommitHook.*): with it on, a commit is held back when the files it takes have
// markers in the Problems panel of the severity the settings list, until the user says to commit anyway.
//
// Ported from VS Code: extensions/git/src/commands.ts (evaluateDiagnosticsCommitHook)

// diagnosticSeverity is the number of a severity setting in VS Code's DiagnosticSeverity: the smaller the more serious.
func diagnosticSeverity(name string) (int, bool) {
	switch name {
	case "error":
		return 0, true
	case "warning":
		return 1, true
	case "information":
		return 2, true
	case "hint":
		return 3, true
	}
	return 0, false
}

// markerSeverity is the number of a marker's severity in the same scale.
func markerSeverity(s extension.Severity) int {
	switch s {
	case extension.SeverityWarning:
		return 1
	case extension.SeverityInfo:
		return 2
	case extension.SeverityHint:
		return 3
	}
	return 0
}

// unresolved counts the markers of a file that the settings call unresolved: a marker with no source or whose source is set to none is
// ignored, a marker of a listed source counts when it is at least as serious as that source's setting, and any other counts when it is at
// least as serious as the "*" setting.
func unresolved(markers []extension.Marker, sources map[string]string) int {
	n := 0
	for _, m := range markers {
		if m.Source == "" || sources[m.Source] == "none" {
			continue
		}
		if level, listed := sources[m.Source]; listed {
			if limit, ok := diagnosticSeverity(level); ok && markerSeverity(m.Severity) <= limit {
				n++
				continue
			}
		}
		if level, ok := sources["*"]; ok {
			if limit, ok := diagnosticSeverity(level); ok && markerSeverity(m.Severity) <= limit {
				n++
			}
		}
	}
	return n
}

// diagnostics is the step of the commit that asks about the problems of the files it takes.
func (c *commitRun) diagnostics(next func()) {
	x, r := c.x, c.r
	if !x.cfg().diagnosticsHook {
		next()
		return
	}
	var list []resource
	switch {
	case len(r.res.index) > 0:
		list = r.res.index
	case c.o.tracked:
		list = filterResources(r.res.working, func(res resource) bool { return !isUntracked(res) })
	default:
		list = r.workAndUntracked()
	}
	var files []string
	for _, res := range list {
		markers, err := x.api.Problems().Read(fileURI(res.path))
		if err != nil {
			if tag := errTag(err); tag != "ExtensionProblemsUnavailable" && tag != "ExtensionDeactivated" {
				x.log.Warn("cannot read the problems of a file", "path", res.path, "err", err)
			}
			continue
		}
		if unresolved(markers, x.cfg().diagnosticsSources) > 0 {
			files = append(files, res.path)
		}
	}
	if len(files) == 0 {
		next()
		return
	}
	spec := extension.DialogSpec{
		Type:    extension.DialogWarning,
		Message: "There are " + strconv.Itoa(len(files)) + " files that have unresolved diagnostics.",
		Detail:  "How would you like to proceed?",
		Buttons: []extension.DialogButton{{Label: "Commit Anyway"}, {Label: "View Problems"}, cancelButton},
	}
	if len(files) == 1 {
		spec.Message = "The following file has unresolved diagnostics: '" + baseName(files[0]) + "'."
	}
	x.ask(spec, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || res.Button == 2:
		case res.Button == 1:
			x.showProblems()
		default:
			next()
		}
	})
}

// showProblems shows the Problems panel; a window that has none does nothing.
func (x *gitscm) showProblems() {
	if err := x.api.Problems().Show(); err != nil {
		if tag := errTag(err); tag != "ExtensionProblemsUnavailable" && tag != "ExtensionDeactivated" {
			x.log.Warn("cannot show the Problems panel", "err", err)
		}
	}
}
